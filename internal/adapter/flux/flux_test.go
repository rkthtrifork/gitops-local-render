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

func TestTransformAppliesFluxPatchesBeforePostBuildSubstitution(t *testing.T) {
	unit := mustFluxUnit(t, `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: app
spec:
  patches:
    - target:
        group: batch
        version: v1
        kind: CronJob
        name: (configure-harbor|rotate-harbor-robots)
      patch: |
        apiVersion: batch/v1
        kind: CronJob
        metadata:
          name: ignored-by-target
        spec:
          suspend: true
          schedule: ${SCHEDULE}
    - target:
        group: rbac.authorization.k8s.io
        version: v1
        kind: ClusterRole
        name: tenant-admin
      patch: |
        - op: add
          path: /rules/-
          value:
            apiGroups:
              - harbor.harbor-operator.io
            resources:
              - projects
            verbs:
              - get
    - target:
        group: batch
        version: v1
        kind: Job
        name: bootstrap
      patch: |
        $patch: delete
        apiVersion: batch/v1
        kind: Job
        metadata:
          name: bootstrap
  postBuild:
    substitute:
      SCHEDULE: 0 1 * * *
`)
	input, err := manifest.Parse([]byte(`apiVersion: batch/v1
kind: CronJob
metadata:
  name: configure-harbor
  namespace: harbor-system
spec:
  schedule: "0 0 * * *"
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: rotate-harbor-robots
  namespace: harbor-system
spec:
  schedule: "0 0 * * *"
---
apiVersion: batch/v1
kind: Job
metadata:
  name: bootstrap
  namespace: harbor-system
spec: {}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: tenant-admin
rules:
  - apiGroups:
      - ""
    resources:
      - pods
    verbs:
      - get
`))
	if err != nil {
		t.Fatal(err)
	}

	objects, err := (Adapter{}).Transform(unit, []api.RenderResult{{Objects: input}}, lookup{})
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 3 {
		t.Fatalf("expected deleted Job and three remaining objects, got %d", len(objects))
	}
	for _, object := range objects[:2] {
		spec := object.Data["spec"].(map[string]any)
		if spec["suspend"] != true || spec["schedule"] != "0 1 * * *" {
			t.Fatalf("expected patched and substituted CronJob, got %#v", spec)
		}
	}
	roleRules := objects[2].Data["rules"].([]any)
	if len(roleRules) != 2 {
		t.Fatalf("expected appended Harbor rule, got %#v", roleRules)
	}
	harborRule := roleRules[1].(map[string]any)
	if got := harborRule["apiGroups"].([]any)[0]; got != "harbor.harbor-operator.io" {
		t.Fatalf("expected Harbor API group, got %v", got)
	}
}

func TestTransformRejectsNonInlineFluxPatch(t *testing.T) {
	unit := mustFluxUnit(t, `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: app
spec:
  patches:
    - path: patch.yaml
`)

	_, err := (Adapter{}).Transform(unit, []api.RenderResult{{Objects: []api.Object{
		mustFluxObject(t, "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: app\n"),
	}}}, lookup{})
	if err == nil || !strings.Contains(err.Error(), "Flux patches must be inline") {
		t.Fatalf("expected inline patch error, got %v", err)
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
