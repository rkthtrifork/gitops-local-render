package main

import (
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
)

var errComparisonDifferent = errors.New("rendered desired state differs")

type compareOptions struct {
	plan          string
	baseWorkspace string
	headWorkspace string
	renderRoot    string
	format        string
}

type comparisonSide struct {
	label      string
	workspace  string
	renderRoot string
}

func runCompare(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseCompareOptions(args, stderr)
	if err != nil {
		return err
	}
	base, head, err := comparisonSides(options)
	if err != nil {
		return err
	}

	fmt.Fprintf(stderr, "Base: %s\n", base.label)
	fmt.Fprintf(stderr, "Head: %s\n", head.label)
	fmt.Fprintf(stderr, "Plan: %s\n", options.plan)
	fmt.Fprintf(stderr, "Render root: %s\n", options.renderRoot)

	baseResult, err := renderWorkspace(ctx, options.plan, base.renderRoot)
	if err != nil {
		return fmt.Errorf("render base %s: %w", base.label, err)
	}
	headResult, err := renderWorkspace(ctx, options.plan, head.renderRoot)
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
	flags.StringVar(&options.baseWorkspace, "base-workspace", "", "prepared directory to render as the base")
	flags.StringVar(&options.headWorkspace, "head-workspace", "", "prepared directory to render as the head")
	flags.StringVar(&options.renderRoot, "render-root", ".", "path beneath each workspace against which plan paths resolve")
	flags.StringVar(&options.format, "format", "summary", "comparison output format: summary or json")
	if err := flags.Parse(args); err != nil {
		return compareOptions{}, err
	}
	if flags.NArg() != 0 {
		return compareOptions{}, errors.New("compare accepts no positional arguments")
	}
	if options.plan == "" {
		return compareOptions{}, errors.New("--plan is required")
	}
	if options.baseWorkspace == "" || options.headWorkspace == "" {
		return compareOptions{}, errors.New("--base-workspace and --head-workspace are required")
	}
	if options.format != "summary" && options.format != "json" {
		return compareOptions{}, errors.New("--format must be summary or json")
	}
	cleanRenderRoot := filepath.Clean(options.renderRoot)
	if filepath.IsAbs(options.renderRoot) || cleanRenderRoot == ".." || strings.HasPrefix(cleanRenderRoot, ".."+string(filepath.Separator)) {
		return compareOptions{}, errors.New("--render-root must be a relative path contained by each workspace")
	}
	options.renderRoot = cleanRenderRoot
	return options, nil
}

func comparisonSides(options compareOptions) (comparisonSide, comparisonSide, error) {
	plan, err := filepath.Abs(options.plan)
	if err != nil {
		return comparisonSide{}, comparisonSide{}, err
	}
	if _, err := os.Stat(plan); err != nil {
		return comparisonSide{}, comparisonSide{}, fmt.Errorf("inspect plan: %w", err)
	}
	base, err := existingDirectory(options.baseWorkspace)
	if err != nil {
		return comparisonSide{}, comparisonSide{}, fmt.Errorf("base workspace: %w", err)
	}
	head, err := existingDirectory(options.headWorkspace)
	if err != nil {
		return comparisonSide{}, comparisonSide{}, fmt.Errorf("head workspace: %w", err)
	}
	return comparisonSide{
			label:      base,
			workspace:  base,
			renderRoot: filepath.Join(base, options.renderRoot),
		}, comparisonSide{
			label:      head,
			workspace:  head,
			renderRoot: filepath.Join(head, options.renderRoot),
		}, nil
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
