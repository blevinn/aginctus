# Controller enrollment prototype and project-scoped execution gates

**Status:** Historical non-authorizing prototype; **not the target authorization architecture**. See [ADR 0006](../adr/0006-environment-incus-authority.md), [three-layer state](three-layer-state.md), [#696](https://github.com/blevinn/aginctus/issues/696), and [#704](https://github.com/blevinn/aginctus/issues/704).

## Existing prototype: documented, not promoted

The already merged `internal/controllerauthority` package (#865/#869 and later work) records administrator-created *system/user controller enrollments* in an exact root-owned file:

```text
/etc/aginctus/controller-enrollment.json
```

It verifies owner-controlled files, uses explicit create/revoke and epoch/tombstone records, and has narrowly scoped read-only checks. The host exclusion primitive from #897 coordinates cooperating host enrollment operations only. No NixOS/Home Manager activation creates this record or deploys its service, and an enrollment match grants **no** Incus or accepted-state authority.

**Do not integrate this prototype into a new root-mediated user lifecycle.** ADR 0006 removes the need to merge system/user configured candidates or enlist a root host broker to decide which user owns a project. Its code and tests should be inventoried for safe removal or narrower extraction once their real consumers are known; avoid deleting security evidence or migration diagnostics prematurely. [PR #900](https://github.com/blevinn/aginctus/pull/900), a proposed root-owned read-only Unix peer-enrollment socket, was closed as superseded without merging.

The old `domain=host` / `domain=operator` enrollment model is **not** a project authority model. Root-owned configuration may still govern *host* capabilities, but it does not issue a substitute permission grant for project resources.

## Current authority and state model

**Incus projects are the authorization and three-layer state scopes** for environment resources. A single project may contain several environments, each with its own accepted-revision pointer, immutable history, resource ownership and cooperating-writer fence. Environment labels, display names and revision records are not independent security principals. Project-wide resources, where supported, require an explicit lifecycle owner rather than implicit sharing across environments.

NixOS/system host policy is a separate root-governed scope. A future independent configured → accepted → observed system-state lifecycle is possible, but neither combines with project revisions nor changes ordinary NixOS activation semantics.

Accepted state **never** follows from an `AGINCTUS_*` override, Home Manager switch or NixOS switch. Promotion requires independently authenticated, authorized project-scoped CAS; reconciliation pins an accepted revision and separately checks Incus permissions and resource provenance at mutation time. No user-to-root accepted-state patching or automatic authority handoff is part of this contract.

## Reusable non-authorizing evidence

| Component | Useful, bounded behavior | What it does not establish |
| --- | --- | --- |
| `ObserveRestrictedOperatorOverTLS` (#892) | Fresh client-certificate-authenticated HTTPS GET to an exact pinned server, without ambient remote or Unix admin socket | Trusted provisioning of endpoint/pin, accepted-state authority, writer ownership, privilege to mutate |
| `PreviewAcceptedWorkloadLifecycle` (#895) | Pure comparison of one pinned workload and observed resource; blocks unverified ownership, immutable drift and implicit stop/delete | That a caller-supplied snapshot is authentic, that its environment/project binding is enrolled, or write authorization |
| `AcquireHostEnrollmentExclusion` (#897) | Cooperating root-only enrollment operations do not conflict on the same host-owned file | Project-wide fencing, remote writer exclusion, or an operator's authority to touch Incus |
| Incus restricted-principal VM evidence | Relevant API-policy positive/negative observations against the pinned Incus version | Blanket confinement of every API, live-operation revocation, or protection from an unrestricted administrator |

These APIs are **not interchangeable**. An authenticated TLS read, a local revision pointer, a project-name flag, a matching label, a host-admin enrollment, or a cooperative file lock individually or together do not authorize a write.

## Required project-scoped execution contract

1. **Explicit selected project/environment.** Resolve immutable environment identity under the verified Incus server/project authority. Project credentials authorize access; environment ownership and versioned records supply lifecycle correctness. A project admin may act outside Aginctus, so resource labels are never ACLs.
2. **Project-owned accepted state.** Promote typed, nonsecret candidate intent through independently authenticated per-environment CAS, using project-scoped storage/service access; preserve optional explicit project-wide records. The state service may not hold a blanket host-admin credential and act on untrusted selectors.
3. **Pinned operation.** Observe resources from the same verified project authority and pin the selected accepted environment revision before planning. Reject stale pins, cross-environment collisions, foreign resources, unauthorized adoption and ambiguous shared ownership.
4. **Mutation-time authorization and fencing.** Use independently authenticated Incus credentials, action-specific preflight, a cooperating-controller fence with revision and resource checks, fresh ETags, and independent host-capability observation. Root host capability dependencies cannot be repaired from untrusted environment declarations.
5. **Durable uncertainty.** Record operations before potentially mutating; mark partial, cancelled, revoked or crashed operations uncertain until independently observed. Incus operations are not transactions and do not automatically roll back.
6. **Independent system boundary.** Host firewall, daemon, pool and trusted host capabilities stay under NixOS/root. A future system-level three-layer history may share generic CAS/revision primitives, **not** project authorization credentials or cross-principal merging.

## Acceptance before activating a writer

Disposable Incus testing must show: two environments in one project with independent CAS/fencing and collision refusal; restricted vs unrestricted principal behavior including forged project/environment selectors; host-policy and cross-project denial; revision changes during execution; in-flight TLS revocation and asynchronous-operation uncertainty; state-service restart/backup; and no secret or protected-volume disclosure. Preserve AppArmor and transport assumptions in the test evidence.

[#704](https://github.com/blevinn/aginctus/issues/704) remains a security gate. No production system/user handoff, Home Manager mutating unit, privileged proxy or pilot-host mutation is authorized by this document. The implementation should avoid creating a new broker merely to preserve the obsolete root/user enrollment prototype.
