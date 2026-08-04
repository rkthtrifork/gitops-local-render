# 0003: Workspace semantic comparison

Status: accepted
Date: 2026-08-04

## Decision

Compare fully rendered workspace results rather than serialized YAML. Match objects by Kubernetes identity, retain deployment-unit provenance, report structured field changes, and redact Secret values.

Accept two prepared workspaces and resolve plan paths against an optional render root. Leave Git checkout and repository preparation to the caller rather than embedding them in the renderer.

## Rationale

The renderer's stable responsibility is converting prepared local inputs into a desired-state graph and comparing two such graphs. Git refs, temporary worktrees, and repository-specific generators are useful caller workflows, but they require assumptions and tools that do not belong in the renderer image.

Semantic object comparison removes formatting noise and supports stable machine-readable output. Keeping process execution outside the plan preserves its declarative security boundary.

## Consequences

- Callers can compare Git-derived workspaces without requiring Git inside the renderer.
- Repository-specific generators remain entirely under caller control.
- Comparison reports use redaction for Secret value fields.
- A valid desired-state difference has a distinct exit status from operational errors.
