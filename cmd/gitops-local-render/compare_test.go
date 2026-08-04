package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareUsesDirtyLiveWorktreeAsHead(t *testing.T) {
	root := t.TempDir()
	initializeRepository(t, root)
	writeCLIFile(t, root, "plan.yaml", rawPlan())
	writeCLIFile(t, root, "manifests/config.yaml", configMap("old"))
	runCLICommand(t, root, "git", "add", ".")
	runCLICommand(t, root, "git", "commit", "-qm", "base")

	writeCLIFile(t, root, "manifests/config.yaml", configMap("new"))
	writeCLIFile(t, root, "manifests/untracked.yaml", "apiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: added\n  namespace: apps\n")

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"compare", "--repo", root, "--base-ref", "HEAD", "--plan", "plan.yaml",
	}, &stdout, &stderr)
	if !errors.Is(err, errComparisonDifferent) {
		t.Fatalf("expected a desired-state difference, got %v", err)
	}
	if !strings.Contains(stdout.String(), "Changed: ConfigMap apps/settings") || !strings.Contains(stdout.String(), "Added: ServiceAccount apps/added") {
		t.Fatalf("comparison did not include dirty and untracked changes:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Head: live worktree") || !strings.Contains(stderr.String(), "1 modified, 1 untracked") {
		t.Fatalf("comparison identity was not explicit:\n%s", stderr.String())
	}
}

func TestCompareRequireCleanRejectsDirtyHead(t *testing.T) {
	root := t.TempDir()
	initializeRepository(t, root)
	writeCLIFile(t, root, "plan.yaml", rawPlan())
	writeCLIFile(t, root, "manifests/config.yaml", configMap("old"))
	runCLICommand(t, root, "git", "add", ".")
	runCLICommand(t, root, "git", "commit", "-qm", "base")
	writeCLIFile(t, root, "manifests/config.yaml", configMap("new"))

	err := run(context.Background(), []string{
		"compare", "--repo", root, "--base-ref", "HEAD", "--plan", "plan.yaml", "--require-clean",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "live head worktree is dirty") {
		t.Fatalf("expected dirty-worktree error, got %v", err)
	}
}

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

func TestCompareRejectsPreparationThatChangesTrackedHeadFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test preparation command uses POSIX shell syntax")
	}
	root := t.TempDir()
	initializeRepository(t, root)
	writeCLIFile(t, root, "plan.yaml", rawPlan())
	writeCLIFile(t, root, "manifests/config.yaml", configMap("old"))
	runCLICommand(t, root, "git", "add", ".")
	runCLICommand(t, root, "git", "commit", "-qm", "base")

	err := run(context.Background(), []string{
		"compare", "--repo", root, "--base-ref", "HEAD", "--plan", "plan.yaml",
		"--prepare", "printf changed > manifests/config.yaml",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "preparation changed tracked files") {
		t.Fatalf("expected preparation-mutation error, got %v", err)
	}
}

func TestCompareCanUsePlanFromEachRef(t *testing.T) {
	root := t.TempDir()
	initializeRepository(t, root)
	writeCLIFile(t, root, "plan.yaml", rawPlan())
	writeCLIFile(t, root, "manifests/config.yaml", configMap("base"))
	runCLICommand(t, root, "git", "add", ".")
	runCLICommand(t, root, "git", "commit", "-qm", "base")

	writeCLIFile(t, root, "plan.yaml", `apiVersion: gitops-local-render.dev/v1alpha1
kind: RenderPlan
entrypoint:
  path: replacement.yaml
  renderer: raw
`)
	writeCLIFile(t, root, "replacement.yaml", "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: replacement\n")
	runCLICommand(t, root, "git", "add", ".")
	runCLICommand(t, root, "git", "commit", "-qm", "head")

	err := run(context.Background(), []string{
		"compare", "--repo", root, "--base-ref", "HEAD~1", "--head-ref", "HEAD",
		"--plan", "plan.yaml", "--plan-mode", "each-ref",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, errComparisonDifferent) {
		t.Fatalf("expected comparison using per-ref plans, got %v", err)
	}
}

func initializeRepository(t *testing.T, root string) {
	t.Helper()
	runCLICommand(t, root, "git", "init", "-q")
	runCLICommand(t, root, "git", "config", "user.email", "test@example.test")
	runCLICommand(t, root, "git", "config", "user.name", "Test")
}

func runCLICommand(t *testing.T, directory, name string, arguments ...string) {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, output)
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
