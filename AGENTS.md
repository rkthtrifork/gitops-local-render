# Project Instructions

`gitops-local-render` is a strict local renderer for graphs produced by multiple GitOps controllers. The public extension contracts live in `pkg/api`; the platform-neutral engine and renderers live under `internal`; each controller owns its behavior in `internal/adapter/<name>`.

## Boundaries

- Keep graph traversal, source confinement, deterministic output, and object identity controller-neutral.
- Put controller CR parsing, source selector construction, dependency semantics, renderer selection, and controller-specific transformations in adapters.
- Put manifest generation behavior in renderers. Do not add controller conditionals to a renderer.
- Unsupported behavior must return a precise error. Never approximate controller behavior, inspect the process environment for substitutions, fetch undeclared sources, or weaken source confinement.
- Treat `pkg/api` and the render-plan schema as compatibility-sensitive contracts. Update architecture, examples, and conformance tests with changes.
- Seed Secret values are sensitive. Tests must use synthetic values, and diagnostics must not include values.

## Canonical commands

- `make format`: format Go sources.
- `make test`: run the test suite.
- `make build`: build `bin/gitops-local-render`.
- `make verify`: check formatting, vet, race-enabled tests, and a clean build; CI invokes this command.

Architecture and extension rules are in `docs/architecture.md`. Release behavior is in `docs/releasing.md`; security boundaries are in `docs/security.md`. Record consequential architectural changes as a decision under `docs/decisions`, keep implementation detail in code and tests, and remove stale guidance rather than appending exceptions.
