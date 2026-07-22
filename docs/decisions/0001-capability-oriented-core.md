# 0001: Capability-oriented GitOps adapters

Status: accepted
Date: 2026-07-22

## Decision

Keep graph traversal and manifest rendering controller-neutral. Represent each GitOps controller as a compiled-in adapter that owns discovery, controller source addressing, renderer planning, transformations, dependencies, recursive generation, and state requirements. Keep configuration-management languages in separate renderer implementations.

Local-only mappings live in a strict render-plan file rather than comments or annotations on controller resources.

## Rationale

Flux and Argo CD share graph traversal and manifest generators but differ in source identity, composition, templating, and dependency semantics. A controller-neutral engine avoids encoding Flux behavior as generic behavior and gives future controllers a complete extension boundary.

Comments do not reliably survive Kustomize rendering, while annotations modify cluster-visible resources. A sidecar plan remains available before and after rendering and maps a source once rather than repeating local paths across controller resources.

Compiled-in Go interfaces are type-safe and reproducible. Go runtime plugins require exact compiler and dependency compatibility. A versioned process protocol can be added later for external renderers without changing the adapter boundary.

## Consequences

- Adding a controller requires an adapter and selector schema but not graph-engine changes.
- Adding Helm or Jsonnet requires a renderer rather than controller-specific rendering code.
- Adapter and renderer contracts are compatibility-sensitive.
- Unsupported controller features fail explicitly until their owning adapter or requested service implements them.
