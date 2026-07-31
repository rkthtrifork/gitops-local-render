package render

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

func TestKustomizeAllowsBasesInsideSource(t *testing.T) {
	root := t.TempDir()
	writeRenderFile(t, filepath.Join(root, "base", "kustomization.yaml"), "resources:\n  - namespace.yaml\n")
	writeRenderFile(t, filepath.Join(root, "base", "namespace.yaml"), "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: test\n")
	writeRenderFile(t, filepath.Join(root, "overlays", "dev", "kustomization.yaml"), "resources:\n  - ../../base\n")

	objects, err := (Kustomize{}).Render(context.Background(), api.RenderRequest{Source: api.LocalSource{Name: "test", Path: root}, Path: "overlays/dev"})
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 || objects[0].Name() != "test" {
		t.Fatalf("unexpected objects: %#v", objects)
	}
}

func TestKustomizeRejectsSourceEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "source")
	writeRenderFile(t, filepath.Join(root, "overlay", "kustomization.yaml"), "resources:\n  - ../../outside.yaml\n")
	writeRenderFile(t, filepath.Join(parent, "outside.yaml"), "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: outside\n")

	_, err := (Kustomize{}).Render(context.Background(), api.RenderRequest{Source: api.LocalSource{Name: "test", Path: root}, Path: "overlay"})
	if err == nil || !strings.Contains(err.Error(), "escapes local source root") {
		t.Fatalf("expected source escape error, got %v", err)
	}
}

func TestKustomizeBuildsGeneratedManifestWithoutModifyingSource(t *testing.T) {
	root := t.TempDir()
	original := "resources:\n  - namespace.yaml\ncomponents:\n  - ../common\n"
	writeRenderFile(t, filepath.Join(root, "base", "kustomization.yaml"), original)
	writeRenderFile(t, filepath.Join(root, "base", "namespace.yaml"), "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: app\n")
	writeRenderFile(t, filepath.Join(root, "common", "kustomization.yaml"), "apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\nresources:\n  - common.yaml\n")
	writeRenderFile(t, filepath.Join(root, "common", "common.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: common\n")
	writeRenderFile(t, filepath.Join(root, "local", "kustomization.yaml"), "apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\nresources:\n  - local.yaml\n")
	writeRenderFile(t, filepath.Join(root, "local", "local.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: local\n")

	objects, err := (Kustomize{}).Render(context.Background(), api.RenderRequest{
		Source: api.LocalSource{Name: "test", Path: root}, Path: "base",
		Kustomize: &api.KustomizeOptions{
			KustomizationFile: "kustomization.yaml",
			Kustomization:     []byte("resources:\n  - namespace.yaml\ncomponents:\n  - ../common\n  - ../local\n"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 3 {
		t.Fatalf("expected base and both component resources, got %#v", objects)
	}
	data, err := os.ReadFile(filepath.Join(root, "base", "kustomization.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("source Kustomization was modified:\n%s", data)
	}
}

func TestKustomizeBuildsGeneratedKustomizationForPlainManifests(t *testing.T) {
	root := t.TempDir()
	writeRenderFile(t, filepath.Join(root, "base", "namespace.yaml"), "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: app\n")
	request := api.RenderRequest{
		Source: api.LocalSource{Name: "test", Path: root}, Path: "base",
		Kustomize: &api.KustomizeOptions{
			KustomizationFile: "kustomization.yaml",
			Kustomization:     []byte("resources:\n  - namespace.yaml\n"),
		},
	}
	objects, err := (Kustomize{}).Render(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 || objects[0].Name() != "app" {
		t.Fatalf("unexpected objects: %#v", objects)
	}

	if _, err := os.Stat(filepath.Join(root, "base", "kustomization.yaml")); !os.IsNotExist(err) {
		t.Fatalf("expected generated Kustomization to remain in memory, got %v", err)
	}
}

func writeRenderFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
