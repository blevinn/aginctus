# Native Incus reconciliation: staged migration design

Status: **active v0.2 implementation**. Production execution still uses the legacy drivers. See [ADR 0002](../adr/0002-native-incus-reconciliation.md), [OCI feasibility](native-oci-feasibility.md) and [epic #153](https://github.com/blevinn/aginctus/issues/153).

## Release guardrails

v0.1 is released. v0.2 targets complete native orchestration and dependency retirement. Existing Apply/Compose behavior remains the supported execution path until individual subsystems pass parity gates. Re-scope with the owner if a safe completion is infeasible; do not extend legacy forks to finish scoped writes.

## Target layering

```text
Aginctus config / explicitly selected workloads
                |
      runtime adapters (OpenCode, Hermes, ...)
                |
      embedded restricted Jsonnet
                |
       versioned typed resource intent
                |
       Aginctus plan/validation engine
                |
    native Incus observe/compare/plan
                |
       preflight + safety policy
                |
     Incus Go SDK / daemon API
```

Preserve current Aginctus-owned engine, ordered execution, fail-fast semantics and secrets-by-reference approach. Replace upstream-specific `apply`/`compose` plan bodies with a typed native resource contract incrementally; document versioning/migration for existing generated plans.

Start with explicit bounded operations, not a continuous reconciler. Runtime
adapters emit intent without SDK types. Observation and pure planning remain
separate from actor authorization and mutation-time identity checks. Future
desired-state configuration can drive the same operations; no external engine
or general-purpose controller is part of this migration.

## Resource and lifecycle model

Initial scoped types: Incus projects, bridges/networks, images/fingerprints, storage volumes, instances and devices. Keep OCI acquisition separate until the feasibility spike chooses an implementation. Each resource has an Aginctus identity and ownership metadata, explicit desired fields, lifecycle target and dependencies. A missing resource in a selected plan does not imply deletion.

For each selected resource: observe actual state, compare Aginctus-controlled fields, produce structured create/update/replace/delete/no-op actions, preflight the entire selected plan for ownership and safety, then apply in dependency order. Do not silently replace on immutable-field drift. Make storage retention and destructive actions explicit. Dry-run shares planning logic with execution and must not mutate.

## Risks and proofs

- **OCI/gateway:** audit Compose-supplied project, OCI import, image cache/seed, env/secret, service dependency and network behavior. Spike a narrow native replacement before gateway migration (#154).
- **Storage and workspaces:** never delete host paths or data volumes as a side effect of instance replacement, device revocation or project teardown. Verify UID mapping and workspace root policy using existing v0.2 design (#157).
- **Ownership and selection:** refuse foreign collisions and unsafe adoption; only explicitly selected workloads participate (#155–#157).
- **Migration:** preserve CLI, credential and resource naming contracts; support side-by-side drivers and opt-in per subsystem until proven (#157–#159).
- **Validation:** real disposable Incus integration tests for idempotence, drift, dry-run, recreation, cancellation, network, image fingerprints, persistence, gateway lifecycle and rollback (#156–#159).

## Suggested sequence

1. #154: OCI/gateway feasibility and inventory (go/no-go).
2. #155: Aginctus-native schema and diff/plan contract.
3. #156: Native Incus foundational reconciler with real-Incus tests.
4. #157: Controlled migration of networks, Herdr and selected workloads.
5. #158: Gateway and image lifecycle migration, contingent on spike.
6. #159: Cutover, migration guidance, dependency cleanup and retirement of legacy drivers.

Tasks may overlap where safe; v0.2 is the completion target subject to the safety gates. Revisit #26's upstream-process proposal only when native orchestration fully supersedes it.

## Completion gates

No loss or unapproved replacement of volumes, workspaces, gateway database or identity state; existing deployment upgrade and rollback exercised; ownership collisions fail closed; all supported CLI paths have parity; dry-run remains non-mutating; legacy dependencies are removed only when nothing relies on them.
