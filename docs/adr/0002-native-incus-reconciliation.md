# ADR 0002: Evolve toward Aginctus-native Incus reconciliation

- Status: Accepted direction; implementation deferred
- Date: 2026-10-12

## Context

Aginctus currently generates declarative orchestration plans with trusted Jsonnet and executes them through a Go engine with `incus-apply` and `incus-compose` drivers. These upstream tools accelerated the v0.1 vertical slice but Aginctus increasingly implements policy and lifecycle logic around them: ownership/collision checks, persistent volume safety, image fingerprint drift, instance recreation, workspace device reconciliation, and runtime-specific behavior. Driver capabilities differ, including dry-run support. A separate proposal, [#26](https://github.com/blevinn/aginctus/issues/26), considers spawning upstream CLIs rather than linking libraries.

## Decision

Preserve the Aginctus-owned configuration, restricted Jsonnet evaluation, orchestration plan, validation, and execution engine. Over time replace Apply/Compose drivers with native Incus resource reconciliation using the official Incus Go client. Target a narrowly scoped reconciler for projects, networks, images, volumes, instances and devices with structured observe/diff/plan/validate/execute semantics. Runtime adapters describe intent; they must not depend on orchestration backends.

**Schedule is part of the decision:** no native migration implementation is to begin before the v0.1 deliverable has been accepted. Exploratory and incremental implementation may begin in v0.2, but finishing the migration is neither a v0.2 nor a v0.3 prerequisite; it may continue into later releases. v0.1 retains existing working Apply/Compose paths. This ADR and the tracking issues may be recorded ahead of implementation.

Prefer typed Incus SDK operations over subprocess invocation for the native backend. Do not build a general-purpose Compose replacement. Defer final gateway/OCI image choices pending a feasibility spike.

## Safety and compatibility constraints

- Existing v0.1 CLI workflows and managed deployments retain behavioral compatibility during migration.
- Only selected workloads are reconciled; omission never implies deletion.
- Reject foreign resources; require explicit ownership proof for existing managed resources.
- Reconciliation is idempotent, with non-mutating dry-run and explicit actions for deletion or recreation.
- Preserve persistent volumes, host workspace files, SSH identity and gateway data. Never silently adopt, destroy or orphan state.
- Allow legacy and native drivers to coexist until parity is proven with disposable real-Incus tests and a documented rollback.
- Maintain deterministic ordering, context cancellation reporting, secret redaction and a conservative failure model.

## Consequences

Aginctus will own more resource-specific reconciliation code, but eliminate divergent upstream orchestration semantics and dependency friction. The migration requires a typed resource contract, tested safety policy, OCI/gateway feasibility assessment, staged subsystem migration and eventual cleanup. [Tracking epic #153](https://github.com/blevinn/aginctus/issues/153) captures the long-running execution plan.

## Alternatives

- Retain linked Apply/Compose integrations: lowest short-term risk, but preserves overlapping models.
- Spawn upstream Apply/Compose CLIs (#26): allows independent tool updates, but preserves overlapping models and adds subprocess compatibility concerns.
- Rewrite the current engine: unnecessary; its backend-independent structure is retained.

## Revisit triggers

If the OCI/gateway spike indicates that direct Incus management would substantially reimplement Compose, narrow or defer that portion instead of forcing an all-or-nothing cutover.
