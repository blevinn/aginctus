# Project-scoped accepted-state storage with environment revisions

Status: design contract, **not an approved authenticated state-service implementation**.
Parent: [ADR 0006](../adr/0006-environment-incus-authority.md). Gates: [#695](https://github.com/blevinn/aginctus/issues/695), [#696](https://github.com/blevinn/aginctus/issues/696), [#704](https://github.com/blevinn/aginctus/issues/704). Existing experimental store: [accepted-state local](../operations/accepted-state-local.md).

## Implementation checkpoint (2026-11-21)

The first environment-scoped persistence engineering stack is
[#1007](https://github.com/blevinn/aginctus/pull/1007) →
[#1009](https://github.com/blevinn/aginctus/pull/1009) →
[#1010](https://github.com/blevinn/aginctus/pull/1010) →
[#1011](https://github.com/blevinn/aginctus/pull/1011):

- A versioned `AcceptedRevision` content identity binds immutable
  environment UUID, server/project, generation, parent and canonical
  nonsecret `EnvironmentCandidate` digest.
- `LocalProjectStateStore` is a deliberately **same-UID, local filesystem
  fixture**, holding a versioned server/project anchor and a separate
  `environments/<UUID>/` accepted pointer, immutable revision history and
  cooperative lock for each environment. It tests two revision histories
  under one project, CAS conflict, restart and tampering.
- `PinLocalAccepted` and the immutable **unverified** start-intention record
  test retention of selected accepted state and process-crash recovery,
  without authorizing an Incus mutation.
- Historical reads verify reachability from the *published* active pointer;
  an immutable revision written before a crash but never published does
  **not** count as accepted history. Explicit CAS may safely reuse an
  identical uncommitted immutable record after inspection.

**None of these APIs authenticates a project principal or makes this
fixture a project-accessible state authority.** The root path is caller
provided and must be under a trusted same-UID directory. Its exact
server/project anchor is validated for correctness but was not obtained
from an Incus daemon. A caller-supplied observed binding is not credential
evidence; a filesystem `.flock` cannot prevent another Incus client from
changing a resource. Do not expose the fixture directly as a user-facing
`accept` endpoint or infer from its tests that #695/#696/#704 are complete.

The **next physical implementation** must choose and deploy project-owned
Incus-accessible storage/service topology, independently prove the
effective daemon project identity on every trust-boundary crossing, and
enforce project credential restrictions without a privileged root deputy.
It must bootstrap and recover without reading its own unavailable accepted
pointer, independently fence cooperating environment writers, and persist
durable unknown/partial outcomes around the actual SDK operation. Exercise
multiple environments, cross-project denial, crash/backup/restore and
in-flight revocation against disposable *real* Incus before enabling
Home Manager or NixOS automatic environment reconciliation.

## Scope and authority

The **Incus project is the logical three-layer configured → accepted → observed state authority**, identified by verified server identity plus exact project. A project can contain many independently managed environments. Environment state is a subordinate namespace keyed by immutable environment UUID; it is not a separate authentication or authorization principal. Never identify a project authority by display name, selected remote alias, Unix UID or XDG path. Never identify an environment's accepted records by project alone. Two environments in one project have distinct active pointers, immutable revision lineages, operation histories and cooperating-writer fences; no project-wide monolithic accepted revision is required.

The same project authority may own explicitly identified **project-wide state** (for example, shared project-level configuration). Such resources must have one assigned lifecycle owner and cannot be implicitly claimed by multiple environments. Cross-environment references or dependencies are not an authorization grant; project-wide and environment-specific revisions must not be silently merged.

Environment state APIs require a complete explicit project/environment selector. Project-wide state APIs require the explicit project authority and a distinct project-level record selector. The backend obtains/validates server/project identity from an authenticated connection or equivalent trusted enrollment; a caller-provided JSON field or the `--project` flag is not proof. No implicit default-project fallback. An attempted name-only lookup is read-only discovery until it has resolved an unambiguous enrolled environment identity.

## Records and CAS

Persist only canonical versioned nonsecret intent, revision metadata, provenance references and nonsecret operational evidence. Never persist runtime credentials, OAuth refresh tokens, generated secrets or raw user environment. An environment acceptance transaction must atomically compare the expected active pointer within **that environment's subordinate namespace**, durably append immutable revision data and advance the pointer. A separate project-wide record, if introduced, has its own explicit CAS authority and ownership. A conflicting expectation is reported; do not retry against a different environment or apply last-writer-wins.

A promotion transaction does not touch live Incus workloads. Reconciliation pins one accepted revision, obtains an authoritative cooperating-writer fence, observes authorized resources, and checks ownership + Incus concurrency tokens at each mutation boundary. Incus administration by another authorized actor is outside the lock's authority. Record uncertain operations durably before potentially mutating; recovery reobserves rather than assuming atomic Incus rollback.

## Storage topologies to validate

See [the provisional project-volume service topology](project-state-service-topology.md)
for the selected disposable implementation: one custom volume and one service
instance within an Incus project, independently versioned environment UUID
namespaces, and a bootstrap path outside accepted-state reconciliation.
This is a physical feasibility direction, **not** a production authenticated
state service or permission to enable writer processes.



The preferred logical design is a **project-scoped state authority** with separately versioned environment records. Candidate physical implementations include a project-owned volume/service, environment-specific data partitions within project-confined durable storage, or a carefully limited shared service. None is selected yet; independent environment revision histories do **not** require one state-service container per environment. A state service MUST NOT hold unrestricted root Incus credentials and then accept a project selector from untrusted clients. Project storage-volume permission boundaries, API authentication, namespace traversal, cross-project reads, crash-safety, backups, recovery and bootstrapping must be tested against actual pinned Incus.

A project member with project-wide write access may modify other environments' resources through Incus; environment isolation is a reconciliation correctness property, **not** a second ACL. Project authorization controls access; environment-scoped ownership, CAS and writer fencing control lifecycle correctness. Stronger tenant isolation requires separately authorized Incus projects.

## Separate system-state possibility

A future root-owned **system** three-layer state scope may govern selected host-level configuration outside Incus projects. It is not a second project/environment authority or a merger of system and user revisions. NixOS remains the host authority; this design does not redefine `nixos-rebuild switch` or require a system state service. Reuse canonicalization, immutable revisions, comparison and CAS primitives if warranted, not the project authorization transport or a universal scope framework.

## Compatibility

The current `DeploymentSpec` and same-UID local CAS store are experimental workload-only components. This contract does not reinterpret their deployment/project composite key or allow existing records to be automatically relabeled. Migration requires enumeration of existing manifests and resources, verifiable binding enrollment, backup, explicit old→new mapping, source-version transformation, and dry-run before switching readers/writers. Do not modify application-owned secret volumes.

## Exit tests before activation

1. Two different environment UUIDs in one Incus project can each promote independently; stale CAS conflicts only within the matching environment. A possible project-wide record has separate revision ownership and cannot be overwritten by accepting an environment candidate.
2. Identical display names across projects/remotes do not collide; display rename cannot change namespace; remote alias changes cannot redirect state.
3. An unauthorized restricted identity cannot inspect/promote another project's environment via any direct API or the state service, even with forged selector/header.
4. Authorized project users cannot promote host policy, invoke a privileged proxy or silently mutate another environment's accepted records or project-wide resource ownership through environment promotion.
5. Concurrent promotions and reconcilers cannot apply mixed revision intent; test crash, stale fence, restart, in-flight revocation, partial/uncertain outcomes and recovery.
6. Foreign resources, forged labels, orphaned state and legacy records are never silently adopted.
7. State-service instance re-creation preserves volume-backed histories, restores from backup and does not bootstrap from its own unavailable accepted record.
8. All negative authorization tests run against disposable actual Incus; no production host mutation.
