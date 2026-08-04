package comparison

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rkthtrifork/gitops-local-render/internal/engine"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

func TestCompareReportsSemanticObjectChanges(t *testing.T) {
	base := result(
		record("base", object("v1", "ConfigMap", "apps", "changed", map[string]any{"value": "old"})),
		record("base", object("v1", "ConfigMap", "apps", "removed", nil)),
	)
	head := result(
		record("head", object("v1", "ConfigMap", "apps", "changed", map[string]any{"value": "new"})),
		record("head", object("v1", "ConfigMap", "apps", "added", nil)),
	)

	report := Compare(base, head)
	if len(report.Added) != 1 || report.Added[0].Object.Name != "added" {
		t.Fatalf("unexpected additions: %#v", report.Added)
	}
	if len(report.Removed) != 1 || report.Removed[0].Object.Name != "removed" {
		t.Fatalf("unexpected removals: %#v", report.Removed)
	}
	if len(report.Changed) != 1 || report.Changed[0].Fields[0].Path != "/data/value" {
		t.Fatalf("unexpected changes: %#v", report.Changed)
	}
}

func TestCompareRedactsSecretValues(t *testing.T) {
	base := result(record("unit", object("v1", "Secret", "apps", "credentials", map[string]any{"token": "old-secret"})))
	head := result(record("unit", object("v1", "Secret", "apps", "credentials", map[string]any{"token": "new-secret"})))

	report := Compare(base, head)
	if len(report.Changed) != 1 || len(report.Changed[0].Fields) != 1 || !report.Changed[0].Fields[0].Redacted {
		t.Fatalf("expected one redacted change, got %#v", report)
	}
	var output bytes.Buffer
	if err := WriteJSON(&output, report); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "old-secret") || strings.Contains(output.String(), "new-secret") {
		t.Fatalf("secret leaked in JSON: %s", output.String())
	}
}

func TestCompareDistinguishesAbsentFieldFromNull(t *testing.T) {
	baseObject := object("v1", "ConfigMap", "apps", "settings", nil)
	headObject := object("v1", "ConfigMap", "apps", "settings", nil)
	headObject.Data["data"] = nil

	report := Compare(result(record("unit", baseObject)), result(record("unit", headObject)))
	if len(report.Changed) != 1 || len(report.Changed[0].Fields) != 1 {
		t.Fatalf("expected absent-to-null change, got %#v", report)
	}
	change := report.Changed[0].Fields[0]
	if change.Path != "/data" || change.BeforePresent || !change.AfterPresent {
		t.Fatalf("unexpected presence change: %#v", change)
	}
}

func TestCompareReportsProvenanceChanges(t *testing.T) {
	shared := object("v1", "ConfigMap", "apps", "settings", nil)
	baseRecord := record("flux:apps/base", shared)
	headRecord := record("flux:apps/head", shared)
	headRecord.Sources = []string{"replacement"}

	report := Compare(result(baseRecord), result(headRecord))
	if len(report.Changed) != 1 {
		t.Fatalf("expected provenance-only change, got %#v", report)
	}
	change := report.Changed[0]
	if change.BaseUnit != "flux:apps/base" || change.HeadUnit != "flux:apps/head" || change.HeadSources[0] != "replacement" {
		t.Fatalf("unexpected provenance: %#v", change)
	}
}

func object(apiVersion, kind, namespace, name string, data map[string]any) api.Object {
	value := map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"namespace": namespace,
			"name":      name,
		},
	}
	if data != nil {
		value["data"] = data
	}
	return api.Object{Data: value}
}

func record(unit string, object api.Object) engine.ObjectRecord {
	return engine.ObjectRecord{Unit: unit, Sources: []string{"platform"}, Object: object}
}

func result(records ...engine.ObjectRecord) *engine.Result {
	objects := make([]api.Object, 0, len(records))
	for _, record := range records {
		objects = append(objects, record.Object)
	}
	return &engine.Result{Objects: objects, Records: records}
}
