# v0.2 design: live host workspace sharing

Status: **proposed design, not implemented**. This document records the agreed
v0.2 contract; examples are not yet accepted by the CLI or NixOS module.
See [v0.2 release tracker](https://github.com/blevinn/aginctus/issues/173) for release scope and acceptance.

## Goal

An Aginctus-managed coding workload can operate directly on an existing
directory on its Incus host. Changes made by the host developer or the guest
`agent` account are immediately visible to the other side. The host directory
is authoritative: Aginctus neither copies nor synchronizes its contents.

The Herdr client remains an SSH control/presentation environment. It does **not**
receive the host mount. Only the explicitly authorized agent workload receives
a filesystem device.

## v0.2 scope and non-goals

The initial supported target is an **unprivileged Incus container**, using the
supplied non-root OpenCode `agent` account. Design the workload contract to
remain meaningful for Hermes and future VM workloads, but do not claim
functional VM workspace sharing until its permissions and lifecycle have been
validated independently.

v0.2 provides one optional primary host workspace per workload. It does not
provide snapshots, overlays, isolated working copies, merge/export flows,
synchronization, multiple arbitrary mounts, remote/multi-host sharing,
automatic host user/group administration, or a general host-path passthrough
API. The managed guest's own runtime/configuration state is separate from its
host workspace.

## Configuration: workload intent

A workspace is opt-in. The illustrative configuration extends the existing
`workloads.<id>` entry:

```json
{
  "workloads": {
    "dev": {
      "workspace": {
        "source": "/home/user/projects/aginctus",
        "target": "/workspace",
        "access": "read-write"
      }
    }
  }
}
```

| Field | Contract |
| --- | --- |
| `source` | Required, absolute, existing directory on the **Incus server host**; never created by Aginctus. |
| `target` | Absolute directory in the guest; defaults to `/workspace`. |
| `access` | `read-only` (default) or explicitly requested `read-write`. |
| `workspace` omitted | No host directory shared. |

`/home/agent` remains the guest's home and contains no host workspace mount.
Agent sessions should start in `/workspace` when a workspace is configured.
Explicit writable access is appropriate for coding tasks; it is not a global
default.

Configuration is workload intent, not raw Incus configuration. A container
implementation can generate a device conceptually equivalent to:

```yaml
workspace:
  type: disk
  source: /home/user/projects/aginctus
  path: /workspace
  readonly: "false"
  required: "true"
  recursive: "false"
  shift: "true"
```

This is illustrative generated Incus configuration, **not** a user-facing
device map. `shift=true` is container-only and must be capability-checked.
No managed Incus storage volume or image contents own the source directory.
For Incus device semantics, see the
[Incus disk device reference](https://linuxcontainers.org/incus/docs/main/reference/devices_disk/).

## Host policy: optional workspace roots

Ordinary workload settings use Aginctus's configuration cascade and may be
overridden through user configuration, environment variables, or CLI flags.
Security policy must **not** live in that cascade: an ordinary workload
override must not be able to relax the host's sharing policy.

Introduce an independently loaded, host-administrator-controlled file:

`/etc/aginctus/policy.json`

```json
{
  "workspace": {
    "allowedRoots": ["/home/user/projects", "/srv/agent-workspaces"]
  }
}
```

`allowedRoots` is a three-state value:

| Host policy | Effect |
| --- | --- |
| File/key omitted or `null` (default) | No additional allowlist restriction; still requires explicit workload sharing and baseline checks. |
| `[]` | Deny **all** host workspace mounts. |
| Nonempty list | Source must be the root itself or a descendant of an allowed root. |

This is **not** an automatic mount list. An allowed root authorizes potential
sources; each workload still needs its own explicit source and access grant.
An empty array must never be treated as equivalent to an absent policy.

For NixOS, the proposed typed option is
`services.aginctus.workspace.allowedRoots`, with type
`nullOr (listOf str)` and default `null`, outside
`services.aginctus.settings`. For example:

```nix
services.aginctus = {
  enable = true;
  workspace.allowedRoots = [ "/home/user/projects" ];
  settings.workloads.dev.workspace = {
    source = "/home/user/projects/aginctus";
    target = "/workspace";
    access = "read-write";
  };
};
```

Use strings, not Nix `path` option values, so an existing checkout is never
implicitly copied into the Nix store. The module generates the host policy
independently of `/etc/aginctus/config.json`; the policy contains no secrets.
Validate option types and absolute-path syntax at Nix evaluation, and check
real filesystem state at runtime.

The policy file must come from a host-administrator-controlled location (or
NixOS-generated equivalent), not a user-selectable alternative. A malformed or
unreadable present policy must fail closed; only genuine absence selects the
default `null` policy. Normal `--config`, `--configuration-file`, environment,
and user configuration values cannot override it. Do not expose a
`--force` policy bypass.

## Host source and guest target validation

Validate before **any** Incus mutation on an ensure request, including when an
instance already exists:

1. Require an existing directory at an absolute source path on the Incus
   server host, never the CLI client's remote filesystem. Resolve the source
   and every configured allowed root to canonical paths.
2. Compare containment by **path components**, not string prefixes; equality
   with an allowed root is permitted. Reject symlinks that escape the allowed
   roots after resolution.
3. Reject the host filesystem root and protected operating-system/control
   paths regardless of allowed-root policy (including `/etc`, `/proc`,
   `/sys`, `/dev`, `/run`, `/root`, `/nix/store`, and Incus state paths).
   Compare canonical paths and protected descendants. Account for sensitive
   per-user directories when specifying the final protected-path policy.
4. Reject a guest target that replaces the root filesystem, the agent home,
   or protected guest runtime, SSH, or Aginctus state directories. Resolve
   target path conflicts with existing managed devices.
5. Do not request recursive mounting of nested host mount points. This
   reduces accidental breadth but is not a substitute for auditing what a
   permitted source directory exposes.

Path checks must avoid time-of-check/time-of-use substitution of a verified
source. The implementation must establish a safe canonical-path/device
handoff (or fail where parent paths cannot be trusted), and recheck the
realized device on subsequent reconciliation. A policy allowlist is an
Aginctus safety guard, not a hard boundary against someone who already has
full Incus/host administrative authority.

The entire shared tree is writable when read-write is granted, including
repository metadata and executable code. Do not assume Git, symlinks,
subdirectories, or `recursive=false` provide additional isolation.

## Identity mapping and permissions

The supplied workload runs Herdr/OpenCode under the non-root `agent` account.
For v0.2, retain unprivileged containers and prefer an Incus disk device with
`shift=true`, where the kernel, filesystem, Incus configuration, and active
identity mapping support it.

The desired result is that `agent` can operate on developer-owned files and
the developer can operate on `agent`-created files **without changing host
ownership or permissions**. Discover the host source owner UID/GID and the
guest's numeric agent UID/GID. Do not assume equal numbers alone prove a
working mapping, or that `shift=true` remaps arbitrary mismatched identities:
verify actual effective ownership and access after attachment.

Unsupported idmapped mounts, incompatible IDs, unsafe permissions, or
failed guest access are actionable errors. Do **not** silently enable
`security.privileged`, inject `raw.idmap`, recursively `chown`, alter host
ACLs/modes, or manufacture a host-side service account. Customized mappings
and dedicated host identities are future features.

Read-only enforcement uses the mount's `readonly` device setting, not only
filesystem DAC bits. Unit tests should validate the generated flags; disposable
Incus integration tests must verify a write is actually denied.

`--dry-run` may read metadata and report planned capability checks but must
not mutate the workspace to test write permissions. Integration validation
can exercise read/create/modify/rename/delete in a **disposable test
directory** as the guest `agent`, then check usable host ownership. A dry run
must not report access as verified if it was not tested.

## Lifecycle, ownership, and revocation

The named device is Aginctus-owned and should be stable across ensures.
Changes to existing devices must never silently overwrite a foreign mount,
root disk, management NIC, or any extra device Aginctus does not own. The
current workload Apply resource supplies a complete device map, and upstream
`incus-apply` can replace an existing device set: implementation must guard
against unexpected device deletion or replacement, not assume additive merges.

| Operation | Required behavior |
| --- | --- |
| Initial `workload <id> ensure` | Validate policy, paths, identity capabilities; attach the authorized mount and start the workload. |
| Repeated `ensure` | Idempotent; verify the realized device and policy, including drift. |
| `ensure --dry-run` | Report desired mount, access, policy decision, and anticipated mutations without changing host or guest files. |
| Change `source`, `target`, or `access` | Reconcile only the owned device if safely supported live; otherwise fail with an explicit interruption/recreation requirement. |
| Remove workspace from workload configuration | Revoke/detach the owned workspace mount; verify it is no longer accessible. Do not delete source files. |
| Tighten allowed roots | Existing now-disallowed attachments are noncompliant until detached; a failed ensure alone must **not** be reported as successful revocation. Provide an explicit stop/detach recovery path if live detachment is unsupported. |
| `workload <id> teardown` | Remove the workload and its device independently of current allowlist policy; never delete the host directory. |
| `--force` | Does not bypass workspace policy or permit unexpected foreign device replacement. |

Reconciliation should not restart a running coding agent implicitly merely to
apply a changed mount. If an interruption is required, surface it and require
an explicit operator-approved lifecycle action. The host source being removed
or becoming inaccessible is a reconciliation failure, not a reason to create
an empty replacement directory.

A restored/recreated workload points at the same host checkout; no workspace
data is migrated. Workspace file contents, Git history, and host ownership
survive guest recreation and teardown.

## Implementation boundaries

- Add a typed workspace model to `internal/workload` with safe defaults and
  policy validation performed before orchestration.
- Load host policy separately from `internal/config`'s normal override
  precedence; provide an injectable source for unit tests without public
  user-overridable policy paths.
- Extend the embedded workload Jsonnet resource to generate only the
  Aginctus-owned device after validation, preserving root/NIC and foreign
  device invariants.
- Verify the actual behavior of the pinned Apply driver for device changes,
  dry-run, and ownership. Introduce explicit reconciliation if required;
  never rely on a silently ignored unsupported field.
- Expose the proposed NixOS policy and workspace options, ensure NixOS
  reconciliation responds to policy/configuration changes, and document the
  selected guest user/working directory contract.
- Add read-only status/diagnostics identifying desired source, target,
  permissions, realized mount, and noncompliance without exposing secrets.

## Acceptance: disposable host and guest

On a disposable Incus host with a disposable Git checkout:

1. Create a workload with no workspace: no host directory is exposed.
2. Explicitly share a checkout read-write at `/workspace`. Observe edits
   immediately in both directions and start OpenCode in the workspace.
3. As `agent`, read/create/modify/rename/delete files; confirm resulting
   host ownership is usable and host metadata was not rewritten wholesale.
4. Verify read-only mounting prevents guest writes.
5. Verify `null`, `[]`, and a nonempty `allowedRoots` list are distinct;
   deny traversal, prefix confusion, escaping symlinks, protected paths,
   missing sources, and guest target conflicts.
6. Verify repeated `ensure` is a no-op; safely reject foreign-device
   collisions and unsupported source/access drift, including when
   `--force` is supplied.
7. Verify policy tightening never reports a stale mount as revoked;
   exercise explicit revocation and confirm guest access is gone.
8. Verify `teardown` and workload replacement leave checkout contents and
   ownership unchanged; verify dry-run is non-mutating.
9. Verify unsupported idmapping fails closed without privilege escalation
   or filesystem permission changes.

The initial release gate is the NixOS/Incus **container** path. VM VirtioFS/9p
sharing and VM identity semantics require a later, independent acceptance
gate. See [Incus disk devices](https://linuxcontainers.org/incus/docs/main/reference/devices_disk/)
and [Incus user namespace ID mappings](https://linuxcontainers.org/incus/docs/main/userns-idmap/).

## Relationship to the release plan

v0.1's basic agent workspace requirement can be satisfied by a guest-local or
provisioned test repository; **live host sharing is not a v0.1 prerequisite**.
v0.2 includes live sharing alongside the network segmentation effort
tracked in [issue #119](https://github.com/blevinn/aginctus/issues/119).
This design adds no implemented behavior by itself.
