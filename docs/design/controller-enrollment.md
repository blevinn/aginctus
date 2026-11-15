# Experimental host controller enrollment record

**Status:** implementation candidate only; **not write authorization**. Tracks
[#441](https://github.com/blevinn/aginctus/issues/441),
[#696](https://github.com/blevinn/aginctus/issues/696),
[#704](https://github.com/blevinn/aginctus/issues/704), and
[ADR 0005](../adr/0005-provisional-authority-topology.md).

## What is implemented

The internal `controllerauthority` package introduced by #865 and #869
models a host-administrator-authored, **nonsecret** controller enrollment
record. It has a deliberately fixed pathname:

```
/etc/aginctus/controller-enrollment.json
```

The reader requires the final directory and file to be real, owner-controlled
objects (no symlinks, group/world writes or ambiguous hard links). The file
must be a regular file owned by **root** and the parent directory must be
root-owned and non-writable by less privileged principals. The root-only
administrative API can create an enrollment or explicitly revoke one under
a persistent, cooperating-writer flock, with atomic write/rename and fsync.

**There is no automatic bootstrap of this file.** Normal NixOS and Home
Manager modules do not install or activate it. The NixOS module's ordinary
`environment.etc` output is typically a Nix-store symlink, which the current
reader intentionally rejects. Any future NixOS integration must provision
a genuine, independently trusted root-owned file without treating user
declarations or Nix store contents as an authorization channel.

Illustrative *synthetic* record (not a production grant):

```json
{
  "apiVersion": "aginctus.dev/controller-enrollment/v1alpha1",
  "enrollments": [
    {
      "controllerID": "operator-controller",
      "domain": "operator",
      "uid": 1000,
      "certificateFingerprint": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "deploymentID": "demo",
      "incusProject": "aginctus",
      "workloadID": "dev",
      "revisionID": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "generation": 1,
      "epoch": 1,
      "actions": ["start", "stop"],
      "status": "active"
    }
  ]
}
```

Every field is required. Root-owned host controllers use `domain=host`,
`uid=0`, and an **empty** certificate fingerprint; operator controllers
use a nonroot UID and exactly one pinned restricted-TLS certificate.
Unknown actions, keys, defaults, broad project selectors, duplicate resources
or simultaneous host and operator claims fail closed.

The root-side create operation accepts only a new exact workload, never an
implicit takeover. Revocation compares the exact owner/resource and prior
epoch and writes a retained `revoked` tombstone with an incremented epoch.
It does not silently delete that history or let a different owner enroll
in its place. **A tombstone is not evidence that the old process stopped.**

## Live Incus authority is independent

The read-only #867 path separately requests **daemon-reported** facts
through the exact restricted operator's current TLS session:
`/1.0?project=NAME`, that identity's `/1.0/certificates/FINGERPRINT`,
and the exact `/1.0/projects/NAME`. It checks trusted TLS, certificate
restriction to **only** the selected project, expected Aginctus ownership
labels and explicit restricted bridge/pool/NIC/disk policy. The host-owned
policy candidate in #866 includes `restricted.devices.disk=managed` to
match the executed disposable Incus VM matrix.

The current read callback is an injected, read-only diagnostic interface,
**not** a cryptographically authenticated session owned by a protected
executor. A caller could fabricate its output, and an enrollment-only
match is intentionally named
`enrollment-matched-not-authorized`. Neither result produces a write token.

## Newly implemented read-only and cooperative pieces

The next independently reviewable slices provide three complementary
**non-activating** APIs. They are not interchangeable:

| Evidence or primitive | Guarantees within its stated boundary | Does **not** guarantee |
| --- | --- | --- |
| `ObserveRestrictedOperatorOverTLS` (#892) | Every read uses a fresh, client-certificate-authenticated HTTPS connection to a server with an exact, independently pinned leaf SHA256; no redirection, non-GET method, ambient CLI remote or Unix administrator socket | Trusted host provenance for caller-supplied endpoint/pin, peer UID, persistent-connection revocation behavior, controller ownership or any write entitlement |
| `PreviewAcceptedWorkloadLifecycle` (#895) | Pure preview of an immutable-fingerprint, selected accepted workload against explicit observed identity, ownership and current instance metadata; no implicit stop/delete or foreign adoption | That the caller-supplied snapshot actually came from a held accepted-state pin, real device/volume authority, current Incus ETag or permission to start |
| `AcquireHostEnrollmentExclusion` (#897) | Root-only, same-host cooperative serialization using the exact administrative enrollment lock; rejects revoked, changed or ambiguous host controller selection and excludes simultaneous administrative enrollment changes | Noncooperating root/manual Incus clients, cross-user/project delegation, an Incus-native fencing token, a verified restricted TLS principal or live Home Manager execution |

All three intentionally **lack any Incus mutation entrypoint** and create no
NixOS or Home Manager service or timer. The host exclusion requires an
already existing real root-owned enrollment file and lock; it is not created
by a normal `nixos-rebuild switch` or `home-manager switch`.

The eligible root-owner `HostEnrollmentExclusion` is NOT a token that
operators can borrow. For a delegated operator writer, a future independently
authenticated broker must verify the real peer UID (not a caller-supplied
number), pin and own the restricted TLS session independently, and provide a
single authoritative cross-writer ownership protocol. Without that broker,
treat all user-controlled requests as **not authorized** even when their
read-only project preflight succeeds.

### Next integration and acceptance sequence

1. Implement and review an independently authenticated, restricted-project
   executor/owner protocol without a general administrator-socket escape.
   Bind one resource to a particular accepted revision and exact enrolled
   principal; deny concurrent NixOS/user/legacy writer ownership.
2. Couple a durable operation journal with the actual accepted-revision pin,
   independent host capability policy, native Incus observation and
   mutation-boundary ETag. Keep partial/crashed operations `unverified` until
   a separately authorized observer confirms the result.
3. Execute opt-in disposable Incus tests for restricted TLS request provenance,
   same-credential revocation on a persistent connection, policy changes
   during a previously accepted operation, competing root/user writers,
   database/volume preservation and reversible quiescence/handoff.
4. Require explicit human sign-off under #704 **before** enabling an opt-in
   Home Manager user oneshot that can start or stop selected resources. Even
   then, normal Home Manager activation must never mutate running workloads.

## Outstanding trusted writer protocol

Before enabling #438/#439, the execution boundary must actually enforce:

1. A kernel-verified peer (or independently authenticated service identity)
   bound to the root-published enrollment, including a genuinely restricted
   Incus TLS transport whose private key cannot reach the system/root
   administrator socket.
2. Current accepted revision, explicit workload and allowed action checked
   under one authoritative operation, with independent resource provenance,
   observed project policy, Incus ETag and host-capability dependency checks.
3. A monotonically fenced, live exclusive owner across all participating
   NixOS and Home Manager writers, plus an explicit policy for manual,
   renamed or uncooperative actors. An enrollment epoch or flock alone
   cannot satisfy this.
4. Durable operation journaling *before* the first Incus mutation,
   cancellation/revocation and partial-result handling; no fictional
   rollback of an already accepted asynchronous Incus operation.
5. Disposable VM exercises of host/user conflicts, in-flight TLS revocation,
   project-policy drift, service crash/recovery, and explicit handoff/rollback
   while preserving gateway/volume/secret owners.

ADR 0005 and #704 remain proposed/open. Do not infer security sign-off from
merged implementation code, a policy match, a successful hermetic test or
post-operation TLS denial. No pilot-host or production Incus state is changed
by this experimental package.
