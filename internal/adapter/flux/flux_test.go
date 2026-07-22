package flux

import (
	"strings"
	"testing"

	"github.com/rkthtrifork/gitops-local-render/internal/manifest"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

type lookup map[string]api.Object

func (l lookup) Get(kind, namespace, name string) (api.Object, bool) {
	object, found := l[kind+"/"+namespace+"/"+name]
	return object, found
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
