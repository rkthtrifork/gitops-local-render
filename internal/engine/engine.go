package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/rkthtrifork/gitops-local-render/internal/config"
	"github.com/rkthtrifork/gitops-local-render/internal/render"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

type Engine struct {
	plan      *config.Plan
	renderers *render.Registry
	adapters  map[string]api.Adapter
}

type Result struct {
	Objects []api.Object
	Records []ObjectRecord
	Units   []string
	Skipped []string
}

type ObjectRecord struct {
	Object  api.Object
	Unit    string
	Sources []string
}

func New(plan *config.Plan, renderers *render.Registry, adapters ...api.Adapter) (*Engine, error) {
	engine := &Engine{plan: plan, renderers: renderers, adapters: map[string]api.Adapter{}}
	for _, adapter := range adapters {
		if adapter.Name() == "" {
			return nil, fmt.Errorf("adapter has no name")
		}
		if _, exists := engine.adapters[adapter.Name()]; exists {
			return nil, fmt.Errorf("duplicate adapter %q", adapter.Name())
		}
		engine.adapters[adapter.Name()] = adapter
	}
	return engine, nil
}

func (e *Engine) Run(ctx context.Context) (*Result, error) {
	seeds, err := e.plan.LoadSeeds()
	if err != nil {
		return nil, err
	}
	store := newObjectStore()
	store.put(seeds)

	rootObjects, err := e.renderers.Render(ctx, e.plan.EntrypointRequest())
	if err != nil {
		return nil, fmt.Errorf("render entrypoint: %w", err)
	}
	for _, name := range e.adapterNames() {
		transformer, supported := e.adapters[name].(api.EntrypointTransformer)
		if !supported {
			continue
		}
		rootObjects, err = transformer.TransformEntrypoint(rootObjects, store)
		if err != nil {
			return nil, fmt.Errorf("transform entrypoint with %s adapter: %w", name, err)
		}
	}
	result := &Result{}
	outputIndex := map[api.ObjectKey]int{}
	if err := e.mergeOutput(result, outputIndex, rootObjects, "entrypoint", []string{"entrypoint"}); err != nil {
		return nil, fmt.Errorf("entrypoint output: %w", err)
	}
	store.put(rootObjects)

	pending := map[string]api.Unit{}
	parents := map[string]string{}
	completed := map[string]bool{}
	if err := e.discover(rootObjects, "", pending, parents, completed); err != nil {
		return nil, err
	}

	for len(pending) > 0 {
		unit, found := nextReadyUnit(pending, completed)
		if !found {
			return nil, fmt.Errorf("dependency cycle among deployment units: %s", strings.Join(sortedUnitIDs(pending), ", "))
		}
		delete(pending, unit.ID)

		adapter := e.adapters[unit.Adapter]
		requests, err := adapter.Plan(unit, e.plan)
		if err != nil {
			return nil, fmt.Errorf("plan %s: %w", unit.ID, err)
		}
		if len(requests) == 0 {
			completed[unit.ID] = true
			result.Skipped = append(result.Skipped, unit.ID)
			continue
		}
		results := make([]api.RenderResult, 0, len(requests))
		sources := make([]string, 0, len(requests))
		seenSources := map[string]struct{}{}
		for _, request := range requests {
			if _, seen := seenSources[request.Source.Name]; !seen {
				sources = append(sources, request.Source.Name)
				seenSources[request.Source.Name] = struct{}{}
			}
			objects, err := e.renderers.Render(ctx, request)
			if err != nil {
				return nil, fmt.Errorf("render %s: %w", unit.ID, err)
			}
			results = append(results, api.RenderResult{Request: request, Objects: objects})
		}
		objects, err := adapter.Transform(unit, results, store)
		if err != nil {
			return nil, fmt.Errorf("transform %s: %w", unit.ID, err)
		}
		sort.Strings(sources)
		if err := e.mergeOutput(result, outputIndex, objects, unit.ID, sources); err != nil {
			return nil, fmt.Errorf("output from %s: %w", unit.ID, err)
		}
		store.put(objects)
		completed[unit.ID] = true
		result.Units = append(result.Units, unit.ID)
		if err := e.discover(objects, unit.ID, pending, parents, completed); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (e *Engine) Capabilities() map[string]map[string][]api.Capability {
	adapters := map[string][]api.Capability{}
	for name, adapter := range e.adapters {
		adapters[name] = adapter.Capabilities()
	}
	return map[string]map[string][]api.Capability{
		"adapters":  adapters,
		"renderers": e.renderers.Capabilities(),
	}
}

func (e *Engine) discover(objects []api.Object, parent string, pending map[string]api.Unit, parents map[string]string, completed map[string]bool) error {
	for _, name := range e.adapterNames() {
		units, err := e.adapters[name].Discover(objects)
		if err != nil {
			return fmt.Errorf("discover %s deployment units: %w", name, err)
		}
		for _, unit := range units {
			if unit.ID == "" || unit.Adapter != name {
				return fmt.Errorf("adapter %q returned invalid deployment unit %#v", name, unit)
			}
			if isAncestor(unit.ID, parent, parents) {
				return fmt.Errorf("deployment graph cycle: %s is an ancestor of %s", unit.ID, parent)
			}
			if completed[unit.ID] {
				continue
			}
			if existing, exists := pending[unit.ID]; exists {
				if existing.Object.Key() != unit.Object.Key() {
					return fmt.Errorf("deployment unit ID %q has conflicting objects", unit.ID)
				}
				continue
			}
			pending[unit.ID] = unit
			parents[unit.ID] = parent
		}
	}
	return nil
}

func (e *Engine) adapterNames() []string {
	names := make([]string, 0, len(e.adapters))
	for name := range e.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (e *Engine) mergeOutput(result *Result, index map[api.ObjectKey]int, objects []api.Object, unit string, sources []string) error {
	for _, object := range objects {
		key := object.Key()
		if object.APIVersion() == "" || key.Kind == "" || key.Name == "" {
			return fmt.Errorf("document has incomplete Kubernetes identity")
		}
		position, duplicate := index[key]
		if duplicate && e.plan.Strict.DuplicateObject == "error" {
			return fmt.Errorf("duplicate object %s", key)
		}
		if duplicate && e.plan.Strict.DuplicateObject == "last-wins" {
			result.Objects[position] = object
			result.Records[position] = ObjectRecord{Object: object, Unit: unit, Sources: sources}
			continue
		}
		if !duplicate {
			index[key] = len(result.Objects)
		}
		result.Objects = append(result.Objects, object)
		result.Records = append(result.Records, ObjectRecord{Object: object, Unit: unit, Sources: sources})
	}
	return nil
}

func nextReadyUnit(pending map[string]api.Unit, completed map[string]bool) (api.Unit, bool) {
	for _, id := range sortedUnitIDs(pending) {
		unit := pending[id]
		blocked := false
		for _, dependency := range unit.DependsOn {
			if _, local := pending[dependency]; local && !completed[dependency] {
				blocked = true
				break
			}
		}
		if !blocked {
			return unit, true
		}
	}
	return api.Unit{}, false
}

func sortedUnitIDs(units map[string]api.Unit) []string {
	ids := make([]string, 0, len(units))
	for id := range units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func isAncestor(candidate, current string, parents map[string]string) bool {
	for current != "" {
		if current == candidate {
			return true
		}
		current = parents[current]
	}
	return false
}
