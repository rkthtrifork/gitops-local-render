package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComparePreparedWorkspacesWithoutGit(t *testing.T) {
	base := t.TempDir()
	head := t.TempDir()
	planDirectory := t.TempDir()
	plan := writeCLIFile(t, planDirectory, "plan.yaml", rawPlan())
	writeCLIFile(t, base, "manifests/config.yaml", configMap("same"))
	writeCLIFile(t, head, "manifests/config.yaml", configMap("same"))

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"compare", "--plan", plan, "--base-workspace", base, "--head-workspace", head,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Summary: 0 added, 0 removed, 0 changed") {
		t.Fatalf("unexpected comparison output: %s", stdout.String())
	}
}

func TestCompareRequiresTwoWorkspaces(t *testing.T) {
	planDirectory := t.TempDir()
	plan := writeCLIFile(t, planDirectory, "plan.yaml", rawPlan())

	err := run(context.Background(), []string{"compare", "--plan", plan, "--base-workspace", t.TempDir()}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--base-workspace and --head-workspace are required") {
		t.Fatalf("expected workspace validation error, got %v", err)
	}
}

func TestRenderAcceptsExplicitRenderRoot(t *testing.T) {
	workspace := t.TempDir()
	planDirectory := t.TempDir()
	plan := writeCLIFile(t, planDirectory, "plan.yaml", rawPlan())
	writeCLIFile(t, workspace, "manifests/config.yaml", configMap("rendered"))

	var stdout bytes.Buffer
	if err := run(context.Background(), []string{
		"render", "--plan", plan, "--render-root", workspace,
	}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "name: settings") {
		t.Fatalf("render did not use explicit root: %s", stdout.String())
	}
}

func TestCompareResolvesPlanPathsFromRenderRoot(t *testing.T) {
	base := t.TempDir()
	head := t.TempDir()
	planDirectory := t.TempDir()
	plan := writeCLIFile(t, planDirectory, "plan.yaml", rawPlan())
	writeCLIFile(t, base, "artifact/manifests/config.yaml", configMap("base"))
	writeCLIFile(t, head, "artifact/manifests/config.yaml", configMap("head"))

	err := run(context.Background(), []string{
		"compare", "--plan", plan, "--base-workspace", base, "--head-workspace", head, "--render-root", "artifact",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, errComparisonDifferent) {
		t.Fatalf("expected render-root comparison difference, got %v", err)
	}
}

func writeCLIFile(t *testing.T, root, relative, contents string) string {
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

func rawPlan() string {
	return `apiVersion: gitops-local-render.dev/v1alpha1
kind: RenderPlan
entrypoint:
  path: manifests
  renderer: raw
  recursive: true
`
}

func configMap(value string) string {
	return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  namespace: apps\ndata:\n  value: " + value + "\n"
}
