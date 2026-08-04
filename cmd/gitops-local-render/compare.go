package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rkthtrifork/gitops-local-render/internal/comparison"
	"github.com/rkthtrifork/gitops-local-render/internal/config"
	"github.com/rkthtrifork/gitops-local-render/internal/engine"
	"github.com/rkthtrifork/gitops-local-render/internal/gitworkspace"
)

var errComparisonDifferent = errors.New("rendered desired state differs")

type compareOptions struct {
	plan                    string
	repository              string
	baseRef                 string
	headRef                 string
	baseWorkspace           string
	headWorkspace           string
	prepare                 string
	renderRoot              string
	format                  string
	planMode                string
	requireClean            bool
	allowPreparationChanges bool
}

type comparisonSide struct {
	label      string
	workspace  string
	renderRoot string
	plan       string
	state      *gitworkspace.State
	close      func()
}

func runCompare(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseCompareOptions(args, stderr)
	if err != nil {
		return err
	}
	base, head, err := prepareComparisonSides(ctx, options, stderr)
	if err != nil {
		return err
	}
	defer base.close()
	defer head.close()

	printComparisonSide(stderr, "Base", base)
	printComparisonSide(stderr, "Head", head)
	fmt.Fprintf(stderr, "Plan: %s (%s mode)\n", options.plan, options.planMode)
	fmt.Fprintf(stderr, "Render root: %s\n", options.renderRoot)

	if err := prepareSides(ctx, options, base, head, stderr); err != nil {
		return err
	}
	baseResult, err := renderWorkspace(ctx, base.plan, base.renderRoot)
	if err != nil {
		return fmt.Errorf("render base %s: %w", base.label, err)
	}
	headResult, err := renderWorkspace(ctx, head.plan, head.renderRoot)
	if err != nil {
		return fmt.Errorf("render head %s: %w", head.label, err)
	}

	report := comparison.Compare(baseResult, headResult)
	switch options.format {
	case "summary":
		err = comparison.WriteSummary(stdout, report)
	case "json":
		err = comparison.WriteJSON(stdout, report)
	}
	if err != nil {
		return err
	}
	if report.Different() {
		return errComparisonDifferent
	}
	return nil
}

func parseCompareOptions(args []string, stderr io.Writer) (compareOptions, error) {
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := compareOptions{}
	flags.StringVar(&options.plan, "plan", "", "path to a RenderPlan")
	flags.StringVar(&options.repository, "repo", ".", "Git repository containing the live head worktree")
	flags.StringVar(&options.baseRef, "base-ref", "", "Git ref to render as the base")
	flags.StringVar(&options.headRef, "head-ref", "", "Git ref to render as the head instead of the live worktree")
	flags.StringVar(&options.baseWorkspace, "base-workspace", "", "prepared directory to render as the base")
	flags.StringVar(&options.headWorkspace, "head-workspace", "", "prepared directory to render as the head")
	flags.StringVar(&options.prepare, "prepare", "", "explicit shell command to run in each workspace before rendering")
	flags.StringVar(&options.renderRoot, "render-root", ".", "path beneath each workspace against which plan paths resolve")
	flags.StringVar(&options.format, "format", "summary", "comparison output format: summary or json")
	flags.StringVar(&options.planMode, "plan-mode", "current", "plan selection: current or each-ref")
	flags.BoolVar(&options.requireClean, "require-clean", false, "reject a dirty live head worktree")
	flags.BoolVar(&options.allowPreparationChanges, "allow-prepare-changes", false, "allow preparation to change tracked files in the live worktree")
	if err := flags.Parse(args); err != nil {
		return compareOptions{}, err
	}
	if flags.NArg() != 0 {
		return compareOptions{}, errors.New("compare accepts no positional arguments")
	}
	if options.plan == "" {
		return compareOptions{}, errors.New("--plan is required")
	}
	gitMode := options.baseRef != "" || options.headRef != ""
	workspaceMode := options.baseWorkspace != "" || options.headWorkspace != ""
	if gitMode == workspaceMode {
		return compareOptions{}, errors.New("select Git mode with --base-ref or workspace mode with --base-workspace and --head-workspace")
	}
	if gitMode && options.baseRef == "" {
		return compareOptions{}, errors.New("--base-ref is required in Git mode")
	}
	if workspaceMode && (options.baseWorkspace == "" || options.headWorkspace == "") {
		return compareOptions{}, errors.New("--base-workspace and --head-workspace are both required in workspace mode")
	}
	if options.format != "summary" && options.format != "json" {
		return compareOptions{}, errors.New("--format must be summary or json")
	}
	if options.planMode != "current" && options.planMode != "each-ref" {
		return compareOptions{}, errors.New("--plan-mode must be current or each-ref")
	}
	if workspaceMode && options.planMode != "current" {
		return compareOptions{}, errors.New("--plan-mode each-ref is only supported in Git mode")
	}
	if workspaceMode && options.requireClean {
		return compareOptions{}, errors.New("--require-clean is only supported in Git mode with a live head worktree")
	}
	if options.headRef != "" && options.requireClean {
		return compareOptions{}, errors.New("--require-clean is only meaningful when the head is the live worktree")
	}
	cleanRenderRoot := filepath.Clean(options.renderRoot)
	if filepath.IsAbs(options.renderRoot) || cleanRenderRoot == ".." || strings.HasPrefix(cleanRenderRoot, ".."+string(filepath.Separator)) {
		return compareOptions{}, errors.New("--render-root must be a relative path contained by each workspace")
	}
	options.renderRoot = cleanRenderRoot
	return options, nil
}

func prepareComparisonSides(ctx context.Context, options compareOptions, stderr io.Writer) (comparisonSide, comparisonSide, error) {
	noop := func() {}
	if options.baseWorkspace != "" {
		plan, err := filepath.Abs(options.plan)
		if err != nil {
			return comparisonSide{}, comparisonSide{}, err
		}
		base, err := existingDirectory(options.baseWorkspace)
		if err != nil {
			return comparisonSide{}, comparisonSide{}, fmt.Errorf("base workspace: %w", err)
		}
		head, err := existingDirectory(options.headWorkspace)
		if err != nil {
			return comparisonSide{}, comparisonSide{}, fmt.Errorf("head workspace: %w", err)
		}
		return comparisonSide{label: base, workspace: base, renderRoot: filepath.Join(base, options.renderRoot), plan: plan, close: noop}, comparisonSide{label: head, workspace: head, renderRoot: filepath.Join(head, options.renderRoot), plan: plan, close: noop}, nil
	}

	repository, err := gitworkspace.Open(ctx, options.repository)
	if err != nil {
		return comparisonSide{}, comparisonSide{}, err
	}
	planPath, planRelative, err := resolveRepositoryPlan(repository.Root, options.plan, options.planMode == "each-ref")
	if err != nil {
		return comparisonSide{}, comparisonSide{}, err
	}
	baseCommit, err := repository.Resolve(ctx, options.baseRef)
	if err != nil {
		return comparisonSide{}, comparisonSide{}, err
	}
	baseWorktree, err := repository.AddWorktree(ctx, baseCommit)
	if err != nil {
		return comparisonSide{}, comparisonSide{}, err
	}
	basePlan := planPath
	if options.planMode == "each-ref" {
		basePlan = filepath.Join(baseWorktree.Path, planRelative)
	}
	base := comparisonSide{
		label: options.baseRef + " @ " + shortCommit(baseCommit), workspace: baseWorktree.Path, renderRoot: filepath.Join(baseWorktree.Path, options.renderRoot), plan: basePlan,
		close: func() { warnClose(stderr, baseWorktree.Close(context.Background())) },
	}

	if options.headRef != "" {
		headCommit, err := repository.Resolve(ctx, options.headRef)
		if err != nil {
			base.close()
			return comparisonSide{}, comparisonSide{}, err
		}
		headWorktree, err := repository.AddWorktree(ctx, headCommit)
		if err != nil {
			base.close()
			return comparisonSide{}, comparisonSide{}, err
		}
		headPlan := planPath
		if options.planMode == "each-ref" {
			headPlan = filepath.Join(headWorktree.Path, planRelative)
		}
		return base, comparisonSide{
			label: options.headRef + " @ " + shortCommit(headCommit), workspace: headWorktree.Path, renderRoot: filepath.Join(headWorktree.Path, options.renderRoot), plan: headPlan,
			close: func() { warnClose(stderr, headWorktree.Close(context.Background())) },
		}, nil
	}

	state, err := repository.State(ctx, repository.Root)
	if err != nil {
		base.close()
		return comparisonSide{}, comparisonSide{}, err
	}
	if options.requireClean && state.Dirty() {
		base.close()
		return comparisonSide{}, comparisonSide{}, fmt.Errorf("live head worktree is dirty: %d modified, %d untracked", state.Modified, state.Untracked)
	}
	return base, comparisonSide{
		label: "live worktree", workspace: repository.Root, renderRoot: filepath.Join(repository.Root, options.renderRoot), plan: planPath, state: &state, close: noop,
	}, nil
}

func prepareSides(ctx context.Context, options compareOptions, base, head comparisonSide, stderr io.Writer) error {
	if options.prepare == "" {
		return nil
	}
	fmt.Fprintf(stderr, "Preparing base with: %s\n", options.prepare)
	if err := gitworkspace.RunPreparation(ctx, base.workspace, options.prepare, stderr); err != nil {
		return err
	}

	var before []byte
	var repository *gitworkspace.Repository
	if head.state != nil && !options.allowPreparationChanges {
		var err error
		repository, err = gitworkspace.Open(ctx, head.workspace)
		if err != nil {
			return err
		}
		before, err = repository.TrackedSnapshot(ctx, head.workspace)
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(stderr, "Preparing head with: %s\n", options.prepare)
	if err := gitworkspace.RunPreparation(ctx, head.workspace, options.prepare, stderr); err != nil {
		return err
	}
	if repository != nil {
		after, err := repository.TrackedSnapshot(ctx, head.workspace)
		if err != nil {
			return err
		}
		if !bytes.Equal(before, after) {
			return errors.New("preparation changed tracked files in the live worktree; revert those changes or pass --allow-prepare-changes")
		}
	}
	return nil
}

func renderWorkspace(ctx context.Context, planPath, workspace string) (*engine.Result, error) {
	plan, err := config.LoadWithOptions(planPath, config.LoadOptions{WorkspaceRoot: workspace})
	if err != nil {
		return nil, err
	}
	application, err := newEngine(plan)
	if err != nil {
		return nil, err
	}
	return application.Run(ctx)
}

func resolveRepositoryPlan(repositoryRoot, plan string, requireInsideRepository bool) (absolute, relative string, err error) {
	if filepath.IsAbs(plan) {
		absolute = filepath.Clean(plan)
	} else {
		absolute = filepath.Join(repositoryRoot, plan)
	}
	relative, err = filepath.Rel(repositoryRoot, absolute)
	if err != nil {
		return "", "", err
	}
	if requireInsideRepository && (relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return "", "", errors.New("Git comparison requires --plan to be inside the repository")
	}
	if _, err := os.Stat(absolute); err != nil {
		return "", "", fmt.Errorf("inspect plan: %w", err)
	}
	return absolute, relative, nil
}

func existingDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", absolute)
	}
	return absolute, nil
}

func printComparisonSide(output io.Writer, name string, side comparisonSide) {
	fmt.Fprintf(output, "%s: %s\n", name, side.label)
	if side.state != nil {
		fmt.Fprintf(output, "      HEAD %s, %d modified, %d untracked\n", shortCommit(side.state.Commit), side.state.Modified, side.state.Untracked)
	}
}

func shortCommit(commit string) string {
	if len(commit) <= 12 {
		return commit
	}
	return commit[:12]
}

func warnClose(output io.Writer, err error) {
	if err != nil {
		fmt.Fprintf(output, "warning: %v\n", err)
	}
}
