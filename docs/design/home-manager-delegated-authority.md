# Delegated Home Manager execution: proposed authority disposition

Status: **proposed, not approved for cross-principal writes**. Parent: [#704](https://github.com/blevinn/aginctus/issues/704). Related: [#441](https://github.com/blevinn/aginctus/issues/441), [#696](https://github.com/blevinn/aginctus/issues/696), [#426](https://github.com/blevinn/aginctus/issues/426), [ADR 0004](../adr/0004-three-layer-state.md), [executed Incus evidence](incus-project-authority-evidence.md).

## Recommendation after the 7.5.1 disposable tests

Prefer **two independently enforced authority domains**:

- **Host authority**: the NixOS system administrator provisions Incus, the shared bridge and storage-pool boundary, effective firewall policy, protected gateway credentials, approved project restrictions and operator certificate trust. A Home Manager process must never inherit the root-equivalent local Incus socket, a root state-service credential, or an API that accepts user configuration and executes privileged host operations.
- **Delegated operator authority**: a genuinely project-restricted Incus TLS principal, with a host-configured project restriction and exact approved host network/pool allowlists, operates only on explicitly selected project resources. The proposed first unit of delegation is one accepted workload—not the gateway or host lifecycle.
- **Accepted-state authority**: keep a deployment identity and read-only composite status, but do **not** implement a recursive root/user JSON merge or accept one root-authoritative revision editable by the user. The initial same-UID local revision store is a private proof, not a shared trusted broker. Until an independently authenticated state service is approved, accept/promote only within the actor's own domain and require explicit host-side ownership transfer.

This is a **design proposal**, not a permission to activate a controller. A secure Incus identity limits the *maximum possible* operation scope; it does not establish that the particular Aginctus resource belongs to this principal or that another controller will not act concurrently. Incus project restrictions are independent of the future accepted-revision CAS and controller fence.

## Concrete evidence, not inferred guarantees

The [#750](https://github.com/blevinn/aginctus/pull/750) → [#752](https://github.com/blevinn/aginctus/pull/752) → [#756](https://github.com/blevinn/aginctus/pull/756) disposable VM suite actually ran against Incus client/server **7.5.1**. It demonstrated a restricted TLS client creating and running a selected instance using an approved shared host bridge and storage pool; denied host-policy/pool/bridge administration, arbitrary host disk and raw runtime options; and server-side filtering of populated foreign instances and named-resource denial through CLI and the same identity's raw API.

The tested project's restrictions included `restricted=true`, `restricted.devices.nic=managed`, exact `restricted.networks.access` and `restricted.storage-pools.access`, selected `features.*` flags and the pinned server's restricted-TLS authorization routing. **Do not** equate this with the current `ProjectScope.DesiredConfig()`, which leaves `restricted` unset and inherits shared networking. No automatic mutation of existing deployments follows from this evidence.

The isolated review of [#773](https://github.com/blevinn/aginctus/pull/773) and [#775](https://github.com/blevinn/aginctus/pull/775) also executed post-operation revocation/re-enrollment, populated foreign-image filtering and managed custom-volume attachment on Incus 7.5.1. Those results do not close #704. Remaining security tests include revocation during a write (including an already authenticated persistent connection and asynchronous operations already accepted by Incus); daemon/state-service restart and mid-flight policy changes; compromised unprivileged workload behavior when AppArmor is enabled; schema/candidate confused-deputy attacks; and permission/version checks at the real mutation boundary. Post-operation denial of a new TLS request does not establish cancellation or rollback of an in-flight operation.

## Proposed initial writer authorization contract

At each execution, a separately authenticated authority must supply and enforce (not merely echo caller JSON):

1. A principal identity bound to one trusted project grant. Reject an unrestricted `incus-admin` socket for a Home Manager user, ambient project switching and any unknown authorization routing.
2. A deployment identity, exact selected workload, action allowlist, project identity and immutable accepted revision. An XDG/CLI candidate can **propose**, not itself promote or execute.
3. One active controller/owner per resource. Host/NixOS, user and legacy/manual writers must not simultaneously claim execution. A local advisory or abstract socket lock coordinates only cooperating same-host processes and is not a cross-principal security fence.
4. A writer exclusion/fencing token from the **authoritative** resource owner, validated again with the accepted revision and resource identity immediately before each Incus mutation. ETags/conditional writes prevent certain stale updates, not external privileged actors from acting.
5. A persistent operation journal of started/observed/partial/uncertain outcomes. Incus writes are not transactional; process death, revocation or timeout must not be reported as rollback or successful convergence.
6. Independent host readiness and root-owned secret checks. An unobservable capability fails closed; selected omission never means delete; no automatic legacy adoption or arbitrary host device attachment.

The first implementation can safely add *non-authorizing* same-UID revision pinning and typed project policy generation in parallel. Do not connect either to a Home Manager service or a privileged API until the underlying authority and negative tests are reviewed.

## Explicit sign-off still required

Before closing #704 again or enabling #438/#439, record a human-reviewed decision on: (a) project TLS provisioning/rotation and the target Incus server feature policy; (b) the host/project accepted revision layout and authenticated state-service API; (c) controller ownership issuance and revocation; (d) concurrent system/user/manual writer exclusion and migration; (e) each remaining negative test, with actual disposable-VM results; and (f) residual constraints such as AppArmor absence in the existing fixture.

**No pilot-host mutation or existing-project policy migration is authorized by this document.** The first user-managed oneshot is a later, separately gated feature slice.
