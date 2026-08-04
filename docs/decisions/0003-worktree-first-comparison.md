# 0003: Worktree-first semantic comparison

Status: accepted
Date: 2026-08-04

## Decision

Compare fully rendered workspace results rather than serialized YAML. Match objects by Kubernetes identity, retain deployment-unit provenance, report structured field changes, and redact Secret values.

Provide Git-aware orchestration with an asymmetric default: a requested base ref is materialized in a temporary detached worktree, while the head is the caller's existing live worktree, including staged, unstaged, and untracked files. Support an explicit head ref for committed-revision comparison and direct base/head workspace inputs for non-Git callers.

Keep repository preparation explicit through a CLI command rather than adding executable fields to `RenderPlan`. Resolve plan paths against an optional render root so generated artifacts can be compared without relocating their plans.

## Rationale

The primary development question is what the files currently being edited change relative to a known base. Requiring a clean worktree or temporary commit makes human and coding-agent iteration alter Git history for the renderer's benefit. Rendering the live filesystem preserves the state the user actually intends to inspect.

Git refs remain useful for reproducible release and CI comparisons, but Git is orchestration rather than a requirement of the comparison engine. Prepared directories and multi-source local mappings must continue to work without Git.

Semantic object comparison removes formatting noise and supports stable machine-readable output. Keeping process execution outside the plan preserves its declarative security boundary.

## Consequences

- Dirty live worktrees are supported by default and identified in diagnostics.
- Git comparison does not fetch, stash, commit, switch, reset, or clean.
- Repository-specific generators run only through an explicit preparation command.
- Preparation that changes tracked live files fails unless explicitly allowed.
- Comparison reports use redaction for Secret value fields.
- A valid desired-state difference has a distinct exit status from operational errors.
