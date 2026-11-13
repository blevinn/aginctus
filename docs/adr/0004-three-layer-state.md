# ADR 0004: Separate configured, accepted, and current state

- Status: Proposed (three-layer contract agreed in principle; system/user authorization provisional pending #704)
- Date: 2026-11-08
- Tracking: [#693](https://github.com/blevinn/aginctus/issues/693)
- Detailed design: [Three-layer state and durable control plane](../design/three-layer-state.md)
- Provisional authority alternative (no write approval): [ADR 0005](0005-provisional-authority-topology.md)
- Related: [ADR 0002](0002-native-incus-reconciliation.md), [#153](https://github.com/blevinn/aginctus/issues/153), [#155](https://github.com/blevinn/aginctus/issues/155), [#439](https://github.com/blevinn/aginctus/issues/439), [#496](https://github.com/blevinn/aginctus/issues/496)

## Context

Aginctus combines CLI flags, explicit configuration files, environment variables, system/user JSON and NixOS/Home Manager declarations into an effective configuration. Its current orchestration machinery evaluates intent and reconciles Incus resources, but effective configuration and some operational metadata are currently resolved or stored in the invoking client context. Consequently, one changed environment or state directory can affect what a later lifecycle invocation intends to deploy. Nix configuration changes are also too closely coupled to optional resource reconciliation.

ADR 0002 established an Aginctus-owned native Incus observe/plan/validate/execute engine and kept future accepted-state configuration out of its initial operation-oriented scope. The next architectural step is to make the accepted deployment specification durable and independent from any individual CLI invocation, without building a new general-purpose infrastructure controller.

## Decision (proposed)

Aginctus has **three distinct layers**:

1. **Configured state:** a fully resolved, validated candidate based on the current caller's configuration cascade and selected deployment. It is a proposal, not authority to change deployed infrastructure.
2. **Accepted state:** the durable, authoritative, versioned *accepted* specification for one explicitly selected Aginctus deployment. It is independent of subsequent configuration/environment changes and is the only source of deployment intent for infrastructure reconciliation.
3. **Current state:** independently observed Incus resources and relevant runtime/application resources. Observation supplies evidence for planning but never overrides policy or proves ownership on its own.

There are **two distinct reconciliation boundaries**:

- **Configured → accepted:** inspect a canonical, nonsecret diff and *explicitly promote* a validated candidate as a new immutable accepted revision. Promotion is a version-checked state transaction, not an Incus mutation.
- **Accepted → current:** use a pinned accepted revision to observe, diff, preflight and execute selected, authorized infrastructure changes using the native orchestration/reconciliation engine. This must not re-resolve deployment intent from ambient CLI parameters or environment. Infrastructure reconciliation never implicitly promotes a configured candidate.

A NixOS or Home Manager switch, a change to an AGINCTUS_ environment variable, or a changed CLI parameter must not alter the Aginctus accepted revision or reconcile Aginctus-managed deployment resources by itself. NixOS may still activate *its own* root-owned host firewall/services outside this acceptance boundary. Manual operation is the initial default. Optional automatic **accepted → current** reconciliation is independent of configuration promotion; automatic promotion is not introduced by this ADR.

## Durable ownership boundary

Aginctus introduces a small **state service**, logically scoped to the selected deployment, responsible for immutable revisions, active-revision selection, persistent operation/convergence metadata and safe concurrent access. A dedicated, privately reachable Incus service container with protected persistent volume(s) is the preferred initial topology, subject to a focused bootstrap/transport feasibility gate. State APIs must not require that every future deployment lives inside Incus.

The state service is not the infrastructure reconciler. It should not require unrestricted Incus administrative credentials. Existing typed intent, restricted Jsonnet generation, owner checks, planning, field ownership and the Incus Go SDK remain the resource execution path under separately authorized actors.

Persist canonical validated **nonsecret** deployment intent (including explicit identities, selected resource scope, pinned resolved values and opaque secret references), rather than a raw dump of process environment, user configuration files or secret values. Record schema version and source/provenance metadata separately. Accepted revisions are immutable; updating the active pointer requires compare-and-swap against a known prior revision. Concurrent promotions cannot silently overwrite one another.

The state service may hold Aginctus-owned operational metadata and references to credentials. It is **not** automatically the owner of every private key, access token or application database. Dedicated runtime volumes and their credential owners remain authoritative, including LiteLLM's sole ownership of OAuth refresh credentials ([#171](https://github.com/blevinn/aginctus/issues/171)). Avoid plaintext secrets, secret-derived hashes, and expanded Incus configuration in plans, revision history, logs and generic state APIs.

## Operational modes and ownership (proposed)

**Provisional working model, not an approved security boundary:** one logical accepted-state authority **per deployment**, with one or more independently authorized scope projections, rather than competing root-side and user-side owners of the same resource. The current draft uses an immutable accepted revision with an explicit **owner/authority scope for every managed resource or policy domain**. The physical store, per-authority revisions, and enforcement model may change following [#704](https://github.com/blevinn/aginctus/issues/704). System and user operational modes are two authenticated, capability-filtered projections of this one deployment. An actor may promote only changes within its explicitly assigned scope; the state service constructs and CAS-commits the full new revision, retaining other scopes unchanged. A namespace or field cannot silently change owner as a side effect of selecting another CLI user or configuration source. Host/user ownership transfer is a separately approved, backed-up, fenced migration.

- **NixOS (system/root) context:** root-managed host capabilities (Incus daemon, storage pool prerequisites, host DNS/DHCP and egress firewall policy) and any explicitly system-owned Aginctus deployment resources. Its candidate comes from trusted system declarations and authorized root-side overrides, not a general merge of an untrusted user's XDG/AGINCTUS_ overlay for host-owned policy. Root is a powerful OS administrator, but application-layer writes still require an explicit selected deployment and scoped operation; no ambient project or implicit takeover of a user-owned runtime credential.
- **Home Manager (user/operator) context:** user-scoped nonsecret configuration and explicitly delegated operator-owned resources within verified Incus project and host capability boundaries. A Home Manager candidate cannot alter host-owned accepted fields, authorize a Linux bridge/firewall change, or acquire root-only secret material through the state API. A user with broad Incus host administration rights must still be treated as privileged; `home-manager` is not a security sandbox.
- **NixOS-only deployment:** system actor may own both host and operator resources when explicitly configured. A Home Manager installation on the same host is not automatically a second accepted-state writer.
- **Home Manager-only deployment:** user may inspect and, if separately authorized, own selected project-scoped operator resources; required host capabilities must preexist under an external host administrator. An absent or unverified host capability is not automatically created with sudo or an ambient Incus admin connection.
- **Combined deployment (working option):** NixOS remains authoritative for host capability/policy; Home Manager owns only the explicitly delegated operator portion. A resource has exactly one active lifecycle owner, with coordinated leases at shared-resource boundaries; competing system/user reconcilers fail closed.

**Important scope limit:** `nixos-rebuild switch` legitimately applies NixOS-owned system and firewall configuration through the *NixOS* activation machinery. Aginctus cannot promise to freeze that external host policy behind its accepted-state promotion. The **target guarantee after migration** is that the switch does not automatically accept a new Aginctus revision or reconcile Aginctus-managed deployment resources. Existing opt-in NixOS reconciliation units currently read effective configuration; they require explicit migration to obtain this guarantee. An HM switch changes its operator package and candidate config, not the running infrastructure; it never triggers privileged host policy. Host capability changes may independently cause observed drift or make a previously accepted deployment nonconvergent, which must be reported.

**Views and access:** both contexts address the same selected deployment and accepted revision, but do not necessarily see the same detail. System mode can report host capabilities, shared-resource ownership, resource convergence and nonsecret user-scope health/summary only as authorized; user mode can inspect its accepted operator declarations and sanitized availability/health of required host capabilities, not host policy internals or another user's secrets. Current-state observation requires separate read capabilities and must distinguish *not authorized* from *not observable* from *no drift*. An `--owner` or `--mode` flag is only an explicit selector, never proof of authority; authenticated actor identity and state-service policy determine permission.

This is a **tentative logical single accepted revision with scoped updates**, not a promise of a monolithic secret store or shared writable root/user filesystem. It does **not** authorize blending untrusted user configuration into a root-privileged tree or exposing a root-capable proxy to user callers. Cross-scope changes require distinct explicitly authorized steps or a separately designed joint transaction. The per-deployment state service must validate every change server-side against the owner's scope, even when a client supplies a full candidate document.

## Security risk and design deferral: cross-authority config composition

**Status:** Open / provisionally accepted for design exploration only; **no acceptance of residual privilege-escalation risk for implementation**. See [#704](https://github.com/blevinn/aginctus/issues/704).

The system/user distinction in this ADR is a *working hypothesis*, not a permanent commitment. Combining configuration from untrusted Home Manager/user inputs with privileged NixOS/root inputs in a shared schema or accepted-state merge creates a **privilege-escalation/confused-deputy risk**. Bugs in normalization, field/path ownership, recursive merging, defaulting, future schema changes, partial updates or server-side authorization could cause user-controlled data to influence host bridges, firewall policy, storage pools, project restrictions or other privileged resources. A versioned revision and compare-and-swap prevent lost updates but do **not** establish actor authorization.

Before implementing cross-authority writes, evaluate **Incus-native project confinement** (restricted projects and project-scoped credentials) as the preferred starting alternative; also compare separately authoritative host/project accepted specifications and hybrid composite views. Native confinement can reduce application-enforced permission logic for tenant resources but **does not** replace NixOS host policy or automatically confer safety when an operator has the root-equivalent Incus local admin socket. Incus project configuration features may inherit data from the default project, so isolation must be proved against the pinned server and exact deployment topology.

The durable **configured → accepted → current** contract, nonsecret typed schema, immutable revision/history, read-only comparison, and independently authorized *single-domain* operations may proceed. Cross-principal config merging, root-capable user APIs, ownership handoff, and user-to-host reconciliation are **gated** on a threat model, real disposable-Incus project authorization experiments, negative security tests, and a reviewed choice documented in the ADR. The implementation must allow the accepted-state authority boundary to evolve without forcing the core state model to change.

## Safety and recovery

- Selection, profile/deployment identity, Incus project identity and resource ownership must be fixed at the relevant authorization boundary, never inferred from a changed ambient environment.
- Only explicitly selected workloads/resources participate; omission is not deletion. Foreign resources, unsupported drift, destructive replacements and protected volumes retain current fail-closed/explicit-approval rules.
- Reconciliation pins an accepted revision and coordinates writer exclusion/fencing. A stale revision, conflict, service outage or lost lock cannot silently produce a mixed-revision deployment.
- Incus operations are not transactional. Journal partial/uncertain outcomes and verify observed convergence; an accepted revision may remain active while current state is not converged. Rollback means a new accepted revision plus separately validated resource changes, not rewinding a database or claiming infrastructure rollback.
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
- **Use Incus resources themselves as the accepted-state database:** conflates accepted policy with observed infrastructure and cannot reliably represent independent configuration drift.
- **Put a full continuous privileged controller in the state container:** adds unnecessary daemon authority and failure complexity before bounded operations are proven.
- **Use an external orchestration/secret-management platform:** increases dependencies without evidence that Aginctus needs its full scope.

## Review gates

System/user trust boundary is specifically **unresolved**, with an independent design and threat-model gate in #704. Safe single-authority state foundations can proceed; privileged cross-mode promotion and reconciliation cannot until that gate passes.

Before changing runtime behavior, establish schema/provenance/secret-ref semantics, state-service bootstrap and protected storage topology, authentication and writer fencing, safe compatibility with selected projects/profiles, and disposable Incus evidence for both drift paths and recovery. The detailed design explicitly tracks these proof obligations.
