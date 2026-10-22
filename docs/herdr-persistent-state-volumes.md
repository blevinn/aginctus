# Canonical persistent state volumes for managed containers

**Status:** initial implementation for the Herdr client. The host can run any OS supported by the local Incus API; **NixOS is required only inside the current guest images**. Nothing in this PR updates or recreates the existing fleet.

## Shared contract

A managed container may request zero, one, or two independent custom Incus filesystem volumes:

| State class | Volume name | Canonical guest mount | Intended data |
| --- | --- | --- | --- |
| `state-system` | `<instance>-state-system` | `/var/lib/aginctus/state/system` | Root-managed keys, trust, service state |
| `state-user` | `<instance>-state-user` | `/var/lib/aginctus/state/user` | Agent settings and application data |

The volumes remain separate from the replaceable root disk and are retained after container deletion. User workspaces follow the separate workspace-sharing policy. The two classes are independently optional: the Herdr client currently requests **only system state**. Agent user-state integration is still a follow-up.

## Host-independent volume provisioning in Aginctus

`aginctus herdr client apply` and top-level `aginctus apply` both render an explicit `herdr-system-state` orchestration step. The state-volume driver uses the local Incus Go API to ensure the `<client>-state-system` volume exists in the configured storage pool **after network reconciliation and before the Herdr instance**. No host shell script, Nix command, or host NixOS module is required. Rendered plans and `apply --from` preserve the ordered prerequisite.

- Missing volume: create a `custom` filesystem volume with `user.aginctus.managed=true`, `user.aginctus.resource=state-volume`, `user.aginctus.instance=<client>`, and `user.aginctus.state-class=system`.
- Existing volume: verify the expected name, type, filesystem content and **all ownership markers**; refuse foreign or unmarked volumes.
- Existing same-name instance: verify Aginctus ownership before creating the volume; refuse collisions.
- `--dry-run`: do not create or mutate volumes. The Herdr plan now includes a separate state-volume step; its dry-run validation checks the request but does not inspect actual Incus volume availability.
- `herdr client remove`: deletes only the client instance and retains its volumes. Removing a volume requires a separate, explicitly destructive lifecycle action not provided by this PR.
- Concurrent creators can race: the losing creator fails and may retry the full reconciliation after checking the resulting ownership. Do not silently adopt another volume.

The CLI remains responsible for orchestrating volume provisioning even if a guest is not NixOS; subsequent container runtimes can consume the same Incus volume contract.

## Guest state bindings: declarative, not a Herdr-aware helper

The NixOS guest image imports reusable `nix/guest-state-bindings.nix`. The module takes an allowlisted list of state class, subdirectory and destination; it generates a preparation service and systemd bind mount units. No `guest/state/bind-system.sh` is shipped.

The Herdr image alone declares:

| State subdirectory | Bind-mounted destination |
| --- | --- |
| `system/herdr/` | `/var/lib/herdr` |
| `system/ssh/` | `/var/lib/aginctus/ssh` |

The preparation service refuses missing canonical volume mounts and unexpected symlinks or nonempty rootfs bind targets; each directory is created with root-owned restricted permissions. Systemd requires the prepared bind mounts before starting `herdr.service`. This deliberately fails closed instead of using disposable rootfs state when the volume is missing.

These guest-specific declarations are not part of the host CLI, which does not need to know Herdr's directories. Workload agents will declare their own subdirectories later. Any non-NixOS guest implementation needs to honor the same mount, ownership and startup contract.

## Reinitialize old fleet explicitly

Legacy client containers place SSH identity and Herdr state on the root disk. They must **not** be updated in place: the current native Apply backend rejects some immutable instance changes, and a new bind mount would hide old rootfs contents. The user has opted for an explicit fleet reset; do not initiate it simply by merging this PR.

Start with **read-only inspection**:

```bash
incus list --project default
incus storage list
incus storage volume list default --project default
incus config show aginctus-herdr --project default --expanded
incus exec aginctus-herdr --project default -- aginctus-ssh-client-identity inspect
```

Before deletion, record which workloads have the old client's public key authorized. Either revoke those keys through the existing workload helper or reinitialize those workload instances, after saving anything still needed. Deleting the rootfs loses unsaved Herdr configuration, SSH identity, and other state; credential renewal is mandatory unless migration is explicitly engineered. Check backups, workspaces, and active sessions.

After a separately approved reset and rebuilding/importing the desired image, **do not manually create the custom volume**. Use the Aginctus CLI:

```bash
nix develop -c go run ./cmd/aginctus herdr client apply
echo "Apply exit status: $?"
```

It creates the owned system volume and declares its Incus disk attachment. Verify:

```bash
incus storage volume show default custom/aginctus-herdr-state-system --project default
incus config show aginctus-herdr --project default --expanded
incus exec aginctus-herdr --project default -- mountpoint /var/lib/aginctus/state/system
incus exec aginctus-herdr --project default -- mountpoint /var/lib/herdr
incus exec aginctus-herdr --project default -- mountpoint /var/lib/aginctus/ssh
incus exec aginctus-herdr --project default -- systemctl status aginctus-state-prepare.service
```

Check mount status and guest service health before generating a new SSH key. Only then authorize managed workloads, pin host trust, and validate remote SSH. The current host firewall issue is independent.

## Later image replacement acceptance

A future explicitly supported replace-container command should stop the client, verify volume ownership and attachment, retain `state-system`, remove only the old instance, create from the new image with the same volume, and confirm unchanged SSH public fingerprint, host trust and Herdr machines. No key rotation should be necessary for an ordinary image upgrade. The current state-volume operation does **not** by itself automate safe replacement, versioned rollback, or concurrent operator exclusion.

## Development validation

```bash
nix develop -c go test ./internal/statevolume ./internal/herdrclient -count=1
nix develop -c go test ./...
nix flake check
nix build .#herdr-client --no-link

# Read-only with respect to storage and containers:
nix develop -c go run ./cmd/aginctus herdr client apply --dry-run
```

All commands above except the explicit post-reset apply are nondestructive. Do not run a live apply against an old rootfs-only client; the first deployment requires the approved reinitialization.
