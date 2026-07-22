package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

func TestLoadRejectsUnknownFields(t *testing.T) {
	directory := t.TempDir()
	writeTestFile(t, filepath.Join(directory, "manifests.yaml"), "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: test\n")
	planPath := filepath.Join(directory, "plan.yaml")
	writeTestFile(t, planPath, `apiVersion: gitops-local-render.dev/v1alpha1
kind: RenderPlan
entrypoint:
  path: manifests.yaml
unexpected: true
`)

	_, err := Load(planPath)
	if err == nil || !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("expected strict field error, got %v", err)
	}
}

func TestResolveRequiresOneExactSource(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"one", "two"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	plan := &Plan{Sources: []Source{
		{Name: "one", Path: filepath.Join(directory, "one"), Selectors: Selectors{ArgoCD: []ArgoCDSelector{{RepoURL: "https://example.test/repo.git"}}}},
		{Name: "two", Path: filepath.Join(directory, "two"), Selectors: Selectors{ArgoCD: []ArgoCDSelector{{RepoURL: "https://example.test/repo.git"}}}},
	}}

	_, err := plan.Resolve(api.SourceQuery{Adapter: "argocd", Fields: map[string]string{"repoURL": "https://example.test/repo.git", "targetRevision": "HEAD"}})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
