package render

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	fluxkustomize "github.com/fluxcd/pkg/kustomize"
	"github.com/rkthtrifork/gitops-local-render/internal/manifest"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

type Kustomize struct{}

func (Kustomize) Name() string { return "kustomize" }

func (Kustomize) Capabilities() []api.Capability {
	return []api.Capability{
		{Name: "source-jail", Description: "Allows cross-directory bases only within the mapped local source"},
		{Name: "manifest-overlay", Description: "Builds an adapter-generated Kustomization without modifying the source"},
		{Name: "upstream-flux-build", Description: "Uses fluxcd/pkg/kustomize Build around upstream Kustomize"},
	}
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
	if err := applyKustomizeOptions(fs, path, request.Kustomize); err != nil {
		return nil, err
	}
	resources, err := fluxkustomize.Build(fs, path)
	if err != nil {
		return nil, err
	}
	data, err := resources.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("serialize Kustomize output: %w", err)
	}
	return manifest.Parse(data)
}

func applyKustomizeOptions(fs *jailFS, path string, options *api.KustomizeOptions) error {
	if options == nil || len(options.Kustomization) == 0 {
		return nil
	}
	if options.KustomizationFile != "kustomization.yaml" && options.KustomizationFile != "kustomization.yml" && options.KustomizationFile != "Kustomization" {
		return fmt.Errorf("unrecognized Kustomization file %q", options.KustomizationFile)
	}
	kustomizationPath := filepath.Join(path, options.KustomizationFile)
	if err := fs.overlay(kustomizationPath, options.Kustomization); err != nil {
		return fmt.Errorf("overlay generated Kustomization %q: %w", kustomizationPath, err)
	}
	return nil
}
