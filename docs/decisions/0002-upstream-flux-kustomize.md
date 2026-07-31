# 0002: Delegate Flux Kustomization generation upstream

Status: accepted
Date: 2026-07-31

## Decision

Use `github.com/fluxcd/pkg/kustomize` for Flux-owned pre-build Kustomization generation and its build wrapper around upstream Kustomize. Pin the package to the version used by the target Flux kustomize-controller release.

The Flux adapter passes the complete Flux Kustomization to the upstream generator. The generated `kustomization.yaml` is overlaid in memory inside the source jail and built without modifying the checkout. Keep graph traversal, source mapping, object state, and post-build substitution in their existing project-owned boundaries.

## Rationale

Flux fields such as components, missing-component handling, patches, images, target namespaces, name transforms, build metadata, and automatic Kustomization generation jointly define the input to Kustomize. Reimplementing these translations creates controller-version drift even when the final build already uses upstream Kustomize.

The Flux generator exposes a non-writing `GenerateManifest` operation and uses a secure filesystem rooted at the declared source. Its generated manifest can therefore be combined with the renderer's stricter jailed filesystem and deterministic output pipeline.

## Consequences

- Flux pre-build behavior tracks the pinned upstream package instead of local translations.
- Flux package upgrades require conformance tests and representative repository renders.
- The dependency graph includes the Kubernetes libraries used by the upstream generator.
- Source files remain unchanged; generated Kustomizations exist only in memory.
- Post-build substitution and cluster-like object lookup remain local because they depend on explicitly seeded render state.
