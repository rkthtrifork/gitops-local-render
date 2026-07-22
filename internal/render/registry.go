package render

import (
	"context"
	"fmt"
	"sort"

	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

type Registry struct {
	renderers map[string]api.Renderer
	order     []string
}

func NewRegistry(renderers ...api.Renderer) (*Registry, error) {
	registry := &Registry{renderers: map[string]api.Renderer{}}
	for _, renderer := range renderers {
		if renderer.Name() == "" || renderer.Name() == "auto" {
			return nil, fmt.Errorf("renderer has invalid name %q", renderer.Name())
		}
		if _, exists := registry.renderers[renderer.Name()]; exists {
			return nil, fmt.Errorf("duplicate renderer %q", renderer.Name())
		}
		registry.renderers[renderer.Name()] = renderer
		registry.order = append(registry.order, renderer.Name())
	}
	return registry, nil
}

func (r *Registry) Render(ctx context.Context, request api.RenderRequest) ([]api.Object, error) {
	renderer, err := r.selectRenderer(request)
	if err != nil {
		return nil, err
	}
	objects, err := renderer.Render(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("%s renderer for source %q path %q: %w", renderer.Name(), request.Source.Name, request.Path, err)
	}
	return objects, nil
}

func (r *Registry) Capabilities() map[string][]api.Capability {
	result := make(map[string][]api.Capability, len(r.renderers))
	for name, renderer := range r.renderers {
		result[name] = renderer.Capabilities()
	}
	return result
}

func (r *Registry) selectRenderer(request api.RenderRequest) (api.Renderer, error) {
	if request.Renderer != "" && request.Renderer != "auto" {
		renderer, exists := r.renderers[request.Renderer]
		if !exists {
			return nil, fmt.Errorf("renderer %q is not registered", request.Renderer)
		}
		return renderer, nil
	}

	names := append([]string(nil), r.order...)
	sort.Strings(names)
	for _, name := range names {
		renderer := r.renderers[name]
		matched, err := renderer.Detect(request)
		if err != nil {
			return nil, fmt.Errorf("detect renderer %q: %w", name, err)
		}
		if matched {
			return renderer, nil
		}
	}
	return nil, fmt.Errorf("no renderer detected for source %q path %q", request.Source.Name, request.Path)
}
