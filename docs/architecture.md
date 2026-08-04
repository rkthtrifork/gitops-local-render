# Architecture

## Goal

The project renders the local desired-state graph described by GitOps controller resources. It reproduces manifest generation and controller-owned transformations without applying resources or requiring a live cluster.

## Data flow

```text
RenderPlan
  ├─ entrypoint
  ├─ exact local source mappings
  └─ adapter inputs such as Flux seed objects
          │
          ▼
initial renderer ──> optional adapter entrypoint transforms ──> adapter discovery
                                              │
                                              ▼
                                      deployment-unit graph
                                              │
                              ┌───────────────┴───────────────┐
                              ▼                               ▼
                        source resolver                 dependency order
                              │
                              ▼
                      renderer registry
                              │
                              ▼
                    adapter transformation
                              │
                              ├─> object state
                              ├─> deterministic output
                              └─> recursive discovery
```

The core engine owns traversal, ordering, duplicate identity policy, source resolution calls, and output. It does not switch on controller types.

## Comparison architecture

Comparison operates on two fully rendered engine results. It matches Kubernetes objects by API version, kind, namespace, name, and duplicate occurrence; compares their parsed data rather than serialized YAML; and retains the deployment-unit provenance recorded by the engine. Secret value fields are compared through a redacted path that records change without retaining values in the report.

The comparison command accepts two prepared workspaces and does not inspect or mutate Git state. A caller may prepare those directories from Git worktrees, CI artifacts, or another source, but that orchestration remains outside the renderer.

Render-plan paths may be resolved from an explicit render root. This keeps plans independent of where a prepared artifact is materialized without weakening the existing source jail around each mapped source.

## Extension contracts

The public contracts in `pkg/api` deliberately separate two extension families.

### GitOps adapters

An adapter owns capabilities whose semantics come from a GitOps controller:

1. **Discovery:** recognize controller resources and turn them into deployment units.
2. **Source addressing:** translate controller-specific references, such as a Flux `sourceRef` or Argo CD `repoURL`, into a generic source query.
3. **Composition:** produce one or more ordered render requests for a deployment unit, including typed renderer-language inputs such as an upstream-generated Kustomization manifest.
4. **Renderer selection:** select Kustomize, raw YAML, Helm, Jsonnet, or a controller plugin based on the controller CR.
5. **Transformation:** implement controller post-render behavior such as Flux post-build substitution.
6. **Dependency semantics:** translate controller ordering fields into deployment-unit dependencies.
7. **Recursive generation:** discover child units such as Flux Kustomizations or Argo CD Applications in transformed output.
8. **State requirements:** request exact objects from the read-only object lookup when controller behavior depends on cluster-like inputs.
9. **Entrypoint transformation:** optionally reproduce a controller-owned bootstrap or generator phase before the first discovery pass.

Future adapters for other controllers use the same operations. A new semantic that is controller-specific belongs on the adapter side of the boundary; it should not become a special case in the engine.

ApplicationSet-style generators fit this model as adapter discovery expansion: they generate concrete deployment units before renderer planning. Cluster-backed generators will require an explicit state-provider contract because they cannot be reproduced from local files alone.

### Manifest renderers

A renderer owns a configuration-management language independent of the controller that selected it. Current renderers are Kustomize and raw YAML. Helm, Jsonnet, CUE, Carvel ytt, and external commands can be added by registering another implementation.

Renderers receive a confined local source and may not fetch undeclared content. External command support should use a versioned stdin/stdout protocol rather than Go runtime plugins, which require exact compiler and dependency compatibility.

For Flux Kustomizations, the adapter delegates controller-owned pre-build translation to the pinned `github.com/fluxcd/pkg/kustomize` generator. The resulting Kustomization is passed as a typed render option, overlaid only in the jailed in-memory filesystem, and built through Flux's wrapper around upstream Kustomize. This preserves the adapter/renderer boundary without reimplementing Flux fields or mutating the checkout.

### Services that remain controller-neutral

Source acquisition, decryption, schema validation, policy checks, output formats, and object-state providers are pipeline services rather than adapter implementations. Adapters may declare or request those capabilities, but implementations stay reusable. For example, Flux and another controller could both request SOPS decryption without embedding SOPS in either adapter.

Add such a service only with a real consumer. The current engine provides local source resolution and a read-only object store because the implemented adapters require them.

## Strict behavior

The tool validates the render plan with unknown-field rejection. Selector ambiguity, source escapes, unsupported controller APIs or options, missing required values, graph cycles, and duplicate rendered identities fail the render. A plan may explicitly preserve duplicate identities from independently owned reconciliation units or select last-wins composition. Unmapped sources also fail unless a plan explicitly selects the partial-graph policy `strict.unmappedSource: ignore`; ignored units are reported separately.

The Kustomize renderer uses an in-process filesystem jail with unrestricted Kustomize load traversal inside the mapped source. This permits normal `../base` layouts while preventing reads outside the source, including symlink escapes.

## Adding an adapter

1. Implement `api.Adapter` in `internal/adapter/<controller>`.
2. Add typed selector configuration and exact matching to the render plan.
3. Register the adapter in the CLI.
4. Add end-to-end graph tests and focused conformance tests for controller-specific precedence and failures.
5. Report implemented behavior through `Capabilities`; do not report planned behavior.
6. Update this document and runnable examples if the new adapter adds a new extension pattern.

## Adding a renderer

1. Implement `api.Renderer`.
2. Make detection deterministic and free of external state.
3. Enforce the mapped source boundary for every filesystem access.
4. Register it after considering auto-detection precedence.
5. Add focused tests for detection, rendering, and source escape behavior.
