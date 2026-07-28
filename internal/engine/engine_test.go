package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	argocdadapter "github.com/rkthtrifork/gitops-local-render/internal/adapter/argocd"
	fluxadapter "github.com/rkthtrifork/gitops-local-render/internal/adapter/flux"
	"github.com/rkthtrifork/gitops-local-render/internal/config"
	"github.com/rkthtrifork/gitops-local-render/internal/engine"
	"github.com/rkthtrifork/gitops-local-render/internal/render"
)

func TestFluxGraphRendersParentSubstitutedChildPath(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "entry.yaml", `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: parent
  namespace: flux-system
spec:
  sourceRef:
    kind: GitRepository
    name: platform
  path: ${ROOT_PATH}
  postBuild:
    substituteFrom:
      - kind: ConfigMap
        name: vars
`)
	writeFixture(t, root, "vars.yaml", `apiVersion: v1
kind: ConfigMap
metadata:
  name: vars
  namespace: flux-system
data:
  CHILD_DIR: child
  APP_NAME: rendered
  ROOT_PATH: apps
`)
	writeFixture(t, root, "source/apps/kustomization.yaml", "resources:\n  - child.yaml\n  - namespace.yaml\n")
	writeFixture(t, root, "source/apps/child.yaml", `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: child
  namespace: flux-system
spec:
  sourceRef:
    kind: GitRepository
    name: platform
  path: children/${CHILD_DIR}
`)
	writeFixture(t, root, "source/apps/namespace.yaml", "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: ${APP_NAME}\n")
	writeFixture(t, root, "source/children/child/kustomization.yaml", "resources:\n  - namespace.yaml\n")
	writeFixture(t, root, "source/children/child/namespace.yaml", "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: child\n")
	planPath := writeFixture(t, root, "plan.yaml", `apiVersion: gitops-local-render.dev/v1alpha1
kind: RenderPlan
entrypoint:
  path: entry.yaml
  renderer: raw
sources:
  - name: platform
    path: source
    selectors:
      flux:
        - kind: GitRepository
          namespace: flux-system
          name: platform
adapters:
  flux:
    seedObjects:
      - vars.yaml
    entrypoint:
      namespace: flux-system
      substituteFrom:
        - kind: ConfigMap
          name: vars
`)

	result := runPlan(t, planPath)
	if len(result.Units) != 2 || result.Units[0] != "flux:flux-system/parent" || result.Units[1] != "flux:flux-system/child" {
		t.Fatalf("unexpected units: %v", result.Units)
	}
	if !containsObject(result, "Namespace", "rendered") || !containsObject(result, "Namespace", "child") {
		t.Fatalf("expected rendered namespaces, got %#v", result.Objects)
	}
}

func TestFluxGraphAppliesInlinePatchBeforeRecursiveDiscovery(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "entry.yaml", `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: parent
  namespace: flux-system
spec:
  sourceRef:
    kind: GitRepository
    name: platform
  path: parent
  patches:
    - target:
        group: kustomize.toolkit.fluxcd.io
        version: v1
        kind: Kustomization
        name: child
      patch: |
        - op: replace
          path: /spec/path
          value: patched-child
`)
	writeFixture(t, root, "source/parent/kustomization.yaml", "resources:\n  - child.yaml\n")
	writeFixture(t, root, "source/parent/child.yaml", `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: child
  namespace: flux-system
spec:
  sourceRef:
    kind: GitRepository
    name: platform
  path: original-child
`)
	writeFixture(t, root, "source/patched-child/kustomization.yaml", "resources:\n  - namespace.yaml\n")
	writeFixture(t, root, "source/patched-child/namespace.yaml", "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: patched-child\n")
	planPath := writeFixture(t, root, "plan.yaml", `apiVersion: gitops-local-render.dev/v1alpha1
kind: RenderPlan
entrypoint:
  path: entry.yaml
  renderer: raw
sources:
  - name: platform
    path: source
    selectors:
      flux:
        - kind: GitRepository
          namespace: flux-system
          name: platform
`)

	result := runPlan(t, planPath)
	if len(result.Units) != 2 || !containsObject(result, "Namespace", "patched-child") {
		t.Fatalf("expected patched child path to be discovered, got units=%v objects=%#v", result.Units, result.Objects)
	}
}

func TestArgoAppOfAppsRendersRecursively(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "entry.yaml", `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: parent
  namespace: argocd
spec:
  source:
    repoURL: https://example.test/platform.git
    targetRevision: HEAD
    path: apps
`)
	writeFixture(t, root, "source/apps/kustomization.yaml", "resources:\n  - child.yaml\n")
	writeFixture(t, root, "source/apps/child.yaml", `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: child
  namespace: argocd
spec:
  source:
    repoURL: https://example.test/platform.git
    targetRevision: HEAD
    path: child
`)
	writeFixture(t, root, "source/child/kustomization.yaml", "resources:\n  - namespace.yaml\n")
	writeFixture(t, root, "source/child/namespace.yaml", "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: argocd-child\n")
	planPath := writeFixture(t, root, "plan.yaml", `apiVersion: gitops-local-render.dev/v1alpha1
kind: RenderPlan
entrypoint:
  path: entry.yaml
  renderer: raw
sources:
  - name: platform
    path: source
    selectors:
      argocd:
        - repoURL: https://example.test/platform.git
`)

	result := runPlan(t, planPath)
	if len(result.Units) != 2 || !containsObject(result, "Namespace", "argocd-child") {
		t.Fatalf("unexpected result: units=%v objects=%#v", result.Units, result.Objects)
	}
}

func runPlan(t *testing.T, planPath string) *engine.Result {
	t.Helper()
	plan, err := config.Load(planPath)
	if err != nil {
		t.Fatal(err)
	}
	renderers, err := render.NewRegistry(render.Kustomize{}, render.Raw{})
	if err != nil {
		t.Fatal(err)
	}
	fluxOptions := plan.Adapters.Flux.Entrypoint
	flux := fluxadapter.Adapter{}
	if fluxOptions != nil {
		references := make([]fluxadapter.SubstituteReference, 0, len(fluxOptions.SubstituteFrom))
		for _, reference := range fluxOptions.SubstituteFrom {
			references = append(references, fluxadapter.SubstituteReference{Kind: reference.Kind, Name: reference.Name, Optional: reference.Optional})
		}
		flux.Entrypoint = &fluxadapter.Entrypoint{Namespace: fluxOptions.Namespace, SubstituteFrom: references, Substitute: fluxOptions.Substitute, SubstituteStrategy: fluxOptions.SubstituteStrategy}
	}
	application, err := engine.New(plan, renderers, flux, argocdadapter.Adapter{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := application.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func containsObject(result *engine.Result, kind, name string) bool {
	for _, object := range result.Objects {
		if object.Kind() == kind && object.Name() == name {
			return true
		}
	}
	return false
}

func writeFixture(t *testing.T, root, relative, contents string) string {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
