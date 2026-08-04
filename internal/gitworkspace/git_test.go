package gitworkspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRepositorySupportsDirtyLiveWorktreeAndTemporaryRef(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	run(t, root, "git", "init", "-q")
	run(t, root, "git", "config", "user.email", "test@example.test")
	run(t, root, "git", "config", "user.name", "Test")
	write(t, root, "tracked.txt", "base\n")
	run(t, root, "git", "add", "tracked.txt")
	run(t, root, "git", "commit", "-qm", "base")

	repository, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	baseCommit, err := repository.Resolve(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "tracked.txt", "changed\n")
	write(t, root, "untracked.txt", "new\n")
	state, err := repository.State(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if state.Modified != 1 || state.Untracked != 1 {
		t.Fatalf("unexpected dirty state: %#v", state)
	}

	worktree, err := repository.AddWorktree(ctx, baseCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := worktree.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	data, err := os.ReadFile(filepath.Join(worktree.Path, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "base\n" {
		t.Fatalf("temporary worktree did not contain base revision: %q", data)
	}
}

func run(t *testing.T, directory, name string, arguments ...string) {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, output)
	}
}

func write(t *testing.T, root, relative, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, relative), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
