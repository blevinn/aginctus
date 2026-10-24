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
