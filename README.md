# gitops-local-render

`gitops-local-render` renders a graph of GitOps deployment resources from local checkouts without contacting a Kubernetes cluster or fetching remote sources. It uses exact, explicit mappings between controller source identities and local directories.

The initial adapters support:

- Flux `kustomize.toolkit.fluxcd.io/v1` Kustomizations, including `dependsOn`, Kustomize components, `ignoreMissingComponents`, inline strategic-merge and JSON 6902 patches, strict post-build substitution, `substituteFrom`, inline substitution, substitution strategies, and per-object substitution disabling.
- Argo CD `argoproj.io/v1alpha1` Applications using local Kustomize or raw YAML sources, including multiple sources and app-of-apps recursion.

The executable fails when behavior cannot be reproduced exactly enough. It does not silently fetch sources, read process environment variables, skip required substitutions, or approximate unsupported GitOps features.

Flux pre-build Kustomization generation is delegated to the pinned `github.com/fluxcd/pkg/kustomize` package, and rendering uses its build wrapper around upstream Kustomize. Generated Kustomization files are held in memory and never written into the source checkout.

## Install

Build locally with Go 1.26.5 or newer:

```sh
make build
./bin/gitops-local-render version
```

Tagged GitHub releases attach archives for Linux, macOS, and Windows on amd64 and arm64. The same tag publishes a multi-platform image at:

```text
ghcr.io/rkthtrifork/gitops-local-render:<version>
```

## Render

Create a render plan that maps GitOps source identities to local checkouts:

```yaml
apiVersion: gitops-local-render.dev/v1alpha1
kind: RenderPlan

entrypoint:
  path: ./bootstrap

sources:
  - name: platform
    path: .
    selectors:
      flux:
        - kind: GitRepository
          namespace: flux-system
          name: platform
      argocd:
        - repoURL: https://github.com/example/platform.git

adapters:
  flux:
    seedObjects:
      - ./environments/dev/vars.yaml
    entrypoint:
      namespace: flux-system
      substituteFrom:
        - kind: ConfigMap
          name: bootstrap-vars
```

Paths are resolved relative to the plan file. Render to stdout or atomically to a file:

```sh
gitops-local-render render --plan render-plan.yaml
gitops-local-render render --plan render-plan.yaml --output build/all.yaml
```

Use `--render-root` when the plan is stored separately from the prepared artifact. All relative entrypoint, source, and seed paths then resolve beneath that explicit directory while retaining normal source confinement:

```sh
gitops-local-render render \
  --plan scripts/render-plans/platform.yaml \
  --render-root build/platform \
  --output build/platform/all.yaml
```

`adapters.flux.entrypoint` is optional. It models the bootstrap reconciliation that substitutes top-level Flux Kustomizations before they are discovered. Its references resolve only against explicitly loaded seed objects; normal Flux Kustomizations resolve against seed objects plus objects already rendered into the graph.

Run the container with all referenced local sources mounted beneath the plan’s directory:

```sh
docker run --rm \
  --volume "$PWD:/workspace:ro" \
  ghcr.io/rkthtrifork/gitops-local-render:1.0.0 \
  render --plan /workspace/render-plan.yaml
```

Runnable Flux and Argo CD plans are under [`examples/`](examples/).

## Compare changes

Compare two prepared workspaces without involving Git or a repository-specific build system:

```sh
gitops-local-render compare \
  --plan render-plan.yaml \
  --base-workspace build/base \
  --head-workspace build/head
```

Use `--render-root` when both workspaces contain the prepared artifact below a shared relative directory:

```sh
gitops-local-render compare \
  --plan scripts/render-plans/platform.yaml \
  --base-workspace build/base \
  --head-workspace build/head \
  --render-root build/platform
```

Comparison is semantic: objects are matched by Kubernetes identity, YAML formatting and map-key order are ignored, and field changes use JSON Pointer paths. Summary output includes producing deployment units. `--format json` emits structured output for CI and agents. Secret `data` and `stringData` changes are reported but their values are always redacted. Preparing the two directories—including creating Git worktrees, if desired—is the caller's responsibility.

Exit status is `0` when the renders are equivalent, `1` when valid desired states differ, and `2` for configuration or rendering errors.

## Strictness and trust boundaries

- A source selector must match exactly one local source.
- Kustomize may load bases across directories within a mapped source but may not escape its root, including through symlinks.
- Flux substitutions never fall back to the executable's environment.
- Missing non-optional Flux substitution objects and variables are errors.
- Duplicate rendered Kubernetes identities are errors by default. Use `last-wins` only for a controller composition rule such as Argo CD multiple sources. Use `preserve` for a diagnostic bundle where separate reconciliation units intentionally emit the same identity and ownership must remain visible.
- Unmapped controller sources are errors by default. A partial graph must opt into `strict.unmappedSource: ignore`; skipped deployment units are counted in diagnostics.
- Unknown render-plan fields and unsupported API versions or renderer options are errors.

See [security and secret handling](docs/security.md) before supplying Secret seed objects.

## Current boundaries

The following capabilities are deliberately not approximated yet:

- Argo CD Helm, Jsonnet, config-management plugins, Kustomize-specific options, and ApplicationSets.
- Flux SOPS decryption and cluster-backed substitution lookup.
- Remote source fetching, apply, health checks, drift detection, or Kubernetes API validation.

Attempting to select an unregistered renderer such as `helm`, `jsonnet`, or `argocd-cmp` produces an error. The [architecture](docs/architecture.md) explains how these capabilities extend the project without coupling them to a particular GitOps controller.

## Development

```sh
make format
make test
make verify
```

Repository operating guidance is in [AGENTS.md](AGENTS.md). Release steps are in [docs/releasing.md](docs/releasing.md).
