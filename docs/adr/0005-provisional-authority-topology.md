# ADR 0005 (proposed): Separately authorized host and Incus-project accepted state

- **Status:** PROPOSED ONLY — **not approved for cross-principal writes**
- Date: 2026-11-13
- Parent architecture: [ADR 0004](0004-three-layer-state.md), [#693](https://github.com/blevinn/aginctus/issues/693)
- Security gate: [#704](https://github.com/blevinn/aginctus/issues/704)
- Experimental evidence: [Incus authorization ledger](../design/incus-project-authority-evidence.md)
- Dependencies: [#694](https://github.com/blevinn/aginctus/issues/694), [#695](https://github.com/blevinn/aginctus/issues/695), [#696](https://github.com/blevinn/aginctus/issues/696), [#698](https://github.com/blevinn/aginctus/issues/698), [#699](https://github.com/blevinn/aginctus/issues/699)

## Problem

The configured → accepted → current model is not in doubt. The question is **who has authority to accept and execute each scope**. A recursively merged NixOS/root and Home Manager/user configuration tree invites privilege-escalation bugs, even if a server tracks every edit with a single CAS revision. A selected Incus project over the administrative Unix socket does not confine the client.

Tested restricted Incus 7.5.1 TLS principals have successfully passed meaningful positive/negative project isolation tests. They can use an explicitly allowed host-owned bridge/pool and cannot read populated foreign resources or change the host project's policy. This is strong *API-policy* evidence, not proof of container escape safety, correct application authentication, or authority transfer. Credential revocation and custom-volume/image expansion are implemented in the subsequent opt-in VM test stack but require successful recorded execution before being treated as evidence.

## Proposed topology

Keep two **independently authenticated accepted-state authority domains**, not one writable root/user JSON tree:

| Authority domain | Permitted scope | Authoritative identity and writer |
| --- | --- | --- |
| Host/system | Incus daemon prerequisites, pools, host network bridges, firewall, root-owned integration and system-owned deployment resources | NixOS administrator and an explicitly designated host lifecycle writer |
| Operator/project | Explicitly delegated tenant project instances and permitted project-scoped images/profiles/volumes | Restricted Incus project credential and a dedicated user/project lifecycle writer, **never** an admin socket |
| Credential owner | OAuth refresh tokens, workload private keys, database secrets | Existing application-specific secret owner; **not** the accepted-state service |

The **logical deployment view** may report a composite of the separate host and operator revision pointers, with provenance and per-domain visibility. Composition is **read-only**; it does not confer a combined write transaction or authorize a user to mutate host capabilities.

Proposed status envelope, **illustrative only** (not an executable or accepted schema):

```json
{
  "deploymentID": "example",
  "host": {"revisionID": "host-identity", "visibility": "authorized"},
  "project": {"name": "example-tenant", "revisionID": "project-identity", "visibility": "authorized"},
  "dependencies": [{"hostCapability": "named-bridge", "state": "observed"}],
  "convergence": "unknown"
}
```

A missing, unauthorized or stale host dependency is **not** an implicit permission to create/repair it from the user context. The composite view cannot claim `unchanged` when some required scope is unobserved. An upstream policy change may mark operator state blocked/degraded until an independently authorized actor explicitly resolves it.

## Cross-domain consistency rules (proposal)

1. **One lifecycle writer per resource.** Owner identity comes from trusted deployment enrollment and authenticated transport, not from user-provided JSON fields, Incus resource labels, process UID alone, or a CLI mode flag.
2. **Independent immutable revision chains.** Host and project domains each have their own CAS and durable parent lineage. A composite status read may carry both IDs as a vector. Do not promise a cross-domain atomic commit.
3. **Pinned dependencies.** Project accepted intent may reference a bounded, root-published host capability identity/version. Such a reference must be validated against an actual host-owner statement and observed infrastructure on every privileged write; it is not itself an authorization grant.
4. **Separate promotion from execution.** A project-authorized actor may propose and explicitly promote *only* project-owned, typed nonsecret intent, without reconciling. Native execution later consumes only its pinned accepted revision and separately re-verifies Incus project credential, policy and resource ownership.
5. **Fencing and concurrency.** Existing instance/project ownership markers are necessary but insufficient. Reconciliation needs per-resource/controller leases and a post-observation identity recheck immediately before an authorized mutation; changed permissions, revisions or capabilities must halt/record uncertainty, not silently retarget.
6. **Explicit ownership transfers.** Host↔user handoff requires independent approval from both scopes, a fenced old writer, documented backup/restore, and a new accepted-domain revision. There is no implicit takeover on a Home Manager switch or NixOS switch.
7. **Opaque credential references.** Never include secret values, raw config/environment dumps, or secret-derived hashes in accepted state or generic status. The secret-owning service retains refresh keys independently of any accepted-state upgrade.
8. **Service bootstrap.** A trusted host bootstrap process may create the protected state volume and its runtime envelope. Neither project user nor the service can invoke a general Incus administrator socket. The service starts read-only until an authenticated writer policy is implemented and reviewed.

## Why not one shared scoped-CAS revision?

A single atomic accepted revision makes read views convenient but encourages treating a scoped user patch as an operation over root-owned material. Server-enforced field allowlists, deeply nested schema migrations, stale role metadata and default config inheritance can turn implementation mistakes into root privilege escalation. CAS prevents lost updates, **not** privilege crossing. Independent domain writers with a read-only composite make authorization separations closer to the native Incus server model, at the expense of explicit cross-domain dependency/fencing and no global rollback.

One physical database could eventually store multiple logically separate authorities, but only if authenticated independent policy domains are proved in code and the runtime. Physical colocation is not a reason to unify privilege checks.

## Failure semantics and recovery

- **Operator certificate revoked:** no further operator reads/writes; accepted revision stays intact, reported observation may become unauthorized. Re-enrollment must be a host administrator action, restricted to the original project.
- **Host bridge/pool removed or modified:** operator accepted revision remains immutable; current state reports missing/drifted/blocked. Do not silently recreate host resources from an operator candidate.
- **State-service down / volume missing:** fail closed; no fallback to XDG or another project's state. Retain immutable history and provide explicit administrator-guided recovery.
- **Partial native operations:** journal the attempt and truthfully report `uncertain`; native reconciliation never invents a global rollback across Incus resources.
- **Conflicting system and user changes:** each authority's CAS only affects its own scope. Composite status can expose both revision IDs and an inconsistent dependency state; it must not merge them into a new privileged accepted blob.
- **Credential owner unavailable:** reference visibility is unknown/denied rather than 'none'. Existing secret volumes and token owners remain authoritative.

## Security proof obligations before adoption

**The following are not checked by approving this proposed ADR text:**

- Execute the opt-in restricted-TLS **revocation/re-enrollment** and **foreign-image/project-local custom-volume** VM cases on the pinned Incus/NixOS revision; record exact outcomes, server version and redacted evidence.
- Test `features.networks=false` shared-host bridge inheritance, policy changes while an operator writes, and unexpected Incus credential/feature upgrades without relying on a CLI `--project` assertion.
- Build #696's actual authenticated state-service API and test hostile nested keys, source overlays, null/default/unknown values, stale revision/credential, TOCTOU, schema upgrades and user attempts to set host capabilities.
- Prove the selected operator writer cannot access unrestricted `incus-admin` or host root credentials, and that the state service cannot act as a confused deputy.
- Design and test controller writer fencing, explicit handoff/rollback, secret-owner isolation and backup/restore under #695/#699/#700/#701.
- Obtain explicit human security/architecture approval and record a dated disposition in #704. Until then **keep #704 open and privileged cross-principal writes prohibited**.

## What can proceed without this approval

Safe typed accepted-state refinements, independent local same-UID CAS proof-of-concept, read-only views, storage recovery feasibility and disposable authorization tests. Do not activate NixOS/HM accepted-state promotion or automatic infrastructure reconciliation yet.
