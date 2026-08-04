package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/rkthtrifork/gitops-local-render/internal/manifest"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
	"gopkg.in/yaml.v3"
)

const APIVersion = "gitops-local-render.dev/v1alpha1"

type Plan struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Entrypoint Entrypoint     `yaml:"entrypoint"`
	Sources    []Source       `yaml:"sources"`
	Adapters   AdapterOptions `yaml:"adapters,omitempty"`
	Strict     StrictOptions  `yaml:"strict,omitempty"`
	baseDir    string
}

type Entrypoint struct {
	Path      string `yaml:"path"`
	Renderer  string `yaml:"renderer,omitempty"`
	Recursive bool   `yaml:"recursive,omitempty"`
}

type Source struct {
	Name      string    `yaml:"name"`
	Path      string    `yaml:"path"`
	Selectors Selectors `yaml:"selectors"`
}

type Selectors struct {
	Flux   []FluxSelector   `yaml:"flux,omitempty"`
	ArgoCD []ArgoCDSelector `yaml:"argocd,omitempty"`
}

type FluxSelector struct {
	Kind      string `yaml:"kind"`
	Namespace string `yaml:"namespace"`
	Name      string `yaml:"name"`
}

type ArgoCDSelector struct {
	RepoURL        string `yaml:"repoURL"`
	TargetRevision string `yaml:"targetRevision,omitempty"`
}

type AdapterOptions struct {
	Flux FluxOptions `yaml:"flux,omitempty"`
}

type FluxOptions struct {
	SeedObjects []string        `yaml:"seedObjects,omitempty"`
	Entrypoint  *FluxEntrypoint `yaml:"entrypoint,omitempty"`
}

type FluxEntrypoint struct {
	Namespace          string            `yaml:"namespace,omitempty"`
	SubstituteFrom     []SubstituteRef   `yaml:"substituteFrom,omitempty"`
	Substitute         map[string]string `yaml:"substitute,omitempty"`
	SubstituteStrategy string            `yaml:"substituteStrategy,omitempty"`
}

type SubstituteRef struct {
	Kind     string `yaml:"kind,omitempty"`
	Name     string `yaml:"name"`
	Optional bool   `yaml:"optional,omitempty"`
}

type StrictOptions struct {
	DuplicateObject string `yaml:"duplicateObject,omitempty"`
	UnmappedSource  string `yaml:"unmappedSource,omitempty"`
}

func Load(path string) (*Plan, error) {
	return LoadWithOptions(path, LoadOptions{})
}

type LoadOptions struct {
	WorkspaceRoot string
}

func LoadWithOptions(path string, options LoadOptions) (*Plan, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve plan path: %w", err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read plan: %w", err)
	}

	var plan Plan
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&plan); err != nil {
		return nil, fmt.Errorf("decode plan: %w", err)
	}
	plan.baseDir = filepath.Dir(absPath)
	if options.WorkspaceRoot != "" {
		workspaceRoot, err := filepath.Abs(options.WorkspaceRoot)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace root: %w", err)
		}
		workspaceRoot, err = secureExistingPath(workspaceRoot, ".")
		if err != nil {
			return nil, fmt.Errorf("resolve workspace root: %w", err)
		}
		info, err := os.Stat(workspaceRoot)
		if err != nil {
			return nil, fmt.Errorf("inspect workspace root: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("workspace root %q is not a directory", workspaceRoot)
		}
		plan.baseDir = workspaceRoot
	}
	if err := plan.normalizeAndValidate(); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (p *Plan) EntrypointRequest() api.RenderRequest {
	renderer := p.Entrypoint.Renderer
	if renderer == "" {
		renderer = "auto"
	}
	return api.RenderRequest{
		Renderer:  renderer,
		Source:    api.LocalSource{Name: "entrypoint", Path: p.baseDir},
		Path:      p.Entrypoint.Path,
		Recursive: p.Entrypoint.Recursive,
	}
}

func (p *Plan) LoadSeeds() ([]api.Object, error) {
	var seeds []api.Object
	for _, seedPath := range p.Adapters.Flux.SeedObjects {
		data, err := os.ReadFile(seedPath)
		if err != nil {
			return nil, fmt.Errorf("read seed object %q: %w", seedPath, err)
		}
		objects, err := manifest.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("parse seed object %q: %w", seedPath, err)
		}
		seeds = append(seeds, objects...)
	}
	return seeds, nil
}

func (p *Plan) Resolve(query api.SourceQuery) (api.LocalSource, error) {
	var matches []api.LocalSource
	for _, source := range p.Sources {
		if source.matches(query) {
			matches = append(matches, api.LocalSource{Name: source.Name, Path: source.Path})
		}
	}
	if len(matches) == 0 {
		if p.Strict.UnmappedSource == "ignore" {
			return api.LocalSource{Name: query.Adapter + ":" + formatFields(query.Fields), Ignored: true}, nil
		}
		return api.LocalSource{}, fmt.Errorf("no local source matches %s selector %s", query.Adapter, formatFields(query.Fields))
	}
	if len(matches) > 1 {
		names := make([]string, 0, len(matches))
		for _, match := range matches {
			names = append(names, match.Name)
		}
		sort.Strings(names)
		return api.LocalSource{}, fmt.Errorf("ambiguous %s selector %s matches sources %v", query.Adapter, formatFields(query.Fields), names)
	}
	return matches[0], nil
}

func (p *Plan) normalizeAndValidate() error {
	if p.APIVersion != APIVersion || p.Kind != "RenderPlan" {
		return fmt.Errorf("plan must have apiVersion %q and kind RenderPlan", APIVersion)
	}
	if p.Entrypoint.Path == "" {
		return fmt.Errorf("entrypoint.path is required")
	}
	entrypoint, err := secureExistingPath(p.baseDir, p.Entrypoint.Path)
	if err != nil {
		return fmt.Errorf("entrypoint.path: %w", err)
	}
	p.Entrypoint.Path = entrypoint

	seenNames := map[string]struct{}{}
	for index := range p.Sources {
		source := &p.Sources[index]
		if source.Name == "" || source.Path == "" {
			return fmt.Errorf("sources[%d] requires name and path", index)
		}
		if _, exists := seenNames[source.Name]; exists {
			return fmt.Errorf("duplicate source name %q", source.Name)
		}
		seenNames[source.Name] = struct{}{}
		resolved, err := secureExistingPath(p.baseDir, source.Path)
		if err != nil {
			return fmt.Errorf("source %q: %w", source.Name, err)
		}
		source.Path = resolved
		if len(source.Selectors.Flux) == 0 && len(source.Selectors.ArgoCD) == 0 {
			return fmt.Errorf("source %q must have at least one selector", source.Name)
		}
		for _, selector := range source.Selectors.Flux {
			if selector.Kind == "" || selector.Namespace == "" || selector.Name == "" {
				return fmt.Errorf("source %q has an incomplete Flux selector", source.Name)
			}
		}
		for _, selector := range source.Selectors.ArgoCD {
			if selector.RepoURL == "" {
				return fmt.Errorf("source %q has an Argo CD selector without repoURL", source.Name)
			}
		}
	}

	for index, seed := range p.Adapters.Flux.SeedObjects {
		resolved, err := secureExistingPath(p.baseDir, seed)
		if err != nil {
			return fmt.Errorf("adapters.flux.seedObjects[%d]: %w", index, err)
		}
		p.Adapters.Flux.SeedObjects[index] = resolved
	}
	if entrypoint := p.Adapters.Flux.Entrypoint; entrypoint != nil {
		if entrypoint.Namespace == "" {
			entrypoint.Namespace = "flux-system"
		}
		if entrypoint.SubstituteStrategy == "" {
			entrypoint.SubstituteStrategy = "WithVariables"
		}
		if entrypoint.SubstituteStrategy != "WithVariables" && entrypoint.SubstituteStrategy != "Always" {
			return fmt.Errorf("adapters.flux.entrypoint.substituteStrategy must be WithVariables or Always")
		}
		for index := range entrypoint.SubstituteFrom {
			reference := &entrypoint.SubstituteFrom[index]
			if reference.Kind == "" {
				reference.Kind = "ConfigMap"
			}
			if reference.Kind != "ConfigMap" && reference.Kind != "Secret" {
				return fmt.Errorf("adapters.flux.entrypoint.substituteFrom[%d].kind must be ConfigMap or Secret", index)
			}
			if reference.Name == "" {
				return fmt.Errorf("adapters.flux.entrypoint.substituteFrom[%d].name is required", index)
			}
		}
	}
	if p.Strict.DuplicateObject == "" {
		p.Strict.DuplicateObject = "error"
	}
	if p.Strict.DuplicateObject != "error" && p.Strict.DuplicateObject != "last-wins" && p.Strict.DuplicateObject != "preserve" {
		return fmt.Errorf("strict.duplicateObject must be error, last-wins, or preserve")
	}
	if p.Strict.UnmappedSource == "" {
		p.Strict.UnmappedSource = "error"
	}
	if p.Strict.UnmappedSource != "error" && p.Strict.UnmappedSource != "ignore" {
		return fmt.Errorf("strict.unmappedSource must be error or ignore")
	}
	return nil
}

func (s Source) matches(query api.SourceQuery) bool {
	switch query.Adapter {
	case "flux":
		for _, selector := range s.Selectors.Flux {
			if selector.Kind == query.Fields["kind"] && selector.Namespace == query.Fields["namespace"] && selector.Name == query.Fields["name"] {
				return true
			}
		}
	case "argocd":
		for _, selector := range s.Selectors.ArgoCD {
			if selector.RepoURL != query.Fields["repoURL"] {
				continue
			}
			if selector.TargetRevision == "" || selector.TargetRevision == query.Fields["targetRevision"] {
				return true
			}
		}
	}
	return false
}

func secureExistingPath(base, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q is neither a directory nor a regular file", path)
	}
	return resolved, nil
}

func formatFields(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := ""
	for _, key := range keys {
		if result != "" {
			result += ","
		}
		result += key + "=" + fields[key]
	}
	return result
}
