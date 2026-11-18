# Gateway credential metadata inspection

The read-only `operator state-audit` command checks one explicitly selected **filesystem location** for gateway credential metadata. It is not a host/user lifecycle handoff oracle, an Incus project credential check, or an accepted-state promotion.

Run as the principal that owns the selected tree:

```sh
# From root, to inspect the independent host-managed credential tree:
aginctus operator state-audit --location system

# From the unprivileged account, for that account's user credentials:
aginctus operator state-audit --location user
```

The old `--owner system|user` selector is removed. `--location` denotes where metadata is inspected, **not** who owns reconciliation. System location requires effective UID 0; user location requires an unprivileged caller. The gateway ID is taken from the caller's explicit effective `gateway.id`. Host state lives at `/var/lib/aginctus/state`; user state uses the existing `filestate.StateRoot` resolution. A user never receives permission to read root secrets through this command.

The diagnostic examines one `aginctus/gateway/<gateway.id>/secrets.json` tree using `lstat` only. It never opens or prints secret material, follows symlinks, copies state, changes ownership or invokes sudo/systemd/Incus. It verifies the relevant UID, non-writable directory parents, private gateway directories and restrictive regular-file modes. Nonexistent trees are reported `absent`; missing secret file after directory creation is `incomplete`; unreadable/unknown metadata is `unverified`; symlink, foreign-owner and broad permissions are `unsafe`. Incomplete, unverified and unsafe return nonzero. Both present and absent are observational results, never evidence of execution authorization.

Two principals may each have independent credential files; this **does not** mean both are reconciling the same project environment. Conversely, a matching file cannot prove that a gateway is running with it. Actual project authority requires Incus credentials, immutable environment binding, project-scoped accepted revision, controller fencing and fresh resource checks.

The file metadata check does not establish a secure root when a higher filesystem ancestor is controlled by an adversary. Actual credential reads/writes retain descriptor-based no-follow checks in the file-state owner. This command does not replace the native executor security gate [#704](https://github.com/blevinn/aginctus/issues/704).

The separately scoped [unit inventory](operator-units-audit.md) is also non-authorizing. The retired package-manifest parity audit is not a protocol compatibility or running-service version check.
