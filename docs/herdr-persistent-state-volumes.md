# Canonical persistent container state: system and user

**Status:** initial Aginctus implementation. This PR implements the `state-system` volume for the **Herdr client only**. The second, optional `state-user` class and bindings for agent runtimes are a subsequent workload PR. Existing clients must be explicitly reinitialized; in-place upgrades are not supported.

## Shared contract

Every managed container may have **zero, one, or two** independent custom Incus volumes:

| State class | Volume naming convention | Canonical guest mount | Intended ownership |
| --- | --- | --- | --- |
| `state-system` | `<instance>-state-system` | `/var/lib/aginctus/state/system` | Root; operating-system and managed service state |
| `state-user` | `<instance>-state-user` | `/var/lib/aginctus/state/user` | Application/agent configuration and application data |

Both classes are **optional**, chosen by the container's needs, not automatically created for every instance. A volume never contains executable NixOS image code. Workspaces and repositories belong to a separate workspace-sharing/storage policy, not implicitly in `state-user`.

The Incus disk attachment is deliberately independent of application directories. The guest owns fixed, allowlisted bind mounts from subdirectories of the canonical mount to application paths; do **not** mount arbitrary user-configured host paths or trust a volume to choose destinations.

Each volume must belong to one configured instance identity; no reuse across a different workload or user principal. The container rootfs and `/run` remain disposable. Volume deletion must be separately and explicitly authorized; deleting an instance does **not** delete its custom volumes.

## First consumer: Herdr client system state

The client requires **only** `<client-name>-state-system`, attached at `/var/lib/aginctus/state/system`.

The guest `aginctus-state-system-bind` helper mounts only these fixed pairs:

| Persistent subdirectory | Service path | Contents |
| --- | --- | --- |
| `system/herdr/` | `/var/lib/herdr` | Herdr configuration, machine entries, XDG state |
| `system/ssh/` | `/var/lib/aginctus/ssh` | Managed SSH private identity, generation manifest, pinned trust |

The helper executes in a root-run, oneshot systemd unit ordered before and required by `herdr.service`. It fails if the canonical Incus volume is not mounted, if state paths are symlinks or unowned, or if a bind target already contains rootfs state; **no silent fallback to ephemeral state and no automatic migration**. The managed Herdr service cannot start successfully without this unit.

System-state volumes contain unencrypted SSH private key material. Restrict host, guest, snapshots, backups, and export access accordingly. Keep guest system-state subdirectories root-owned and mode 0700. A future user-state consumer must not grant the agent write access to system keys or trust.

The two-class contract is reusable; this PR **does not yet attach user volumes to OpenCode workloads, bind OpenCode configuration, or persist workload SSH host keys**. Those require explicit guest directory and UID/GID design, startup ordering, and tests. This distinction matters when reinitializing the workload fleet.

## Storage provisioning limitation

The embedded native `incus-apply` backend supports instance disk attachments, but not custom storage-volume creation. The volume must exist in the configured pool before `aginctus herdr client apply`. It must not be silently adopted from another principal. Automatic ownership checking and volume creation are follow-up work; currently the operator must inspect the pre-existing volume. Do not use `--force` to bypass this boundary.

## Fleet reinitialization (operator-approved, destructive)

The old Herdr client stores its identity and trust inside the container rootfs. It **cannot** be upgraded in place to this arrangement. Do not delete the old guest without handling its active authorizations, Herdr sessions, and any rootfs data needing preservation.

First inspect, without changing anything:

```sh
incus list --project default
incus storage list
incus storage volume list default --project default
incus config show aginctus-herdr --project default --expanded
incus exec aginctus-herdr --project default -- aginctus-ssh-client-identity inspect
```

These commands assume the configured storage pool is `default` and the client is `aginctus-herdr`. Confirm both from Aginctus configuration. Revoking an old key is essential before deleting its owner if target containers will be retained; alternately delete old target containers as part of the approved reinitialization. Back up any rootfs-only data you need before proceeding.

Only after choosing the reset, create the new, empty client volume:

```sh
incus storage volume create default aginctus-herdr-state-system --project default
incus storage volume list default --project default
```

Then deploy the rebuilt Herdr client image and recreate the client through the approved instance lifecycle. **Do not apply this changed resource to the existing rootfs-only container** and do not treat an immutable drift error as permission to force replacement.

After recreation and *before* generating or authorizing credentials, inspect:

```sh
incus config show aginctus-herdr --project default --expanded
incus exec aginctus-herdr --project default -- mountpoint /var/lib/aginctus/state/system
incus exec aginctus-herdr --project default -- mountpoint /var/lib/herdr
incus exec aginctus-herdr --project default -- mountpoint /var/lib/aginctus/ssh
incus exec aginctus-herdr --project default -- systemctl status aginctus-state-system.service
incus exec aginctus-herdr --project default -- aginctus-ssh-client-identity inspect
```

A fresh identity may not yet exist (inspect exit 3); create it with the existing guest helper and then rerun `aginctus herdr authorize dev`. Follow the strict host trust/probe path from PR #129 after that PR is merged and the host firewall is corrected. Do not treat authorization from the discarded SSH identity as valid.

## Replacement test after initialization

The desired future replace-container workflow is:

1. Confirm the client is owned, stop Herdr sessions, record public SSH fingerprint, check both the volume attachment and its state.
2. Stop/remove **only** the container instance, keeping the state-system volume.
3. Recreate the instance from the new NixOS image with the same canonical volume.
4. Verify the systemd bind unit, persisted identity/fingerprint, pinned host keys and Herdr configuration; runtime sockets are recreated.
5. Verify authenticated remote SSH and Herdr machine sessions with unchanged credentials.

This PR makes the state layout possible, but does **not** implement atomic replacement orchestration or guarantees about conflicting concurrent operators. An explicit, ownership-checked replacement command and volume lifecycle management remain follow-ups.

## Validation

```sh
git fetch origin
git switch feat/herdr-persistent-state-volumes
git pull --ff-only

nix develop -c go test ./internal/herdrclient -count=1
nix develop -c go test ./...
nix flake check
nix build .#herdr-client --no-link

# Read-only render; do not apply against the old fleet.
nix develop -c go run ./cmd/aginctus herdr client apply --dry-run
```

The plan should contain a `state_system` disk device at `/var/lib/aginctus/state/system` with volume `aginctus-herdr-state-system`. It should **not** create a state-user volume for Herdr, attach directly to service directories, or delete custom volumes.

No fleet reinitialization is performed by merging this PR. Confirm the new guest image boots and the bind unit succeeds on an expendable container before touching the production fleet.
