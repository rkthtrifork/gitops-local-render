package render

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rkthtrifork/gitops-local-render/internal/manifest"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/types"
)

type Kustomize struct{}

func (Kustomize) Name() string { return "kustomize" }

func (Kustomize) Capabilities() []api.Capability {
	return []api.Capability{{Name: "source-jail", Description: "Allows cross-directory bases only within the mapped local source"}}
}

func (Kustomize) Detect(request api.RenderRequest) (bool, error) {
	path, err := pathInside(request.Source.Path, request.Path)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	for _, name := range []string{"kustomization.yaml", "kustomization.yml", "Kustomization"} {
		if _, err := os.Stat(filepath.Join(path, name)); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}

func (Kustomize) Render(_ context.Context, request api.RenderRequest) ([]api.Object, error) {
	path, err := pathInside(request.Source.Path, request.Path)
	if err != nil {
		return nil, err
	}
	fs, err := newJailFS(request.Source.Path)
	if err != nil {
		return nil, fmt.Errorf("create source filesystem: %w", err)
	}
	options := krusty.MakeDefaultOptions()
	options.LoadRestrictions = types.LoadRestrictionsNone
	kustomizer := krusty.MakeKustomizer(options)
	resources, err := kustomizer.Run(fs, path)
	if err != nil {
		return nil, err
	}
	data, err := resources.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("serialize Kustomize output: %w", err)
	}
	return manifest.Parse(data)
}
