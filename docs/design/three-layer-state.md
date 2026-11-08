# Three-layer state and durable Aginctus control plane

**Status:** Proposed architectural design, **not yet implemented**. Companion: [ADR 0004](../adr/0004-three-layer-state.md). Tracking: [#693](https://github.com/blevinn/aginctus/issues/693), implementation slices [#694](https://github.com/blevinn/aginctus/issues/694)–[#701](https://github.com/blevinn/aginctus/issues/701).

This document specifies the intended contract and sequencing. Command/API names below are **illustrative**, not claims about the current CLI. Existing native Incus orchestration remains the executor; see [ADR 0002](../adr/0002-native-incus-reconciliation.md) and [native design](native-incus-reconciliation.md).

## 1. Model and authority

    NixOS / Home Manager / JSON / env / CLI
                       |
            resolve + validate candidate
                       |
                 CONFIGURED STATE
                       |
           diff → explicitly promote (CAS)
                       |
               ACCEPTED STATE SERVICE
           accepted revision + operation log
                       |
          pin revision → observe + plan
                       |
            NATIVE INCUS RECONCILER
          preflight → authorized execution
                       |
          Incus and application resources
                       |
                  CURRENT STATE
            observed independently

| Layer | Authority | Typical change | Not a substitute for |
| --- | --- | --- | --- |
| Configured | Invoking user/module's input candidate | Nix evaluation, JSON, AGINCTUS_ env, flags | Accepted deployment configuration |
| Desired | One immutable, explicitly promoted revision per deployment | Authorized state-service CAS transaction | Live resource observation or secret storage |
| Current | Incus/guest/runtime observations at a point in time | Manual changes, failures, approved reconciler writes | Accepted policy, ownership proof, operation history |

A configuration source's precedence is preserved when building a candidate, but the *accepted* revision captures the normalized result. An environment variable changed after promotion cannot change the accepted revision or the resources selected for reconciliation. A new client does not need the former client's config files or XDG directory to discover the accepted state.

The stable deployment selector is **explicit**. It binds a deployment/profile ID to its intended Incus project and owner metadata; default Incus project, caller environment, resource names and ambient Incus remotes are never authority to infer a new target. This design accommodates the v0.2 single-deployment case and future v0.3 profiles (#199/#204). Do not assume host-shared bridges are in a tenant project: the existing native bridge exception and host authority remain in force.

### System and user operational modes: one accepted deployment, scoped authority

**Working proposal:** model NixOS/system and Home Manager/user as distinct **candidate producers and authorized projections** of the *same deployment's accepted state*, not as two mutually overwriting accepted-state stores. The accepted revision is globally coherent for the deployment, while each field/resource domain has one recorded lifecycle owner. A promotion may change only the caller's owned domains; the state service performs server-side partial-change validation, builds the new complete immutable revision from the previous accepted revision, and commits through CAS. Unchanged domains remain byte-for-byte stable; a conflict triggers a replan, never a silent last-writer-wins merge.

| Mode | Candidate source and visibility | May accept/reconcile | Must not assume |
| --- | --- | --- | --- |
| **NixOS system** | Trusted `services.aginctus` declarations; privileged host configuration and nonsecret deployment view | Host-owned Aginctus resources and operator resources *only if explicitly system-owned* | Automatic authority over HM-owned runtime credentials, user's XDG overlay or a project's every resource |
| **Home Manager user** | `programs.aginctus` nonsecret declarations and authorized project/user scope; sanitized host-capability status | Explicitly delegated operator-owned resources, given independently verified Incus API capabilities | Host firewall/bridge admin, unrestricted `incus-admin`, other users' state, or implicit sudo |
| **Combined** | Two distinct candidates and filtered views of one accepted deployment | Each actor updates its own domain through scoped CAS; one lifecycle writer per resource | Two independent active accepted specifications for the same Incus object |

In **NixOS-only** mode, system authority can own operator workloads/gateway as well as host-facing resources. In **HM-only** mode, project-scoped operator actions are possible only after an external administrator has established necessary host capabilities and delegated bounded Incus authority. In **combined** mode (preferred long-term), system authority supplies host network/firewall/storage prerequisites while HM runs only its explicitly delegated operator lifecycle. Shifting ownership between these modes requires a reviewed migration/handoff with writer quiescence and verification; changing effective UID or selecting a different XDG directory cannot switch ownership.

The user-facing status view should distinguish **configured candidate** (specific to caller), **accepted spec** (one deployment authority, filtered by actor capability), and **current observations** (only what the actor can actually read). System mode may display nonsecret user-scope summary/health when authorized, but should not copy user token material; user mode may see a sanitized host-capability readiness/denial summary but not edit host policy. A hidden or unauthorized field is *not* evidence of equality or absence. State-service authorization is based on authenticated principal and explicit resource ownership, **not** solely a `--mode` flag, `sudo`, root EUID or Nix module location.

**NixOS activation exception:** `nixos-rebuild switch` may still apply the host's own NixOS-managed firewall and Incus host-capability services; those belong to the host operating-system authority, **outside** Aginctus accepted-state reconciliation. Switching a NixOS or HM generation must not, by itself, promote Aginctus candidate configuration or reconcile its accepted Incus workload/gateway/project resources. A host policy change may invalidate or constrain an accepted Aginctus deployment and must surface as capability failure/current-state drift. Home Manager activation itself installs a CLI and nonsecret config; it neither takes over host policy nor implicitly deploys workloads.

**Source hygiene:** do not feed untrusted user `AGINCTUS_*`, `--config`, user JSON or XDG state into a system-authoritative candidate for protected host fields. Conversely do not elevate a user's candidate into system-owned accepted fields by recursively merging it with `/etc/aginctus/config.json`. Implement an explicit scope-aware candidate projection, validate overlaps and reject conflicting system/user ownership before accepting or executing. Existing generic config precedence is retained for permitted caller-owned fields only.

This approach deliberately avoids a second root-vs-user database and duplicate current-state controllers. The remaining work is to prove transport authentication and per-domain authorization in #696, candidate/source scoping in #694/#697, NixOS/HM ownership and handoff in #699/#700, and cross-mode negative tests in #701.

## 2. Two independent drift and mutation paths

### A. Configured → desired (promotion)

1. Resolve all current configuration precedence layers for an explicit deployment selector, without mutating Incus or initializing runtime secrets.
2. Validate the candidate's resource scope, ownership-relevant identities, schema, referenced resources, secret-reference syntax and migration constraints.
3. Canonicalize the nonsecret declaration, pin derived values/artifact identities needed for reproducible operation, and compare it with the active accepted revision.
4. Show structured **configuration drift** (proposed additions/changes/removals and actions requiring explicit permission). No absence-as-delete inference for selected workloads.
5. Accept a promotion only on explicit authorization, with an expected-active-revision compare-and-swap condition. Atomically append immutable revision and move the active pointer. A conflict requires replan, not blind retry.
6. **Do not** execute infrastructure changes as a side effect of promotion.

An invalid candidate, unresolvable artifact, ambiguous credential reference or incompatible schema cannot be silently promoted. Secret material itself must not be required to render public plan output; credential *availability* is checked under the appropriate runtime owner/operation at execution time.

### B. Desired → current (infrastructure reconciliation)

1. Authorize actor, fixed deployment identity and exact Incus project/host-shared resource scope; pin accepted revision R and acquire required writer exclusion/fencing.
2. Compile R into existing typed resource intent / restricted Jsonnet where applicable. **No reread of deployment values from ambient environment.** Non-deployment connection/authentication parameters remain separately authorized.
3. Observe Incus and relevant application state, including ownership proof, field ownership, current image/volume attachment and selected workload identity.
4. Compute deterministic **resource drift**, preflight the full selected plan and refuse foreign collisions or unsafe unknown state; report accurate non-mutating dry-run.
5. Before each mutation, recheck resource identity/ETag, authority, writer fencing and accepted revision policy. If R has been superseded, do not continue silently with a mixed plan.
6. Execute guarded operations in dependency order; verify observations and record completed/failed/uncertain steps and convergence independently of R.

The accepted revision is not a claim of convergence. One revision may remain active while resources are drifted, unavailable or partially converged. Runtime-issued keys, OAuth refresh, active Herdr sessions, and other operational outcomes have separate lifecycles; their values are not declarative state.

### Comparison examples

| Trigger | Configured ↔ desired | Desired ↔ current | Automatic mutation |
| --- | --- | --- | --- |
| Change AGINCTUS_* gateway setting after successful promotion | Drift | None unless resources have independently drifted | None |
| Nix switch changes declared workload image | Drift | None against existing accepted image | None |
| Explicit promotion of candidate revision R+1 | No drift after acceptance | New resource drift until reconciled | State-store CAS only |
| Manual change to an owned Incus NIC field | None | Drift | Only separately authorized resource reconciliation |
| Unselected workload is missing from candidate | Depends on selected resource semantics | Does not imply deletion | None |
| State service unavailable | Candidate may still be resolved locally | Cannot authorize plan/mutation against accepted state | Fail closed |

## 3. Accepted representation and provenance

Design and version one Aginctus-owned, SDK-independent **accepted deployment specification**, aligned to #155. This is not a second generic resource DSL or a stored Compose document.

Minimum conceptual record (names subject to #694 schema review):

- immutable revision ID, schema/API version, canonical specification digest and creation metadata;
- stable deployment/profile ID, explicit intended Incus project, host-shared resource references, field ownership and selected resource set;
- fully normalized nonsecret resource configuration, resolved immutable image/artifact references where required, and opaque secret references;
- separately held provenance: source type, selected original config revision, tool version and who/what authorized promotion, without persisting raw environment or secret-containing files;
- active-revision pointer, changed only by an authorized compare-and-swap transaction.

The digest is over the canonical *nonsecret* spec, not a hash of token/password contents. Distinguish absent, explicit null, empty and defaulted fields; preserve deterministic order and schema migration behavior. Unresolvable runtime dependencies or artifact references cannot be silently replaced with values from a newer environment during infrastructure reconciliation.

A revision can be superseded but not edited. "Rollback" promotes a prior specification as a **new** accepted revision after fresh policy checks; it never rewinds the database or claims already executed Incus operations were transactionally undone.

## 4. Deployment-scoped state service

**Logical responsibilities:** read active revisions, append immutable specs, authenticate and authorize promotions, enforce CAS/writer coordination, keep a redacted operation journal and retain metadata for recovery. State service is not responsible for reconciling infrastructure or issuing gateway/workload credentials.

**Preferred initial topology:** a small dedicated Incus service instance and separately retained protected volume(s), isolated from untrusted workloads. One logical service per deployment is a useful initial isolation boundary; the interface must allow future topology changes (including multiple profiles or remote deployment) without treating any guest address as part of the accepted-state identity. Decide physical project placement and private host↔service transport through a disposable Incus feasibility gate; never assume default-project access or a publicly reachable API. Do not grant the service Incus administrative socket access merely to store revisions.

**Store:** SQLite is an appropriate initial choice for a single-deployment, single-writer database with explicit transactional revision updates and journal durability. Specify durability mode, WAL checkpoint/consistent backup, schema upgrade/downgrade policy and recovery before activation. The authoritative volume must be retained independently of the ephemeral service image. Use fsync-equivalent and crash-test evidence, not an assumption that an Incus snapshot alone establishes recoverability.

**Suggested narrow API** (conceptual, not an implementation commitment):

- inspect active revision, deployment identity, revision history and known convergence status (read capability);
- compare candidate with active revision (read-only, with redaction);
- submit CAS promotion with base revision and an authorized actor (promotion capability);
- acquire/fence an operation for a pinned revision, append stage outcomes, release or mark uncertain (reconciler capability);
- explicit protected backup/restore/export with separate authorization.

No direct "apply arbitrary Incus commands" API and no endpoint exposing raw credentials. An unavailable service must not cause the CLI to fall back to an arbitrary user's local state as the new authority.

### Bootstrap exception and recovery

The state service cannot require itself in order to be created. A minimal **bootstrap domain** uses an explicit local actor/deployment identity and a narrowly scoped Incus project/instance/volume contract to create or recover the service. Verify resource markers and transport on each operation; refuse foreign collisions and uncertain state. The ordinary declarative reconciler must not own deletion of the authority volume.

Document and test a bare-client recovery: from a fresh CLI/XDG directory, attach to an existing authorized deployment and inspect its accepted state. Recovery of a missing service instance with retained volume, missing/mounted-elsewhere volume, corrupted store, interrupted schema migration, and inability to authenticate must each have safe failure and remediation procedures.

## 5. Concurrency, permissions and failure model

- **CAS** serializes promotion; concurrent operators must replan after a stale active-revision error.
- **Writer exclusion/fencing** prevents two resource reconcilers from mutating the same deployment or host-shared resource incompatibly. A per-deployment lease is insufficient for a host-shared bridge: it needs its own coordination/owner boundary.
- **Revision pinning** forbids mixing accepted specs in one operation. Define whether promotion waits for an active reconciler or supersedes it and forces a safe abort; the first implementation should favor a simple exclusive operation protocol.
- **Revalidation** remains necessary at Incus mutation boundaries. DB leases and ownership labels cannot prevent independent external Incus actors from racing.
- **Operation journal** records operation ID, deployment/revision, resource IDs, stages, classification and uncertainty without raw SDK/Incus secret-bearing payloads. Do not report automatic rollback where Incus has none.
- **Authorization** distinguishes inspector, promoter, reconciler and bootstrap/restore actor. Home Manager user operation does not gain NixOS host-firewall or shared-network authority.
- **Fail closed** on unknown deployment selector, unknown schema, foreign ownership, service outage, stale fence, ambiguous local vs system state, unavailable protected secret owner or unverified attachment. Read-only diagnostics may report limited observation but must label that the accepted revision was unavailable.

## 6. Credential and state ownership

A centralized state service is **not** a centralized vault.

| Information | Intended owner | Migration policy |
| --- | --- | --- |
| Accepted nonsecret configuration and change history | State service | Import only after validated candidate/identity checks |
| Resource ownership IDs, operations, last convergence evidence | State service | Explicit bounded migration; no indiscriminate import |
| Gateway provider/catalog declarations | Configured candidate → accepted spec where declarative; runtime metadata separate | Audit existing XDG/system state for collisions (#496) |
| Gateway PostgreSQL data, master/salt, LiteLLM OAuth refresh credentials | Dedicated protected runtime owner/volume | Preserve exclusivity, no copying into generic accepted-state DB (#171) |
| Herdr SSH identities and host trust | Existing protected owner until explicit narrowly scoped migration | No implicit rotation or cross-profile reuse (#204) |
| Short-lived virtual keys and token values | Credential issuer/runtime owner | References, lifecycle and revocation metadata only; not spec values |

Private volumes need proper mount/UID permissions, backup and authentication. Incus volumes alone do not provide encryption or access policy. Never publish credential values via expanded Incus config, Jsonnet, structured plans, CLI env capture, journal, or state export.

## 7. CLI, NixOS and Home Manager surface

Possible user workflow; syntax is **illustrative and does not yet exist**:

    aginctus desired plan            # configured vs accepted: non-mutating
    aginctus desired apply           # explicitly CAS-promote candidate only
    aginctus reconcile plan          # accepted vs current: non-mutating
    aginctus reconcile apply         # separately authorized infrastructure convergence

The exact names and compatibility strategy are defined in #697. Distinguish release/operational command semantics from historical aginctus apply behavior during migration.

NixOS and Home Manager render **scope-filtered** typed configured candidates. Switching either module configuration **cannot automatically promote** the Aginctus accepted revision or deploy Aginctus-owned workloads; NixOS is still free to apply root-owned host firewall/host services under its own OS policy. Optional automatic accepted→current reconciliation, if introduced, is independent, reads only the accepted revision and retains #437/#439/#440/#441 authority restrictions. No user-service fallback to privileged host policy, no implicit Incus default project, and no mixed-owner simultaneous writer.

A future remote client should be able to inspect accepted state and submit a candidate through authorized service APIs without requiring access to the original operator's filesystem. That is a portability property, not a v0.2 remote-multi-host feature promise.

## 8. Engineering sequence and acceptance

1. [#694](https://github.com/blevinn/aginctus/issues/694): typed canonical schema, versioning, hashes and both drift contracts; unit fixtures first.
2. [#695](https://github.com/blevinn/aginctus/issues/695): Incus bootstrap/transport feasibility and persistent service/store, crash-safe backup/recovery.
3. [#696](https://github.com/blevinn/aginctus/issues/696): authentication, CAS, writer exclusion and redacted journal.
4. [#697](https://github.com/blevinn/aginctus/issues/697): read-only configuration drift and explicit promotion CLI.
5. [#698](https://github.com/blevinn/aginctus/issues/698): native planner consumes accepted pinned revision; reconcile current state safely.
6. [#699](https://github.com/blevinn/aginctus/issues/699): independent NixOS/Home Manager switch/promotion/convergence semantics.
7. [#700](https://github.com/blevinn/aginctus/issues/700): inventory and guarded migration of existing XDG/system metadata, not token-material centralization.
8. [#701](https://github.com/blevinn/aginctus/issues/701): disposable real-Incus/NixOS acceptance, recovery/rollback runbooks and release evidence.

A vertical proof should show: accept R, change an env var (candidate drift only), manually drift one owned Incus field (resource drift only), promote R+1 (state transaction only), then separately converge and observe result. Repeat with stale concurrent clients, foreign resource collisions, interrupted writes and a fresh client with no old XDG state. Verify that volumes, identities and runtime secrets survive recreation/rollback.

**Release rule:** design/implementation is incremental; #153 native parity, ownership, credential and production-migration gates remain binding. Nothing here silently adds a v0.2 release blocker, replaces a current CLI command, or authorizes pilot-host mutation.

## 9. Open engineering decisions for early slices

The following are intentionally not settled by the architecture ADR:

1. Protected physical placement: one instance per deployment versus a host-scoped state service with rigorously isolated per-profile stores; prefer the former initially, validate against #199/#204.
2. Host↔service transport and actor authentication in existing Incus networking/host-authority constraints; require a no-public-listener feasible path before committing.
3. Promotion while reconciliation is active: serialize for the first implementation, or prove supersession/fencing safety and recovery.
4. Version retention, audit/export, backup key management and encrypted-at-rest expectations for operational metadata.
5. Compatibility staging for existing CLI apply/ensure and NixOS oneshot reconcile services, plus safe default-profile state migration.

Resolve these with concrete spikes/tests in the referenced issues, not undocumented behavior baked into the first implementation.
