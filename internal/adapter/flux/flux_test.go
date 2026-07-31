package flux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rkthtrifork/gitops-local-render/internal/manifest"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

type lookup map[string]api.Object

type fixedResolver struct {
	source api.LocalSource
}

func (r fixedResolver) Resolve(api.SourceQuery) (api.LocalSource, error) {
	return r.source, nil
}

func (l lookup) Get(kind, namespace, name string) (api.Object, bool) {
	object, found := l[kind+"/"+namespace+"/"+name]
	return object, found
}

func TestPlanUsesUpstreamFluxKustomizeGenerator(t *testing.T) {
	root := t.TempDir()
	writeFluxFile(t, filepath.Join(root, "apps", "base", "kustomization.yaml"), "resources:\n  - namespace.yaml\n")
	writeFluxFile(t, filepath.Join(root, "apps", "base", "namespace.yaml"), "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: app\n")
	writeFluxFile(t, filepath.Join(root, "apps", "local", "kustomization.yaml"), "apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\nresources: []\n")
	unit := mustFluxUnit(t, `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: app
  namespace: flux-system
spec:
  sourceRef:
    kind: OCIRepository
    name: platform
  path: apps/base
  components:
    - ../local
  ignoreMissingComponents: true
  targetNamespace: generated
  namePrefix: flux-
  buildMetadata:
    - originAnnotations
`)
	requests, err := (Adapter{}).Plan(unit, fixedResolver{source: api.LocalSource{Name: "platform", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].Kustomize == nil {
		t.Fatalf("expected one Kustomize request, got %#v", requests)
	}
	if requests[0].Renderer != "kustomize" {
		t.Fatalf("expected explicit Kustomize renderer, got %q", requests[0].Renderer)
	}
	options := requests[0].Kustomize
	if options.KustomizationFile != "kustomization.yaml" {
		t.Fatalf("unexpected generated Kustomization file: %q", options.KustomizationFile)
	}
	generated := string(options.Kustomization)
	for _, expected := range []string{"namespace: generated", "namePrefix: flux-", "- ../local", "- originAnnotations"} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("expected generated Kustomization to contain %q:\n%s", expected, generated)
		}
	}
}

func TestPlanRejectsNonStringComponent(t *testing.T) {
	root := t.TempDir()
	writeFluxFile(t, filepath.Join(root, "kustomization.yaml"), "resources: []\n")
	unit := mustFluxUnit(t, `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: app
spec:
  sourceRef:
    kind: OCIRepository
    name: platform
  components:
    - 42
`)
	_, err := (Adapter{}).Plan(unit, fixedResolver{source: api.LocalSource{Name: "platform", Path: root}})
	if err == nil || !strings.Contains(err.Error(), ".spec.components accessor error") {
		t.Fatalf("expected component type error, got %v", err)
	}
}

func TestPlanRejectsUpstreamGeneratorSourceEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "source")
	outside := filepath.Join(parent, "outside")
	writeFluxFile(t, filepath.Join(outside, "kustomization.yaml"), "resources: []\n")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escaped")); err != nil {
		t.Fatal(err)
	}
	unit := mustFluxUnit(t, `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: app
spec:
  sourceRef:
    kind: OCIRepository
    name: platform
  path: escaped
`)

	_, err := (Adapter{}).Plan(unit, fixedResolver{source: api.LocalSource{Name: "platform", Path: root}})
	if err == nil {
		t.Fatal("expected upstream generator source escape error")
	}
}

func TestTransformUsesFluxPrecedenceAndDisablesPerObject(t *testing.T) {
	unit := mustFluxUnit(t, `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: app
  namespace: flux-system
spec:
  postBuild:
    substituteFrom:
      - kind: ConfigMap
        name: first
      - kind: ConfigMap
        name: second
    substitute:
      VALUE: inline
`)
	first := mustFluxObject(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: first\n  namespace: flux-system\ndata:\n  VALUE: first\n")
	second := mustFluxObject(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: second\n  namespace: flux-system\ndata:\n  VALUE: second\n")
	input, err := manifest.Parse([]byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: enabled
data:
  value: ${VALUE}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: disabled
  annotations:
    kustomize.toolkit.fluxcd.io/substitute: disabled
data:
  value: ${MISSING}
`))
	if err != nil {
		t.Fatal(err)
	}

	objects, err := (Adapter{}).Transform(unit, []api.RenderResult{{Objects: input}}, lookup{
		"ConfigMap/flux-system/first":  first,
		"ConfigMap/flux-system/second": second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := objects[0].Data["data"].(map[string]any)["value"]; got != "inline" {
		t.Fatalf("expected inline precedence, got %v", got)
	}
	if got := objects[1].Data["data"].(map[string]any)["value"]; got != "${MISSING}" {
		t.Fatalf("expected disabled substitution to be preserved, got %v", got)
	}
}

func TestTransformFailsOnMissingVariable(t *testing.T) {
	unit := mustFluxUnit(t, `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: app
spec:
  postBuild:
    substituteStrategy: Always
`)
	input := []api.Object{mustFluxObject(t, "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: ${MISSING}\n")}

	_, err := (Adapter{}).Transform(unit, []api.RenderResult{{Objects: input}}, lookup{})
	if err == nil || !strings.Contains(err.Error(), "variable not set") {
		t.Fatalf("expected strict substitution error, got %v", err)
	}
}

func mustFluxUnit(t *testing.T, yaml string) api.Unit {
	t.Helper()
	return api.Unit{Object: mustFluxObject(t, yaml)}
}

func writeFluxFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustFluxObject(t *testing.T, yaml string) api.Object {
	t.Helper()
	objects, err := manifest.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 {
		t.Fatalf("expected one object, got %d", len(objects))
	}
	return objects[0]
}
