# Native Incus reconciliation: staged migration design

Status: **active v0.2 implementation**, not an accepted all-native production cutover. The selectable native management-network path exists, while Apply/Compose remain part of the current execution path until subsystem-specific parity and migration gates are met. See [ADR 0002](../adr/0002-native-incus-reconciliation.md), [OCI feasibility](native-oci-feasibility.md) and [epic #153](https://github.com/blevinn/aginctus/issues/153).

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

Target scoped types: Incus projects, bridges/networks, images/fingerprints, storage volumes, instances and devices. Keep OCI metadata resolution separate from resource execution. Each resource has an Aginctus identity and ownership metadata, explicit desired fields, lifecycle target and dependencies. A missing resource in a selected plan does not imply deletion.

The first implemented native vertical slice is existing-workload-only:
`workload.Spec.NativeExistingIntent` produces an SDK-independent instance intent,
`native.PlanExistingInstance` performs pure managed-field comparison, and an
injectable local SDK reader validates and rechecks project/instance/image state.
The scoped CLI dry-run consumes this model; static Apply rendering and legacy
writes retain their separate contracts. A native API slice can update only
explicit scalar config keys and description on an existing verified owned
instance. It builds the Incus writable update from a fresh observation, carries
all devices and profiles forward unchanged, and guards submission with a fresh
ETag-bound observation. Device/profile/type/image drift is not repaired by this
operation; lifecycle transitions remain separate.
Explicit empty config values clear the key, matching Incus normalization; omitted
keys remain unmanaged. Description is always explicit, including an empty string.
No actor authorization or execution capability is carried by observations or plans.
`native.EnsureLocalInstance` composes create-if-absent, scalar update and optional
start for one explicit instance intent. Its dry-run uses the same typed planner
without submitting operations. False `EnsureRunning` preserves lifecycle state;
stop remains explicit. Each stage revalidates independently and stops on failure,
retaining completed-stage results without rollback. Callers must establish actor
authority and writer exclusion separately; the API does not activate controllers
or authorize a production migration.

Native project provisioning implements the existing explicit operator
create-only contract through the SDK. It requires confirmed nondefault scope
and verified local transport, rechecks absence before an atomic create with
initial ownership/features, rejects foreign/policy-drifted projects, and reports
uncertain partial completion without rollback. This is not authority for
controller activation, existing-project repair, workload writes or migration.

For public gateway OCI images, `nativeoci.ResolvePublic` resolves and verifies
bounded registry index/manifest/config metadata using ORAS. It keeps full
manifest/config identity separate from Incus's layer-only fingerprint. Incus
pulls the resulting immutable reference through the SDK; the operator does not
unpack the image or require a daemon HTTPS listener. This is demonstrated in
the disposable spike, not yet activated for production gateway lifecycle.

For each selected resource: observe actual state, compare Aginctus-controlled fields, produce structured create/update/replace/delete/no-op actions, preflight the entire selected plan for ownership and safety, then apply in dependency order. Do not silently replace on immutable-field drift. Make storage retention and destructive actions explicit. Dry-run shares planning logic with execution and must not mutate.

### Native plan selection and existing engine binding

`aginctus.dev/native-plan/v1alpha1`, kind `NativeResourcePlan`, is the engine's
JSON selection envelope. Its fields are `apiVersion`, `kind`, `revision` (the
existing environment accepted revision), and `resources`. Each resource has a
logical `id`, a closed `kind` and optional `dependsOn` logical IDs. For example,
an accepted workload uses `{"id":"agent","kind":"workload"}`. This schema does
not serialize domain inputs again: instance fields, immutable image pins, pool
and attachment policy, and versioned credential references remain in their
own typed contracts. Changing the meaning or shape of the selection envelope
requires versioned evolution. Legacy `Orchestration` plans are unchanged.

The engine defines the supported effect set:

| Resource kind | Supported effects |
| --- | --- |
| `workload` | Update or start an existing workload, subject to the actual accepted intent and domain policy |
| `gateway-image` | Create from pinned identity |
| `gateway-volume` | Create without replacement or deletion |
| `gateway-instance` | Create stopped |
| `gateway-runtime` | Update approved runtime configuration, start, or stop |

Resource omission never removes anything. There are no generic project,
network, device, adoption, deletion or replacement operations in this
envelope. Host-shared networking and project provisioning retain separate
authority. Devices are prerequisites verified by the domain, not an implicit
grant to attach or change them. A known replacement requirement blocks this
path; it is never converted into a create.

Trusted composition binds selected IDs to existing domain reconcilers. The
engine validates the full graph and bindings before readings or writes, with a
lexical tie-break between eligible resources. Selection verification must check
the exact graph and domain intent against authoritative pinned input. The
engine does not infer membership, required edges or authority from the JSON.
All feasible global previews and preflight checks run before writes. A
dependency-produced prerequisite can defer a preview; it must be verified at
the resource's own boundary. Every incoming dependency must have a required
postcondition that passes before the dependent runs. Checks correlate evidence
with the same accepted revision; adapters supply resource incarnation where
applicable and independently authorize their reads.

Apply re-observes each stage, restricts action and target to its fresh preview,
and calls the independent writer check for **each** submission, not once for a
composite primitive. Compound stages identify exact logical effect targets (for
example, Postgres and LiteLLM separately). Domains still verify ownership and
preconditions and supply ETags.
Dry-run only validates and observes. It runs preflight-marked offline/read-only
checks, rejects active preflight, skips apply and condition postflight, and
produces no mutation outcomes. Its deferred previews are not readiness claims.
Results retain completed operations, read-only previews, check findings, and
blocked/applied/uncertain effects even if later verification fails. Applied
effects are not evidence of application readiness or durable convergence.
There is no automatic retry or rollback of writes.

The hermetic engine tests compose the existing native workload assessment and
Incus lifecycle reconciler against injected transport and authority fixtures.
This engine entry point does not turn those fixtures into a production
capability. Product composition requires the authenticated writer boundary,
the gateway/domain adapters, and disposable Incus evidence for the joined path.

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
