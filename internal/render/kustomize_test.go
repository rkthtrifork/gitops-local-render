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

func writeRenderFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
