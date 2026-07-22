package argocd

import (
	"fmt"
	"strings"

	adapterutil "github.com/rkthtrifork/gitops-local-render/internal/adapter"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

const (
	name       = "argocd"
	apiVersion = "argoproj.io/v1alpha1"
)

type Adapter struct{}

func (Adapter) Name() string { return name }

func (Adapter) Capabilities() []api.Capability {
	return []api.Capability{
		{Name: "application", Description: "Discovers Argo CD v1alpha1 Applications"},
		{Name: "multiple-sources", Description: "Renders Application sources in declaration order"},
		{Name: "app-of-apps", Description: "Recursively discovers Applications in rendered output"},
	}
}

func (Adapter) Discover(objects []api.Object) ([]api.Unit, error) {
	var units []api.Unit
	for _, object := range objects {
		if object.Kind() != "Application" {
			continue
		}
		if object.APIVersion() != apiVersion {
			if strings.HasPrefix(object.APIVersion(), "argoproj.io/") {
				return nil, fmt.Errorf("unsupported Argo CD Application apiVersion %q", object.APIVersion())
			}
			continue
		}
		namespace := object.Namespace()
		if namespace == "" {
			namespace = "default"
		}
		if object.Name() == "" {
			return nil, fmt.Errorf("Argo CD Application has no metadata.name")
		}
		units = append(units, api.Unit{ID: fmt.Sprintf("argocd:%s/%s", namespace, object.Name()), Adapter: name, Object: object})
	}
	return units, nil
}

func (Adapter) Plan(unit api.Unit, resolver api.SourceResolver) ([]api.RenderRequest, error) {
	spec, err := adapterutil.Map(unit.Object.Data["spec"], "spec")
	if err != nil {
		return nil, err
	}
	sources, err := applicationSources(spec)
	if err != nil {
		return nil, err
	}
	requests := make([]api.RenderRequest, 0, len(sources))
	for index, sourceSpec := range sources {
		path := adapterutil.String(sourceSpec, "path")
		chart := adapterutil.String(sourceSpec, "chart")
		if path == "" && chart == "" && adapterutil.String(sourceSpec, "ref") != "" {
			continue
		}
		repoURL := adapterutil.String(sourceSpec, "repoURL")
		if repoURL == "" {
			return nil, fmt.Errorf("source[%d].repoURL is required", index)
		}
		targetRevision := adapterutil.String(sourceSpec, "targetRevision")
		if targetRevision == "" {
			targetRevision = "HEAD"
		}
		source, err := resolver.Resolve(api.SourceQuery{Adapter: name, Fields: map[string]string{
			"repoURL": repoURL, "targetRevision": targetRevision,
		}})
		if err != nil {
			return nil, fmt.Errorf("source[%d]: %w", index, err)
		}
		if source.Ignored {
			continue
		}
		renderer, recursive, err := rendererFor(sourceSpec)
		if err != nil {
			return nil, fmt.Errorf("source[%d]: %w", index, err)
		}
		if chart != "" {
			path = chart
		}
		if path == "" {
			return nil, fmt.Errorf("source[%d] requires path or chart", index)
		}
		requests = append(requests, api.RenderRequest{Renderer: renderer, Source: source, Path: path, Recursive: recursive})
	}
	return requests, nil
}

func (Adapter) Transform(_ api.Unit, results []api.RenderResult, _ api.ObjectLookup) ([]api.Object, error) {
	var objects []api.Object
	for _, result := range results {
		objects = append(objects, result.Objects...)
	}
	return objects, nil
}

func applicationSources(spec map[string]any) ([]map[string]any, error) {
	if rawSources, exists := spec["sources"]; exists {
		items, err := adapterutil.Slice(rawSources, "spec.sources")
		if err != nil {
			return nil, err
		}
		result := make([]map[string]any, 0, len(items))
		for index, item := range items {
			source, err := adapterutil.Map(item, fmt.Sprintf("spec.sources[%d]", index))
			if err != nil {
				return nil, err
			}
			result = append(result, source)
		}
		return result, nil
	}
	source, err := adapterutil.Map(spec["source"], "spec.source")
	if err != nil {
		return nil, err
	}
	return []map[string]any{source}, nil
}

func rendererFor(source map[string]any) (string, bool, error) {
	if _, exists := source["plugin"]; exists {
		return "argocd-cmp", false, nil
	}
	if _, exists := source["helm"]; exists || adapterutil.String(source, "chart") != "" {
		return "helm", false, nil
	}
	if _, exists := source["jsonnet"]; exists {
		return "jsonnet", false, nil
	}
	if rawKustomize, exists := source["kustomize"]; exists {
		options, err := adapterutil.Map(rawKustomize, "source.kustomize")
		if err != nil {
			return "", false, err
		}
		if len(options) > 0 {
			return "", false, fmt.Errorf("Argo CD source.kustomize options are not yet supported")
		}
		return "kustomize", false, nil
	}
	if rawDirectory, exists := source["directory"]; exists {
		options, err := adapterutil.Map(rawDirectory, "source.directory")
		if err != nil {
			return "", false, err
		}
		return "raw", adapterutil.Bool(options, "recurse"), nil
	}
	return "auto", false, nil
}
