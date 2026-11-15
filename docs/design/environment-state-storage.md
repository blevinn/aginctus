# Environment-scoped accepted-state storage contract

Status: design contract, **not an approved authenticated state-service implementation**.
Parent: [ADR 0006](../adr/0006-environment-incus-authority.md). Gates: [#695](https://github.com/blevinn/aginctus/issues/695), [#696](https://github.com/blevinn/aginctus/issues/696), [#704](https://github.com/blevinn/aginctus/issues/704). Existing experimental store: [accepted-state local](../operations/accepted-state-local.md).

## Scope

A state namespace is keyed by **verified Incus server identity + exact project + immutable environment UUID**. Never key it only by display name, selected remote alias, Unix UID, XDG path, or Incus project. Two environment IDs in the same project have distinct active pointers, immutable revision lineages, operation histories and writer fences.

State APIs always require the complete explicit environment scope. The backend obtains/validates server/project identity from an authenticated connection or equivalent trusted enrollment; a caller-provided JSON field or the `--project` flag is not proof. No implicit default-project fallback. An attempted name-only lookup is read-only discovery until it has resolved an unambiguous enrolled environment identity.

## Records and CAS

Persist only canonical versioned nonsecret intent, revision metadata, provenance references and nonsecret operational evidence. Never persist runtime credentials, OAuth refresh tokens, generated secrets or raw user environment. A new accepted revision must atomically compare the expected active pointer within **that environment's** namespace, durably append immutable revision data and advance the pointer. A conflicting expectation is reported; do not retry against a different environment or apply last-writer-wins.

A promotion transaction does not touch live Incus workloads. Reconciliation pins one accepted revision, obtains an authoritative cooperating-writer fence, observes authorized resources, and checks ownership + Incus concurrency tokens at each mutation boundary. Incus administration by another authorized actor is outside the lock's authority. Record uncertain operations durably before potentially mutating; recovery reobserves rather than assuming atomic Incus rollback.

## Storage topologies to validate

Candidate implementations include per-environment durable Incus volumes with a project-confined state writer; per-project independently confined service; or a carefully limited shared service. None is selected yet. A state service MUST NOT hold unrestricted root Incus credentials and then accept a project selector from untrusted clients. Project storage-volume permission boundaries, API authentication, namespace traversal, cross-project reads, crash-safety, backups, recovery and bootstrapping must be tested against actual pinned Incus.

A project member with project-wide write access may modify other environments' resources through Incus; environment isolation is a reconciliation correctness property, **not** a second ACL. Stronger tenant isolation requires separately authorized Incus projects.

## Compatibility

The current `DeploymentSpec` and same-UID local CAS store are experimental workload-only components. This contract does not reinterpret their deployment/project composite key or allow existing records to be automatically relabeled. Migration requires enumeration of existing manifests and resources, verifiable binding enrollment, backup, explicit old→new mapping, source-version transformation, and dry-run before switching readers/writers. Do not modify application-owned secret volumes.

## Exit tests before activation

1. Two different environment UUIDs in one Incus project can each promote independently; stale CAS conflicts only within the matching environment.
2. Identical display names across projects/remotes do not collide; display rename cannot change namespace; remote alias changes cannot redirect state.
3. An unauthorized restricted identity cannot inspect/promote another project's environment via any direct API or the state service, even with forged selector/header.
4. Authorized project users cannot promote host policy, invoke a privileged proxy or mutate resource ownership in another environment through promotion.
5. Concurrent promotions and reconcilers cannot apply mixed revision intent; test crash, stale fence, restart, in-flight revocation, partial/uncertain outcomes and recovery.
6. Foreign resources, forged labels, orphaned state and legacy records are never silently adopted.
7. State-service instance re-creation preserves volume-backed histories, restores from backup and does not bootstrap from its own unavailable accepted record.
8. All negative authorization tests run against disposable actual Incus; no production host mutation.
