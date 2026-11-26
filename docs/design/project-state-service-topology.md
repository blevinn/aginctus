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
5. Start the service under a nonroot UID. The experimental listener is Unix
   same-UID/read-only and **not** reachable by external project principals.
   The eventual production transport requires a separate restricted-project
   credential or equivalent server-authenticated, no-root-deputy mechanism;
   no generic root-mediated user request proxy is acceptable.

The admin-owned bootstrap path must remain outside accepted-state convergence
or the service would depend on its own unavailable accepted pointer to recover.

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

creates a disposable NixOS VM and its own Incus daemon, project, custom
volume and service instance. It explicitly accepts two distinct environments
in the same project with generation-one revisions, checks stale-CAS failure,
the same-UID Unix reader's cross-UID/POST refusal, service restart, and
**instance deletion/recreation with the same external volume**. It verifies
accepted record hashes and identities persist and the guest does not see a
local Incus admin socket. It does not contact the development host.

**Evidence is not complete until the test is actually run on an authorized
NixOS/KVM host** and the nonsecret result/version/configuration is recorded
under #695/#701. The test is not a restricted TLS multi-principal service
and does not prove cross-project read denial through a production transport,
AppArmor constraints, revocation during an operation, power-loss durability,
consistent backups/restores, stale fences, or service upgrades. Keep #695,
#696, #701 and #704 open.

### Production activation gates

Before replacing this fixture with a user-facing `environment accept`
endpoint:

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
