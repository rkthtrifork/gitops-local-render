package gitworkspace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Repository struct {
	Root string
}

type State struct {
	Commit    string
	Modified  int
	Untracked int
}

func Open(ctx context.Context, path string) (*Repository, error) {
	output, err := runGit(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(strings.TrimSpace(string(output)))
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	return &Repository{Root: root}, nil
}

func (r *Repository) Resolve(ctx context.Context, ref string) (string, error) {
	output, err := runGit(ctx, r.Root, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve Git ref %q: %w", ref, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (r *Repository) State(ctx context.Context, path string) (State, error) {
	commit, err := runGit(ctx, path, "rev-parse", "HEAD")
	if err != nil {
		return State{}, err
	}
	status, err := runGit(ctx, path, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return State{}, err
	}
	state := State{Commit: strings.TrimSpace(string(commit))}
	for _, line := range strings.Split(strings.TrimSpace(string(status)), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "?? ") {
			state.Untracked++
			continue
		}
		state.Modified++
	}
	return state, nil
}

func (s State) Dirty() bool {
	return s.Modified > 0 || s.Untracked > 0
}

func (r *Repository) TrackedSnapshot(ctx context.Context, path string) ([]byte, error) {
	head, err := runGit(ctx, path, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("capture tracked worktree state: %w", err)
	}
	staged, err := runGit(ctx, path, "diff", "--binary", "--cached", "HEAD", "--", ".")
	if err != nil {
		return nil, fmt.Errorf("capture tracked worktree state: %w", err)
	}
	unstaged, err := runGit(ctx, path, "diff", "--binary", "--", ".")
	if err != nil {
		return nil, fmt.Errorf("capture tracked worktree state: %w", err)
	}
	snapshot := append(append(head, 0), staged...)
	return append(append(snapshot, 0), unstaged...), nil
}

type TemporaryWorktree struct {
	Path     string
	repoRoot string
	tempRoot string
}

func (r *Repository) AddWorktree(ctx context.Context, commit string) (*TemporaryWorktree, error) {
	tempRoot, err := os.MkdirTemp("", "gitops-local-render-worktree-")
	if err != nil {
		return nil, fmt.Errorf("create temporary worktree directory: %w", err)
	}
	path := filepath.Join(tempRoot, "checkout")
	if _, err := runGit(ctx, r.Root, "worktree", "add", "--detach", path, commit); err != nil {
		os.RemoveAll(tempRoot)
		return nil, fmt.Errorf("create temporary worktree for %s: %w", commit, err)
	}
	return &TemporaryWorktree{Path: path, repoRoot: r.Root, tempRoot: tempRoot}, nil
}

func (w *TemporaryWorktree) Close(ctx context.Context) error {
	_, removeErr := runGit(ctx, w.repoRoot, "worktree", "remove", "--force", w.Path)
	cleanupErr := os.RemoveAll(w.tempRoot)
	if removeErr != nil {
		return fmt.Errorf("remove temporary Git worktree: %w", removeErr)
	}
	if cleanupErr != nil {
		return fmt.Errorf("remove temporary directory: %w", cleanupErr)
	}
	return nil
}

func RunPreparation(ctx context.Context, directory, command string, output io.Writer) error {
	if command == "" {
		return nil
	}
	name, arguments := shellCommand(command)
	process := exec.CommandContext(ctx, name, arguments...)
	process.Dir = directory
	process.Stdout = output
	process.Stderr = output
	if err := process.Run(); err != nil {
		return fmt.Errorf("prepare workspace %q: %w", directory, err)
	}
	return nil
}

func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/d", "/s", "/c", command}
	}
	return "sh", []string{"-c", command}
}

func runGit(ctx context.Context, directory string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = directory
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(arguments, " "), message)
	}
	return output, nil
}
