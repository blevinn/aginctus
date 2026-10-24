# Lifecycle unit ownership audit (system vs Home Manager)

The read-only `operator units-audit` CLI helps identify competing Aginctus
**image-update and workload-reconciliation** systemd units before any
operator control-plane handoff ([#441](https://github.com/blevinn/aginctus/issues/441)).

From the **unprivileged operator account** whose future Home Manager user
services would run:

```sh
aginctus operator units-audit --owner system
# or, for a proposed user-owned lifecycle:
aginctus operator units-audit --owner user
```

This command does not read Nix/Aginctus configuration. It rejects global
configuration overrides or `--configuration-file`, never calls `sudo`
or `systemctl start/stop/enable`, and does not read secrets or invoke
Incus. It executes only `systemctl --system show` and
`systemctl --user show`, with a bounded timeout, against this **fixed
allowlist of lifecycle service names**:

- `aginctus-image-herdr-client.service`
- `aginctus-image-opencode-workload.service`
- `aginctus-image-update.service` (reserved user-side generic name)
- `aginctus-reconcile.service`

The returned properties are restricted to `LoadState`, `ActiveState`,
and `UnitFileState`; execution commands, environment variables, drop-ins,
and raw systemctl errors are neither requested nor printed. `active`
and `installed` both count as possible competing authorities, even if
the installed service is currently disabled. `not-found` with consistent
inactive metadata is `absent`; unexpected, denied, offline, masked or
inconsistent replies are `unverified`. Conflicts or any unverified
unit yield a nonzero exit status. The selected owner's own known units
may be installed; another owner's must be absent.

This is **not** evidence of a globally exclusive controller. It inspects
only the caller's user manager and the system manager, not another
user account's `systemd --user`, templated/renamed services, manual
CLI processes, unattended external controllers, image aliases, or Incus
project/resource ownership. A successful report must not authorize
writes; future opt-in user units require mutation-time guards and
explicit project capability checks before any reconciliation. If the
user manager is unavailable (for example, no user bus or lingering),
the result is *unverified*, never permission to proceed.

Host-only Aginctus DNS/DHCP/egress firewall services are intentionally
**excluded**: keeping those root-owned alongside an operator lifecycle
is a supported combined design, not a competing workload controller.

Use [gateway state audit](operator-state-audit.md) separately to detect
credential-state collisions. Neither diagnostic copies, reads, or moves
gateway credentials. Reversible ownership migration belongs to [#442](https://github.com/blevinn/aginctus/issues/442).


## Declared CLI package compatibility

The NixOS and Home Manager modules also publish **nonsecret package
declarations** when enabled. The host writes
`/etc/aginctus/operator-package.json`; Home Manager writes
`~/.config/aginctus/operator-package.json` through XDG. Each contains
only schema version, authority (`system` or `user`) and the precise
Nix store package output path chosen for its Aginctus CLI. These files
contain **no passwords, runtime keys, gateway state or unit environment**.

From the ordinary user account, inspect the intended authority:

```sh
aginctus operator package-audit --owner system
# or, for a planned Home Manager-owned operator lifecycle:
aginctus operator package-audit --owner user
```

The audit does not load application configuration or accept overrides.
It reads only these two fixed, bounded JSON files. It requires the
selected authority to have a valid declaration; missing declarations
for the *other* authority are allowed for single-owner installations.
When both exist, the package store paths must be **exactly equal**.
Malformed/unreadable declarations and different package outputs fail
conservatively. Output reports only `declared`, `absent`, or
`unverified`; it does not print absolute paths or manifest contents.

**This tests the packages declared by Nix, not running-service revision
identity.** A separately installed CLI, external unit, override in a
unit drop-in, stale generation, or another user's service can still run
a different revision. Differences in package derivation also need not
imply protocol incompatibility; until a cross-version compatibility
contract exists, this diagnostic treats them as unverified rather than
assuming safety. It is **not an authorization oracle or write guard**.
Use it alongside `operator units-audit` and `operator state-audit`;
project-scoped alias and mutation-time checks remain prerequisites
before introducing user-owned reconcilers (#200, #437, #441).
