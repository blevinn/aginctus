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
