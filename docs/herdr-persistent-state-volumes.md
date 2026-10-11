# Replaceable Herdr client with persistent Incus state volumes

**Status:** first incremental persistent-state slice. This PR attaches **pre-created** custom Incus storage volumes; it does not yet provision volumes automatically or perform a rolling migration of existing containers.

## State boundary

The Herdr client uses two custom volumes in the same configured storage pool as its root disk:

| Incus volume name (default client) | Guest mount | Persisted contents |
| --- | --- | --- |
| `aginctus-herdr-herdr-state` | `/var/lib/herdr` | Herdr server configuration and XDG state (including machines) |
| `aginctus-herdr-ssh-state` | `/var/lib/aginctus/ssh` | Managed client private identity, generations, manifest, and pinned host trust |

The container root filesystem remains disposable. `/run/herdr`, including the server socket, is deliberately ephemeral. Workload filesystems, SSH server host keys and workspaces are **not** covered by these client volumes. A future workload volume policy must decide which workload paths survive replacement, particularly project workspace storage.

The volumes use the configured `infrastructure.herdr.storage.pool` and deterministic names derived from `infrastructure.herdr.name`. They are never created with `force`, silently adopted, or deleted by `herdr client remove`. The Apply driver currently supports instance disk attachments but its native backend does **not** support custom storage-volume resources; provision the volumes separately before running `aginctus herdr client apply`.

**Security:** the SSH state volume contains an unencrypted private SSH key protected by guest permissions. Protect Incus host access, backups, snapshots and volume exports accordingly. Never reuse a state volume for a different security principal. An unexpected identity mismatch must fail closed, not auto-regenerate.

## Fresh fleet reinitialization (destructive)

The operator has explicitly chosen fleet reinitialization instead of in-place container upgrades. This runbook is intentionally staged and **must not** be run blindly: verify the storage pool, attached volumes, active workloads, and backup/revocation requirements before deleting anything.

**First inspect** (read-only):

```sh
incus list --project default
incus storage list
incus storage volume list default --project default
incus config show aginctus-herdr --project default --expanded
incus exec aginctus-herdr --project default -- aginctus-ssh-client-identity inspect
```

The below uses the default client name and the `default` storage pool. Substitute the actual configured pool and client name where different.

1. Stop Herdr sessions and managed workloads; inventory their active authorizations. Treat the prior client identity as compromised/unavailable once the old rootfs is destroyed. Revoke previously authorized workload public keys or delete those workloads before activating a new key.
2. On the old, disposable fleet, explicitly remove the old Aginctus-managed containers using the approved lifecycle commands. **This loses any state not on a separately retained volume.** Take backups or copy required user data before executing.
3. Create two new empty custom volumes, then inspect ownership, content and attachment status:

```sh
incus storage volume create default aginctus-herdr-herdr-state --project default
incus storage volume create default aginctus-herdr-ssh-state --project default
incus storage volume list default --project default
```

4. Import the desired, rebuilt Herdr image and create the client with `aginctus herdr client apply`. The new resource declares both disk devices and should fail if the volumes are missing. Verify mounts before generating keys or starting sensitive operations:

```sh
incus config show aginctus-herdr --project default --expanded
incus exec aginctus-herdr --project default -- findmnt /var/lib/herdr
incus exec aginctus-herdr --project default -- findmnt /var/lib/aginctus/ssh
incus exec aginctus-herdr --project default -- command -v aginctus-ssh-client-identity
```

5. Reinitialize the SSH identity *inside the new client* with the existing helper, provision workload authorization through `aginctus herdr authorize dev`, and verify pinned host trust with `aginctus herdr pin-host dev` after the host firewall is corrected. **Do not reuse authorization from the discarded client key.** The `pin-host` command requires the updated image from PR #129 when it is merged and available.

## Replacement / upgrade acceptance

After the initial reinitialization, a later version of Aginctus should support this sequence without changing the volumes:

- record the currently active client SSH public fingerprint and verify both volumes are attached;
- stop and remove **only the client instance**, not the custom volumes;
- create the client instance from a newer image with the same two volumes attached;
- confirm the private key, managed fingerprint, known_hosts and Herdr configuration survive, and runtime sockets are recreated normally;
- verify no SSH authorization needs to rotate merely because the container image changes; restore and validate remote SSH and Herdr machine sessions.

At present this is a **manual replacement acceptance path**, not an automatic image reconciliation guarantee. The pinned Apply backend rejects unsupported immutable image drift. A future explicit replacement command must add ownership, volume-attachment and identity validation, concurrent-operation locking, and rollback/recovery. Never use `--force` as a substitute for these checks.

### Local validation

```sh
git fetch origin
git switch feat/herdr-persistent-state-volumes
git pull --ff-only

nix develop -c go test ./internal/herdrclient -count=1
nix develop -c go test ./...
nix flake check
nix build .#herdr-client --no-link

# Read-only plan rendering through the ordinary workflow:
nix develop -c go run ./cmd/aginctus herdr client apply --dry-run
```

**Expected:** the Herdr client instance resource declares separate disk devices at `/var/lib/herdr` and `/var/lib/aginctus/ssh`, and the device sources match the volume names. The plan contains no custom volume deletion action. Do not apply the changed plan to an existing rootfs-only client; existing device drift requires explicitly planned fleet reinitialization.

This PR does not promise preservation of existing rootfs state in the legacy deployment. That state is intentionally retired as part of a coordinated, operator-approved fleet reset.
