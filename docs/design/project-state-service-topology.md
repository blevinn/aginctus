# Project-owned accepted-state volume and service topology

**Status:** Provisional physical implementation for disposable validation,
**not** authorization sign-off or a production promotion API.
Decision boundary: [ADR 0006](../adr/0006-environment-incus-authority.md).
Tracking: [#695](https://github.com/blevinn/aginctus/issues/695),
[#696](https://github.com/blevinn/aginctus/issues/696),
[#704](https://github.com/blevinn/aginctus/issues/704).

## Chosen bootstrap topology for the next implementation phase

Use **one project-owned Incus custom storage volume, attached exclusively to one
dedicated state-service instance in the same verified Incus project**, as the
first physical accepted-state implementation. The service instance's rootfs
is disposable; the independent custom volume owns all project-environment
revision histories, active pointers and nonsecret operation records. It need
not be one container or volume per environment. Every environment uses its
immutable UUID as a subordinate namespace under the same project authority.
Future explicitly owned project-wide records must use a separate namespace,
not a shared environment active pointer.

This topology is deliberately distinct from a SQLite/volume *format* decision:
the merged local filesystem CAS fixture proves one on-disk format but does not
establish a supported remote service protocol, snapshot strategy or multi-user
transactional locking. A future SQLite-backed service may replace its file
layout without changing ADR 0006's revision schema and per-environment CAS.

### Authority and bootstrap order

1. An authenticated **host/project administrator** first selects and verifies
   the exact Incus server, nondefault project, storage pool and resource
   inventory. The service may **not** create its own Incus project, volume,
   bridge, pool or privilege grant from a user-supplied environment candidate.
2. The administrator creates or explicitly verifies one project-scoped custom
   volume and one dedicated service instance with exact ownership. Reject
   foreign/preexisting same-name resources and unexpected device attachments;
   never claim a custom volume by matching a label alone. The project
   membership and runtime policy come from actual Incus API observations.
3. Mount the volume into the service instance under one fixed data path,
   verify exclusive writable attachment and root/device provenance, then
   prepare a private state directory for a dedicated nonroot service UID.
   The guest must not receive a host Incus admin socket, host firewall controls,
   SSH/Gateway/OAuth secrets or a privileged service certificate.
4. **Only after the volume is available** initialize schema/version metadata.
   The project store's server/project identity is checked against independently
   authenticated connection evidence before exposing accepted-state APIs.
   Creating a new volume or instance never implicitly creates an accepted
   revision or applies any workload.
5. Keep the listener off by default. The installed mTLS role can be started
   under the dedicated nonroot UID for the administrator-controlled fixture
   below. Delegated access requires a separately verified Incus isolation
   boundary; no generic root-mediated user request proxy is acceptable.

The admin-owned bootstrap path must remain outside accepted-state convergence
or the service would depend on its own unavailable accepted pointer to recover.

### Administrator-controlled deployment only

**Gate:** On the current restricted TLS model, a certificate for the service's
own Incus project gives access to guest exec, guest files and export. Treat
holders of that credential as capable of controlling the service and accessing
its keys and accepted state outside mTLS. Project restriction, ownership
markers, `UsedBy`, a nonroot UID, and an exact promotion grant do not revoke
those Incus permissions. This blocks delegated **reads as well as promotion**.

For a disposable exercise, use a nondefault project on an administrator-controlled
Incus daemon, with only the dedicated service guest and its custom state volume
in that project. The host administrator owns the daemon, pool, profile, network,
trust and enrollment; the guest administrator owns guest configuration and
supervision. Keep every credential able to administer that project, including
its restricted service and test peer keys, under the same trusted administration.
Use distinct short-lived service, member and promoter credentials to exercise
the application roles, not to delegate Incus authority. Keep the listener on
an explicitly selected private interface reachable only by the fixture clients.
Do not use real state or issue same-project credentials to ordinary users for
this exercise. If that trust inventory cannot be guaranteed, leave the listener off.

The installed operator path is explicit:

| Who | Supported invocation | Effect / prerequisite |
| --- | --- | --- |
| Incus host administrator | `aginctus internal project-state-attachment --server-id SERVER_ID --project PROJECT --instance INSTANCE --pool POOL --volume VOLUME --device DEVICE --mount-path MOUNT_PATH` (add `--network NETWORK` for the managed NIC) | Read-only [preflight](project-state-service-preflight.md) over the fixed local socket; `SERVER_ID` is an independently pinned `sha256:` fingerprint. Manually provision or verify the exact guest, volume and attachment first. Stop on failure and repeat after replacement or attachment changes. |
| Guest service UID | `aginctus serve project-state bootstrap` | After preflight, with the protected configuration and mounted volume below, prints `initialized` or `reopened`. Does not start a listener. |
| Guest service UID, under administrator supervision | `aginctus serve project-state` | Set `AGINCTUS_PROJECT_STATE_READ_SERVICE_CONFIG` to the [private service configuration](project-state-read-authentication.md#opt-in-read-only-daemon-2026-11-24). Omit `promotionPolicyFile` for reads; set it only for an explicitly approved exact-grant fixture. Keep the host socket and unrestricted keys out of the guest. |
| Administrator-held fixture peer | `aginctus --incus-project PROJECT --environment UUID state show -o json`; `state plan-accept -o json` and `state promote --expect EXPECTED -o json` with the same global selectors | Use the peer's private `AGINCTUS_RESTRICTED_READ_CONFIG`. Planning needs a selected candidate; promotion also needs the enabled route and exact grant. `EXPECTED` is the current revision ID or `genesis`. See the [operator example](project-state-read-authentication.md#provisioning-exact-grants). |

For an administrator-controlled NixOS service guest, import
`nixosModules.aginctusProjectState`, set `services.aginctus-project-state.enable`
and set `services.aginctus-project-state.uid` to the dedicated UID in the
bootstrap config and retained mount. The module installs the nonroot unit
without scheduling it at boot or starting it on module activation. Provision
the independently verified attachment, bootstrap configuration and private
credentials before running `systemctl start aginctus-project-state.service`
inside the guest. The unit runs the same installed bootstrap before opening
the listener. Its service configuration is fixed at `/run/aginctus/state.json`;
the administrator must restage volatile credentials after guest reboot. Use
`systemctl stop aginctus-project-state.service` before reattachment or guest
replacement and repeat host preflight before a manual start on the replacement.

The module does not enroll peers, provision Incus resources or configure Incus
authorization. The VM fixture contains administrator provisioning examples;
it is not an isolation configuration.

### Narrow isolation review

1. **Review same-project Incus authorization first.** Preserve the existing
   server/project binding while allowing only the required project/enrollment
   observations and live peer trust lookups, and denying data-plane access to
   the state guest and volume. Check that the exact TLS identity presented to
   mTLS can actually be subject to that Incus policy: a separate limited
   identity does not remove permissions from the restricted TLS key checked by
   the installed authorizer. If the Incus authorization mode cannot combine
   those requirements, an authorizer/client change is needed; there is no
   supported policy switch in Aginctus today.
2. **Review an administrator-only storage project or daemon only if the first
   option cannot enforce the boundary.** Keep the logical environment binding
   on the workload project and separately verify the physical attachment scope.
   This needs a reviewed change to the current same-project attachment contract
   and an exact, live non-admin authorization proof for workload peers. Merely
   moving the guest and enrolling peers in its new project restores the same
   access problem; issuing multi-project certificates also fails today's exact
   single-project trust check.

### Guest store bootstrap

After an independently authorized administrator verifies the existing service
instance and exclusive writable volume attachment using
[host preflight](project-state-service-preflight.md), the dedicated guest
service UID can invoke `aginctus serve project-state bootstrap`. The one-shot
role takes no arguments or environment overrides. It uses the nonsecret,
guest-administrator-owned `/etc/aginctus/project-state-bootstrap.json`
(root-owned regular file, mode `0644`, no symlinks or hard links):

```json
{
  "apiVersion": "aginctus.dev/project-state-bootstrap/v1alpha1",
  "binding": {"serverId": "sha256:<pinned-Incus-server-certificate-hash>", "project": "team"},
  "serviceUid": 1234,
  "mountPath": "/srv/aginctus-state",
  "operatorConfigFile": "/run/aginctus/restricted-incus.json"
}
```

Provision the selected service-UID-private restricted Incus TLS configuration
independently. The role verifies the exact pinned server/project and requires
an existing service-UID-private dedicated mountpoint. It serializes cooperating
bootstrap calls on the mount and initializes an empty mount using the existing
`environments/` and `authority.json` layout. The authority record is synced and
published last without replacing any existing entry. An exact retained store
reopens without rewriting its authority or revisions. A partial, nonempty
foreign or unreadable mount requires independent inspection; retry does not
adopt or clean it up. The mount check and guest lock do not attest Incus
attachment exclusivity or fence another guest. The role creates no Incus
resource, publishes no accepted revision, and starts no listener.

### Failure, replacement, backup and recovery

- Ordinary restart: retain the custom volume; revalidate project identity,
  schema and active pointer before returning even read-only results.
- Service instance recreation: stop and remove only the known owned **instance**;
  retain the custom volume and backups. Reattach only after verifying the
  replacement instance and volume ownership/attachment against live Incus.
- Partially appended revisions: immutable records not reachable from the
  published active pointer are *orphans*, **not accepted state**. A caller can
  explicitly retry CAS after authoritative re-observation; no auto-promotion.
- Failed publication: report the pointer outcome uncertain, then read the
  authoritative pointer before retrying. Do not treat Incus instance or state
  writes as a transactional rollback.
- Backup/restore: take a consistent quiesced/snapshotted volume backup and
  restore with server/project/environment identities intact. **These steps
  remain unimplemented and unproven**; backing up only the service rootfs or
  local client XDG state is not sufficient.
- External administrator or project member writes are outside Aginctus's
  cooperative lock. A production writer needs authoritative project-scoped
  fencing and fresh Incus ETag/preflight at every mutation.

### Executable evidence

The opt-in test (not a routine PR gate):

```sh
nix build -L --no-link .#vmTests.x86_64-linux.environment-project-state-incus
```

is written to exercise the installed nonroot bootstrap, mTLS state service,
and operator CLI against Incus inside a disposable NixOS VM. It checks the
volume's exclusive `UsedBy` separately, bootstraps an empty mount, denies
promotion to a project member without the exact grant, and accepts two
generation-one revisions through the CLI. After **instance deletion/recreation
with the same external volume**, it checks retained reopen and byte-identical
authority and revision records. The service guest has no local Incus admin
socket. A direct mTLS read checks that the listener denies a revoked member on
a fresh request. The scenario does not contact the development host.

**Evidence is not complete until the test is actually run on an authorized
NixOS/KVM host** and the nonsecret result/version/configuration is recorded
under #695/#701. The scenario uses distinct ephemeral certificates and an
administrator-owned grant file, but does not prove that same-project Incus API
access cannot reach the service guest or volume. It also does not prove
revocation during an operation, power-loss durability, consistent backup and
restore, stale fences, or service upgrades. Keep #695, #696, #701 and #704 open.

For isolation sign-off, first capture the restricted TLS baseline and then
exercise the selected policy on an approved disposable Incus worker, using
the **actual** service and operator identities and synthetic state. Extend the
fixture with paired checks: the administrator can observe the exact resources;
the restricted workload peer can still do required project reads, and the
service can still resolve the live peer record. Against those same existing
resources, require Incus authorization denials for ungranted principals on
guest exec and file access, instance and volume snapshot/backup/export or copy,
volume attachment to another guest, and changes to the service instance,
devices, profiles or authorization. A missing resource, unavailable endpoint,
client-side failure or mTLS denial is not evidence of an Incus ACL denial.

Then require the intended mTLS read and exact-grant CAS to succeed, ungranted
and foreign peers to fail, revoked peers to fail on fresh and in-flight
requests, and retained authority, pointers and revisions to survive verified
reattachment. Record the tested revision, Incus version and authorization mode,
sanitized policy and identities, expected and observed results, and unchanged
state after denials. Any unproven path keeps delegated access off; the existing
fixture alone does not satisfy this gate.

### Production activation gates

Before replacing this fixture with a user-facing `environment accept`
endpoint:

- Prove the [Incus isolation boundary](#narrow-isolation-review) for the exact
  credentials used by the service and its peers before delegating any access.
- Select and implement authenticated project-scoped state-service transport.
  A **caller-supplied** server/project label, a UID or a revision hash cannot
  act as authorization. A shared service must not hold unrestricted Incus
  credentials and act on untrusted project selectors.
- Implement cross-process/cross-principal project-owned CAS and writer fencing,
  revocation checks, durable operation uncertainty and the service's own
  bootstrap/recovery/upgrade procedures.
- Verify exclusive writable volume attachment, foreign resource refusal,
  backup/restore and two-environment isolation using real disposable Incus.
- Keep candidate producers (NixOS, Home Manager, file) nonsecret and independent;
  no module switch automatically promotes or reconciles them.

This is a deliberately **narrow physical choice**, not a universal multi-scope
state framework and not a release sign-off.
