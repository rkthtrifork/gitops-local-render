package render

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rkthtrifork/gitops-local-render/internal/manifest"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

type Raw struct{}

func (Raw) Name() string { return "raw" }

func (Raw) Capabilities() []api.Capability {
	return []api.Capability{{Name: "yaml-directory", Description: "Reads Kubernetes YAML from a file or directory in lexical order"}}
}

func (Raw) Detect(request api.RenderRequest) (bool, error) {
	_, err := pathInside(request.Source.Path, request.Path)
	return err == nil, err
}

func (Raw) Render(_ context.Context, request api.RenderRequest) ([]api.Object, error) {
	path, err := pathInside(request.Source.Path, request.Path)
	if err != nil {
		return nil, err
	}
	files, err := rawFiles(path, request.Recursive)
	if err != nil {
		return nil, err
	}

	var objects []api.Object
	for _, file := range files {
		if _, err := pathInside(request.Source.Path, file); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", file, err)
		}
		parsed, err := manifest.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("parse %q: %w", file, err)
		}
		for _, object := range parsed {
			if object.APIVersion() == "" || object.Kind() == "" {
				return nil, fmt.Errorf("%q contains a document without apiVersion or kind", file)
			}
		}
		objects = append(objects, parsed...)
	}
	return objects, nil
}

func rawFiles(path string, recursive bool) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().IsRegular() {
		if !isYAML(path) {
			return nil, fmt.Errorf("raw entrypoint %q is not a YAML file", path)
		}
		return []string{path}, nil
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("raw path %q is not a regular file or directory", path)
	}

	var files []string
	err = filepath.Walk(path, func(current string, entry os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == path {
			return nil
		}
		if entry.IsDir() {
			if !recursive {
				return filepath.SkipDir
			}
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("raw renderer does not follow symlink %q", current)
		}
		if isYAML(current) && !isKustomizationFile(current) {
			files = append(files, current)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func isYAML(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return extension == ".yaml" || extension == ".yml"
}

func isKustomizationFile(path string) bool {
	switch filepath.Base(path) {
	case "kustomization.yaml", "kustomization.yml", "Kustomization":
		return true
	default:
		return false
	}
}
