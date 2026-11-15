# ADR 0006: Environment-scoped Incus authority and accepted state

- Status: **Accepted architectural direction; privileged write integration remains gated**
- Date: 2026-11-15
- Reconciles: [ADR 0004](0004-three-layer-state.md), [ADR 0005](0005-provisional-authority-topology.md)
- Tracking: [#693](https://github.com/blevinn/aginctus/issues/693), [#704](https://github.com/blevinn/aginctus/issues/704), [#199](https://github.com/blevinn/aginctus/issues/199)

## Decision

**Incus projects delimit resource authority; Aginctus environments delimit lifecycle; accepted state records environment intent; NixOS governs privileged host capabilities. Configuration origin does not grant permissions.**

An environment has a stable immutable ID, a mutable display name, and exactly one verified Incus server/project binding. A project may contain multiple environments; an environment may not span projects. Connection aliases are not server identity. Relocation to a different server/project is an explicit migration, not an override.

An environment may be declared in root-managed NixOS configuration, user Home Manager configuration, or a selected local file. Each is a producer of the *same* typed environment candidate. There is **no privileged recursive merge of system and user candidates**, no partially writable root-owned accepted revision, and no root proxy for user-proposed state. Conflicting declarations for the same environment identity fail closed. Source-specific configuration precedence may select inputs only within a candidate that the caller is authorized to supply.

The invoking CLI connects using the selected Incus client identity and credentials. Incus enforces API/project permissions. Merely specifying a project on an unrestricted admin socket does not restrict authority. Aginctus also checks environment ownership, explicit selection, resource collisions, pinned revision, and destructive-action preconditions; these are safety properties, not additional security principals.

Root-managed policy may configure local host services, daemon, pools, bridge/firewall and trusted capabilities. Local /etc files do not automatically authorize operations on arbitrary remote Incus servers. Host mutation is an independently privileged activity and may not be delegated through environment reconciliation.

## State and lifecycle

Keep ADR 0004's configured → explicitly accepted (CAS) → observed/converged separation. Accepted nonsecret intent, immutable revision history, operation metadata and CAS are scoped by **environment identity and its verified project binding**, not system/user mode or local XDG directory. A new invoking client must be able to discover state without the original user's config.

Project-scoped Incus-accessible persistence is required logically; exact physical store, service topology, transport and project-restricted authentication remain gated by #695/#696/#704. A shared service must not become a more privileged deputy. State promotion is never itself an Incus workload mutation. Host configuration is not a writable portion of environment accepted state. Secrets stay with their existing exclusive owners.

Every Aginctus-managed resource has exactly one environment lifecycle owner. Labels (such as `user.aginctus.environment-id`) and names are discovery/collision evidence but **not authority or sufficient adoption proof**. Foreign/unknown resources fail closed; transfer and adoption are explicit. Concurrent cooperating reconcilers need authoritative fencing and per-resource mutation preflight; Incus access held by unrelated admins cannot be prevented by an Aginctus lock. Interrupted operations persist uncertain/partial outcomes for observation-based recovery.

## CLI and compatibility

Prefer explicit environment selection with a common CLI and engine regardless of source. Names such as `aginctus environment accept/plan/apply` are illustrative; do not silently change current `apply` semantics. Resolve any Incus remote override against the immutable binding, never redirect intent. Read-only operations must not mutate.

The former Aginctus "profile" / "deployment" scope evolves to "environment", distinct from Incus instance profiles. Existing IDs, workloads, volumes and accepted records are not automatically relabeled or reinterpreted. Migrations require inventory, versioned translation, backup, dry-run, explicit adoption/transfer authorization and zero unexpected destructive changes. Project migration is a separate feature.

## Superseded design and retained safety gates

This ADR supersedes ADR 0004's *provisional* shared root/user partial-patch authority and ADR 0005's *proposed* composite host/project accepted view as the default environment model. It **does not** supersede three-layer state, durable CAS, resource ownership, selected-only operation, separate host authority, or credential separation.

Approval of this architecture is **not permission** to activate user→root execution, production multi-principal promotion, automatic workload reconciliation, privileged service credentials, or to mutate the development host. #704 must retain concrete Incus restricted-principal, state-service confused-deputy, in-flight revocation, restart, race, AppArmor and boundary evidence before the associated write gates are lifted.

## Consequences and implementation ordering

1. Reconcile documentation and tracking (#693–#701, #199, #441).
2. Establish an environment identity and immutable Incus server/project binding with negative tests.
3. Make state namespaces and storage adapters environment-scoped; test independent CAS for two environments in one project.
4. Expose source-agnostic candidate projection and explicit CLI selection.
5. Gate authenticated multi-principal persistence and native execution on #704/#696, including tested fencing.
6. Migrate existing deployments explicitly and accept on disposable Incus before pilot-host changes.

The first slices may be documentation, pure identity/schema and offline state tests only; no production privileged mutation is authorized.
