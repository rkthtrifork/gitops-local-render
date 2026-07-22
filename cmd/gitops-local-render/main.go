package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	argocdadapter "github.com/rkthtrifork/gitops-local-render/internal/adapter/argocd"
	fluxadapter "github.com/rkthtrifork/gitops-local-render/internal/adapter/flux"
	"github.com/rkthtrifork/gitops-local-render/internal/config"
	"github.com/rkthtrifork/gitops-local-render/internal/engine"
	"github.com/rkthtrifork/gitops-local-render/internal/manifest"
	"github.com/rkthtrifork/gitops-local-render/internal/render"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

var version = "dev"

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("a command is required")
	}
	switch args[0] {
	case "render":
		return runRender(ctx, args[1:], stdout, stderr)
	case "capabilities":
		return runCapabilities(args[1:], stdout)
	case "version":
		if len(args) != 1 {
			return errors.New("version accepts no arguments")
		}
		fmt.Fprintln(stdout, version)
		return nil
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runRender(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("render", flag.ContinueOnError)
	flags.SetOutput(stderr)
	planPath := flags.String("plan", "", "path to a RenderPlan")
	outputPath := flags.String("output", "-", "output YAML path, or - for stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("render accepts no positional arguments")
	}
	if *planPath == "" {
		return errors.New("--plan is required")
	}

	plan, err := config.Load(*planPath)
	if err != nil {
		return err
	}
	app, err := newEngine(plan)
	if err != nil {
		return err
	}
	result, err := app.Run(ctx)
	if err != nil {
		return err
	}
	data, err := manifest.Marshal(result.Objects)
	if err != nil {
		return err
	}
	if err := writeOutput(*outputPath, data, stdout); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "rendered %d objects from %d deployment units; skipped %d units with explicitly ignored sources\n", len(result.Objects), len(result.Units), len(result.Skipped))
	for _, unit := range result.Skipped {
		fmt.Fprintf(stderr, "skipped %s\n", unit)
	}
	return nil
}

func runCapabilities(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return errors.New("capabilities accepts no arguments")
	}
	renderers, err := newRenderers()
	if err != nil {
		return err
	}
	capabilities := map[string]map[string][]api.Capability{
		"adapters": {
			"argocd": argocdadapter.Adapter{}.Capabilities(),
			"flux":   fluxadapter.Adapter{}.Capabilities(),
		},
		"renderers": renderers.Capabilities(),
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(capabilities)
}

func newEngine(plan *config.Plan) (*engine.Engine, error) {
	renderers, err := newRenderers()
	if err != nil {
		return nil, err
	}
	return engine.New(plan, renderers, newFluxAdapter(plan), argocdadapter.Adapter{})
}

func newFluxAdapter(plan *config.Plan) fluxadapter.Adapter {
	options := plan.Adapters.Flux.Entrypoint
	if options == nil {
		return fluxadapter.Adapter{}
	}
	references := make([]fluxadapter.SubstituteReference, 0, len(options.SubstituteFrom))
	for _, reference := range options.SubstituteFrom {
		references = append(references, fluxadapter.SubstituteReference{
			Kind: reference.Kind, Name: reference.Name, Optional: reference.Optional,
		})
	}
	return fluxadapter.Adapter{Entrypoint: &fluxadapter.Entrypoint{
		Namespace:          options.Namespace,
		SubstituteFrom:     references,
		Substitute:         options.Substitute,
		SubstituteStrategy: options.SubstituteStrategy,
	}}
}

func newRenderers() (*render.Registry, error) {
	return render.NewRegistry(render.Kustomize{}, render.Raw{})
}

func writeOutput(path string, data []byte, stdout io.Writer) error {
	if path == "-" {
		_, err := stdout.Write(data)
		return err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(absPath), ".gitops-local-render-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, absPath); err != nil {
		return err
	}
	return nil
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, `gitops-local-render renders local GitOps deployment graphs.

Usage:
  gitops-local-render render --plan PLAN [--output FILE]
  gitops-local-render capabilities
  gitops-local-render version`)
}
