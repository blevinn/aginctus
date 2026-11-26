# Restricted project-state read authentication contract

Status: **application-layer interface only** (#1044); no production transport or authorization decision is implemented here.

The project accepted-state authority is the verified Incus server/project, not a caller-supplied `--project` flag, NixOS/HM source or a Unix UID. Environment UUIDs divide revision histories within an authorized project; they are not independent Incus ACL principals.

## Required physical transport

A concrete `RestrictedProjectReadSession` must establish and retain the same authenticated, **project-restricted** identity for binding verification and published record access. The service must have no host Incus administrator socket, unrestricted Incus certificate, host firewall/pool controls, root-deputy API or dynamic project override. An implementation that returns JSON claim fields as `VerifiedBinding` fails this contract.

Every read must reject missing/invalid environment UUIDs; foreign project/server bindings; unexpected revision digest, scope and candidate content; incomplete source results; cancellation; transport errors; and identity changes detected before returning a result. No source error may be interpreted as absent acceptance. The current guard checks binding twice, but those checks are not a transactional guarantee of authorization: revocation must be enforced at the physical service boundary, including after reads begin.

## Negative evidence required before production activation

- Authenticate two different restricted Incus projects against disposable *real* Incus, and prove read denial across project boundaries even when requests forge project or environment selectors
- Prove the backend cannot fetch a foreign project through a shared unrestricted credential, direct local socket or root broker
- Revoke a principal during request processing; refuse access on the new revocation state, without returning a privileged payload
- Verify project membership and immutable server identity are derived from authenticated connection evidence, not aliases, headers, environment variables or candidate files
- Verify the service instance remains unprivileged across restart and reattachment of its project-owned custom volume

No live `accept` endpoint, CAS grant or reconciling writer may be activated by these contracts. Promotion/writer fencing are separate gates under #696/#704.

## Implemented read-only protocol candidates (2026-11-24)

The implementation now includes a typed, pinned-mTLS accepted-state wire
client and a GET-only service handler. The handler requires an independently
authenticated TLS peer and an exact project binding. It checks the peer
before and after publishing; the client independently rechecks the restricted
Incus connection before and after every bounded, hash-validated read.

`LiveIncusPeerAuthorizer` proposes a concrete source for the service's
peer decision: the **current**, non-administratively fetched Incus certificate
trust record for the presented TLS peer, with exactly one restricted project.
This interface *fails closed* if the lookup is unavailable, denied, stale,
foreign or inconsistent. It cannot rely on a cached project label.

The opt-in `incus-project-authority` disposable VM now includes a **positive
probe** of `GET /1.0/certificates/{fingerprint}` with the actual restricted TLS
operator identity. **Measured on the pinned Incus 7.5.1 of this flake**: a
restricted, non-admin operator (no `incus` or `incus-admin` membership, no
admin socket, no unrestricted certificate) resolved its **own** trust record
with HTTP 200 and an exact record — matching fingerprint, `type=client`,
`restricted=true`, `projects=["aginctus-authz"]` — and, with the same identity
and the same route, resolved a **second** restricted peer's trust record with
HTTP 200 and the same exact single-project shape. The returned PEM was the
peer's own certificate, so the authorizer's byte-equality check holds too.

**Scope limit.** The self-read and the peer-read are both exercised, but only
inside one disposable NixOS guest running one real Incus daemon. This is
**exercise-only evidence on a non-dedicated host**. It is **not**
dedicated-worker isolation, **not** live-provider evidence and **not**
operator acceptance of any deployment.

**Upstream dependency.** On Incus 7.5.1 the grant for
`GET /1.0/certificates/{fingerprint}` is a server-level `CanView` on
`ObjectTypeCertificate` (`cmd/incusd/certificates.go`,
`internal/server/auth/driver_tls.go`), so a restricted client resolves
**any** trust record, including one restricted to a foreign project and one
restricted to two projects; upstream does not scope trust-record reads by
project. The probe therefore confirms the read path, while the *authority*
stays with the authorizer's own exactness rule: a foreign-project or
second-project record is **not** accepted by the peer-read projection, an
unknown or mismatched fingerprint resolves to no record (HTTP 404), and after
`incus config trust remove` of the peer the read fails closed (HTTP 404).
Re-verify all of this on every Incus upgrade: the read grant is an upstream
implementation detail, not an Aginctus-owned contract.

If Incus denies the trust lookup, the proposed authorizer is **not
deployable** under the selected least-privilege service identity. That is a
material architecture finding, not a test to weaken by adding a host-admin
socket or certificate.

An independent, non-admin, live certificate/project authorization proof with
revocation semantics, exercised on a dedicated worker and accepted by an
operator, is still outstanding. Until one is demonstrated, do not enable any
project-state service listener or treat the optional installed CLI state
configuration as operationally complete.

No service daemon, protected-volume read-only source, secret provisioning,
NixOS systemd unit, or actual host/VM state service has been deployed by this
stack. Existing same-UID local fixtures remain test-only; their normal
absence/404 semantics are not authenticated production absence.

## Existing mounted-volume read adapter (2026-11-24)

`OpenMountedProjectPublishedSource` can read the existing immutable
project/environment revision format from an explicitly selected, preexisting
mountpoint inside a dedicated unprivileged service instance. It verifies
private ownership, exact server/project binding, the existing revision and
candidate hashes, the mountpoint's presence in kernel mount information,
and the restricted Incus binding again after the read. It neither mounts,
initializes, adopts, creates, publishes nor deletes anything.

**Mountpoint presence is not Incus ownership evidence.** The independent
host-side `VerifyProjectServiceAttachment` must first establish the actual
project-owned instance, exclusive custom-volume attachment, and intended
mount path. A dedicated disposable VM must establish service UID,
AppArmor, restart/recreation persistence and foreign-volume refusal.

Hermetic tests substitute only the mountpoint probe; production constructors
always inspect `/proc/self/mountinfo` and refuse uid 0. The same-UID
filesystem fixture used by tests is not sufficient to pass the production
mountpoint check. This adapter remains non-deployable until the service
peer-authority gate and physical instance/volume evidence pass.
## Opt-in read-only daemon (2026-11-24)

The `aginctus` Nix package includes an explicit, nonroot mTLS service,
read-only by default. Run `aginctus serve project-state` as a separately supervised service.
Update any explicit deployment to use this invocation instead of the removed
`state-project-read` package. NixOS/Home Manager activation does not start it;
there is no
automatic bootstrap, volume attachment, project creation,
host Incus socket, or controller activation.

The dedicated service instance needs an already-owned, exclusively attached
custom volume containing the original accepted-state project layout, an
independently provisioned **project-restricted** Incus client certificate,
and separate inbound TLS listener credentials. The service checks the
kernel mountpoint, nonroot UID, private file permissions, server certificate
pin, exact project, a *live* named Incus certificate trust record, and
already-enrolled environment UUIDs. If trust lookup is denied by Incus,
**startup fails**: do not substitute an administrator credential.

Set `AGINCTUS_PROJECT_STATE_READ_SERVICE_CONFIG` to one absolute,
owner-private (0600), non-symlink JSON file with this shape:

```json
{
  "apiVersion": "aginctus.dev/project-state-read-service/v1alpha1",
  "project": "team",
  "listenAddress": "10.10.10.5:8443",
  "mountPath": "/srv/aginctus-state",
  "operatorConfigFile": "/run/aginctus/private-incus-operator.json",
  "serviceCertificatePem": "-----BEGIN CERTIFICATE-----\\n...\\n-----END CERTIFICATE-----\\n",
  "servicePrivateKeyPem": "-----BEGIN PRIVATE KEY-----\\n...\\n-----END PRIVATE KEY-----\\n",
  "trustedClientCertificatesPem": "-----BEGIN CERTIFICATE-----\\n...\\n-----END CERTIFICATE-----\\n"
}
```

The service uses the `operatorConfigFile` schema from
`RestrictedOperatorReadConfig`, with an independently pinned Incus TLS
server and restricted service client key. The inbound trusted client
certificate bundle must be administrator provisioned, and the live
certificate lookup checks the presented client again on every request.
Only exact private-IP or loopback listeners are accepted; wildcards and
public endpoints are refused.

**Not an operator approval or live evidence:** The VM test in #1091 must
prove the restricted certificate can query the current trust record.
Independent host-side `VerifyProjectServiceAttachment` must establish
the attached volume's project/instance/volume ownership and exclusivity;
mountinfo alone cannot establish it. Exercise TLS revocation, project
retarget, service crash/restart, recovery, and operator-host reads before
relying on this binary. If the live trust lookup is forbidden to restricted
identities, this executable remains intentionally non-operational until a
different narrow non-admin authorization proof is implemented.

## Explicit, administrator-gated promotion

The separate `AuthenticatedPromotionHandler` implements a candidate protocol
for `POST /v1/promotion` over verified mTLS. The installed service registers
this route only when its configuration explicitly sets `promotionPolicyFile`.
The JSON envelope has exactly `apiVersion`
(`aginctus.dev/project-accepted-promotion/v1alpha1`), `environmentId` (an enrolled
UUID), `expected` (explicit `null` for genesis, otherwise the complete revision
envelope), and `candidate` (the full typed candidate). The request is bounded
and cannot select a project, mount or promoter by header or override. Success
returns the new revision identity; a stale comparison or unchanged candidate
returns conflict. An unavailable/unknown outcome or lost response requires an
authenticated re-read of accepted state before another request. No automatic
retry, state merge, or resource reconciliation is part of promotion.

The handler matches the actual TLS leaf against the live restricted Incus
trust record, exact server/project and separately verified enrollment. A
mandatory server-owned promoter policy checks the peer's DER SHA-256
fingerprint and complete CAS request (enrollment, expected revision, canonical
candidate and digest). Membership alone is insufficient. The store repeats the
policy and live trust checks during its existing locked publication flow. A
denial cannot publish the pointer; a late denial can leave an immutable orphan
outside accepted history. These live checks and local publication are not a
single transaction with Incus trust or the policy file; they do not eliminate the revocation race
after the last successful check.

### Provisioning exact grants

For a reviewed deployment, the guest administrator may provision a policy
file outside the state volume and set `promotionPolicyFile` in the existing
private service configuration. The policy must be a regular file owned by
guest root, readable by the service UID, with no group or other write bit.
Every directory component must also be root-owned, non-symlink and protected
against group or other writes. Never allow a promoter Incus access to the
service guest or state volume as a substitute for the grant.

The JSON document has `apiVersion: "aginctus.dev/project-promotion-policy/v1alpha1"`
and a required `grants` array. Each grant contains exactly these values:

| Field | Value |
|---|---|
| `peerSha256` | Lowercase SHA-256 of the exact promoter certificate in DER form. |
| `serverId`, `project` | Independently verified binding of this service. |
| `environmentId` | Exact enrolled environment UUID. |
| `expectedId` | Exact current revision ID, or the literal `genesis`. |
| `candidateDigest` | Lowercase SHA-256 of the full canonical candidate. |

Grant changes and revocation use administrator-owned atomic replacement of
the policy file. For example, replace its `grants` with `[]` to deny all
promotion without restarting. The service reloads it on every authorization
check. Missing or invalid policy fails startup when configured, and fails
promotion if encountered later. Omitting the setting leaves the role read-only.

The operator's `AGINCTUS_RESTRICTED_READ_CONFIG` supplies the explicit
`configuredCandidateFile`, Incus trust and pinned state service used by both
commands. After reviewing the preview, use its current accepted revision ID
in `--expect`, or `genesis` for the first revision:

```sh
aginctus --incus-project team --environment UUID state plan-accept -o json
aginctus --incus-project team --environment UUID state promote --expect genesis -o json
aginctus --incus-project team --environment UUID state show -o json
```

Before activation under [ADR 0006](../adr/0006-environment-incus-authority.md),
review the real promoter provisioning and revocation source, nonroot writer
and exclusive volume ownership/fencing, and service attachment, shutdown and
recovery behavior. Run that exact restricted service and policy on a verified
disposable worker, including cross-project denial and revocation. The local
mTLS test uses synthetic certificates and an injected Incus observer with the
real live-trust checks and real CAS store. It does not establish deployed
authority. Bootstrap and exclusive attachment still require independently
authorized provisioning; this route does not implement either operation.
