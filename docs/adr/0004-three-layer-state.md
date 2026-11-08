# ADR 0004: Separate configured, desired, and current state

- Status: Proposed (design review; not an implemented contract)
- Date: 2026-11-08
- Tracking: [#693](https://github.com/blevinn/aginctus/issues/693)
- Detailed design: [Three-layer state and durable control plane](../design/three-layer-state.md)
- Related: [ADR 0002](0002-native-incus-reconciliation.md), [#153](https://github.com/blevinn/aginctus/issues/153), [#155](https://github.com/blevinn/aginctus/issues/155), [#439](https://github.com/blevinn/aginctus/issues/439), [#496](https://github.com/blevinn/aginctus/issues/496)

## Context

Aginctus combines CLI flags, explicit configuration files, environment variables, system/user JSON and NixOS/Home Manager declarations into an effective configuration. Its current orchestration machinery evaluates intent and reconciles Incus resources, but effective configuration and some operational metadata are currently resolved or stored in the invoking client context. Consequently, one changed environment or state directory can affect what a later lifecycle invocation intends to deploy. Nix configuration changes are also too closely coupled to optional resource reconciliation.

ADR 0002 established an Aginctus-owned native Incus observe/plan/validate/execute engine and kept future desired-state configuration out of its initial operation-oriented scope. The next architectural step is to make the accepted deployment specification durable and independent from any individual CLI invocation, without building a new general-purpose infrastructure controller.

## Decision (proposed)

Aginctus has **three distinct layers**:

1. **Configured state:** a fully resolved, validated candidate based on the current caller's configuration cascade and selected deployment. It is a proposal, not authority to change deployed infrastructure.
2. **Desired state:** the durable, authoritative, versioned *accepted* specification for one explicitly selected Aginctus deployment. It is independent of subsequent configuration/environment changes and is the only source of deployment intent for infrastructure reconciliation.
3. **Current state:** independently observed Incus resources and relevant runtime/application resources. Observation supplies evidence for planning but never overrides policy or proves ownership on its own.

There are **two distinct reconciliation boundaries**:

- **Configured → desired:** inspect a canonical, nonsecret diff and *explicitly promote* a validated candidate as a new immutable accepted revision. Promotion is a version-checked state transaction, not an Incus mutation.
- **Desired → current:** use a pinned accepted revision to observe, diff, preflight and execute selected, authorized infrastructure changes using the native orchestration/reconciliation engine. This must not re-resolve deployment intent from ambient CLI parameters or environment. Infrastructure reconciliation never implicitly promotes a configured candidate.

A NixOS or Home Manager switch, a change to an AGINCTUS_ environment variable, or a changed CLI parameter must not alter the accepted revision or modify managed resources by itself. Manual operation is the initial default. Optional automatic **desired → current** reconciliation is independent of configuration promotion; automatic promotion is not introduced by this ADR.

## Durable ownership boundary

Aginctus introduces a small **state service**, logically scoped to the selected deployment, responsible for immutable revisions, active-revision selection, persistent operation/convergence metadata and safe concurrent access. A dedicated, privately reachable Incus service container with protected persistent volume(s) is the preferred initial topology, subject to a focused bootstrap/transport feasibility gate. State APIs must not require that every future deployment lives inside Incus.

The state service is not the infrastructure reconciler. It should not require unrestricted Incus administrative credentials. Existing typed intent, restricted Jsonnet generation, owner checks, planning, field ownership and the Incus Go SDK remain the resource execution path under separately authorized actors.

Persist canonical validated **nonsecret** deployment intent (including explicit identities, selected resource scope, pinned resolved values and opaque secret references), rather than a raw dump of process environment, user configuration files or secret values. Record schema version and source/provenance metadata separately. Accepted revisions are immutable; updating the active pointer requires compare-and-swap against a known prior revision. Concurrent promotions cannot silently overwrite one another.

The state service may hold Aginctus-owned operational metadata and references to credentials. It is **not** automatically the owner of every private key, access token or application database. Dedicated runtime volumes and their credential owners remain authoritative, including LiteLLM's sole ownership of OAuth refresh credentials ([#171](https://github.com/blevinn/aginctus/issues/171)). Avoid plaintext secrets, secret-derived hashes, and expanded Incus configuration in plans, revision history, logs and generic state APIs.

## Safety and recovery

- Selection, profile/deployment identity, Incus project identity and resource ownership must be fixed at the relevant authorization boundary, never inferred from a changed ambient environment.
- Only explicitly selected workloads/resources participate; omission is not deletion. Foreign resources, unsupported drift, destructive replacements and protected volumes retain current fail-closed/explicit-approval rules.
- Reconciliation pins an accepted revision and coordinates writer exclusion/fencing. A stale revision, conflict, service outage or lost lock cannot silently produce a mixed-revision deployment.
- Incus operations are not transactional. Journal partial/uncertain outcomes and verify observed convergence; a desired revision may be accepted while current state is not converged. Rollback means a new accepted revision plus separately validated resource changes, not rewinding a database or claiming infrastructure rollback.
- Bootstrap and recovery of the state service and its authoritative volume must operate through a narrow, explicit, ownership-checked path even when the state service is unavailable. Normal workload teardown cannot delete the state-service volume.
- Initial activation and migration must be safe for existing deployments, existing host-/user-scoped state and dedicated Incus project policies; no implicit adoption or silent credential migration.

## Consequences

**Benefits:** environment changes cannot silently redirect or change a deployed system; Nix can be declarative without implicit activation; independent configuration and infrastructure drift are visible; multiple authorized clients can operate against one accepted deployment; persistence and recovery no longer depend on an arbitrary invoking XDG directory.

**Costs:** additional storage/API/bootstrap, authentication and concurrency concerns; migration of existing system/user-local operational state; a service-outage mode that must fail closed for mutations; further disposable Incus recovery tests.

This decision is intentionally narrower than a continuous controller, a distributed consensus service, a generalized secrets vault or a new Incus resource DSL. It layers durable accepted state *above* the native engine rather than replacing it.

## Compatibility and rollout

The proposed design does **not** retroactively make the state service a v0.2 release blocker or authorize a production cutover. Existing CLI/Nix behavior remains until explicitly migrated and validated. The plan is tracked by [#693](https://github.com/blevinn/aginctus/issues/693) and engineering children [#694](https://github.com/blevinn/aginctus/issues/694) through [#701](https://github.com/blevinn/aginctus/issues/701). Coordinate with the ongoing native reconciliation parity/rollback gates in #153/#159, Home Manager authority boundaries in #437/#439/#441, gateway secrets in #171 and multi-profile isolation in #199/#204.

## Alternatives considered

- **Directly reconcile the latest effective configuration:** simple but loses a stable acceptance boundary and allows incidental environment changes to alter infrastructure.
- **Store a last-applied file in each CLI user's XDG state:** avoids a service but permits competing clients, distinct user/system state, missing shared authority and unsafe recovery.
- **Use Incus resources themselves as the desired-state database:** conflates accepted policy with observed infrastructure and cannot reliably represent independent configuration drift.
- **Put a full continuous privileged controller in the state container:** adds unnecessary daemon authority and failure complexity before bounded operations are proven.
- **Use an external orchestration/secret-management platform:** increases dependencies without evidence that Aginctus needs its full scope.

## Review gates

Before changing runtime behavior, establish schema/provenance/secret-ref semantics, state-service bootstrap and protected storage topology, authentication and writer fencing, safe compatibility with selected projects/profiles, and disposable Incus evidence for both drift paths and recovery. The detailed design explicitly tracks these proof obligations.
