# Gateway state ownership preflight

This is a **metadata-only** diagnostic for the v0.2 Home Manager/system
control-plane handoff ([#441](https://github.com/blevinn/aginctus/issues/441)).
It does **not** reconcile a gateway, grant access to an Incus project, or
authorize credential migration.

Use it with the same effective nonsecret Aginctus configuration as the
intended operator:

```sh
aginctus operator state-audit --owner system
# OR, when deliberately auditing a user-owned gateway:
aginctus operator state-audit --owner user
```

The explicit owner selector is mandatory. The selected gateway ID comes
from `gateway.id` in the effective configuration. The system owner uses
`/var/lib/aginctus/state` as its `XDG_STATE_HOME`; the user owner uses
`XDG_STATE_HOME` or the conventional `~/.local/state` base, resolved with
the same `filestate.StateRoot` helper used by the gateway.

The audit checks **only filesystem metadata**, using `lstat` on the
`aginctus/gateway/<gateway.id>/secrets.json` tree under each root.
It never opens `secrets.json`, follows a final symlink, prints secret
contents, creates directories, copies state, changes ownership, or calls
`sudo`, `systemctl` or Incus. It checks directory owners, private
gateway directories (0700-class), non-writable parent directories,
and private credential-file permissions (0600-class); symlinks,
misowned/nonregular files and group/world-readable credentials fail
closed. Metadata under an inaccessible root is **unverified**, not absent.

The two observed roots must remain distinct. If **both** credential files
are observed, or a credential file exists only under the *other*
authority, the audit rejects the proposed owner. Unverified,
misowned, incomplete or unsafe state also yields a nonzero result.
An absent state under both authorities is not an error for a CLI-only
Home Manager installation, but is **not proof that a gateway exists**.

**Scope and limitations:**

- The outcome is not a credential or gateway API authentication test.
  In particular, a matching file does not prove the running LiteLLM
  gateway is using that owner or that service versions match.
- The diagnostic does not inspect live user/system systemd units, image
  aliases, Incus projects, volumes, foreign resources or workload
  reconcilers. It is a first step, **not** the write guard required
  before #438/#439, and not a migration tool (#442).
- Root-only state directories may be opaque to an ordinary operator.
  Run the audit through the authorized host owner when read access
  is denied; do not change modes or copy root-owned secrets to make
  the output look successful.
- The metadata audit does not establish a secure filesystem root if
  higher ancestors are controlled by another party. Actual reads and
  writes retain the existing descriptor-based, no-follow security
  checks in the Aginctus file-state layer.

On a conflict, stop before attempting operator handoff. Resolve the
authoritative service, project, state root, backups and rollback
explicitly through [#442](https://github.com/blevinn/aginctus/issues/442).
