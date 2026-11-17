# Read-only Aginctus unit inventory

The `aginctus operator units-audit` command observes known Aginctus systemd units in the **host system manager and the calling user's manager**. This inventory is **not a system/user controller handoff check** and confers no Incus write authority.

Run it as the unprivileged account whose user units you want to inspect:

```sh
aginctus operator units-audit
```

The command rejects `--owner`, configuration overrides and root invocation. It never invokes `sudo`, mutates systemd state, queries Incus, reads service environment/argv or opens protected credentials. It queries only `LoadState`, `ActiveState`, and `UnitFileState` with a bounded timeout for:

- `aginctus-image-herdr-client.service`
- `aginctus-image-opencode-workload.service`
- `aginctus-image-update.service`
- `aginctus-reconcile.service`

Installed and active are **observations**. Neither is evidence of competing authority or of exclusive controller ownership; both manager inventories may contain the same names. Unsupported/ambiguous/denied systemctl responses are `unverified` and cause nonzero exit. `not-found` with consistent inactive evidence is `absent`.

A complete report covers only these **two managers**. It cannot enumerate other users, renamed/templated units, manual reconciler processes, Incus administrators, projects, remote clients or environments. Project authority follows Incus credentials; each environment has its own accepted revision history and lifecycle ownership within its project. Authenticating project-state writers, fencing mutations and recovering from crashes remain requirements under [#696](https://github.com/blevinn/aginctus/issues/696) and [#704](https://github.com/blevinn/aginctus/issues/704).

Host firewall and network enforcement units are outside this workload inventory because they remain independently root-owned. The separate [gateway credential metadata audit](operator-state-audit.md) has historical `--owner` semantics pending its own host-diagnostic replacement; it never authorizes Incus mutation.

The former `operator package-audit --owner system|user` and its Nix package declaration manifests have been retired. A process's declared Nix output is not verification of running-service protocol compatibility or project authority.
Use [gateway state audit](operator-state-audit.md) separately to detect
credential-state collisions. Neither diagnostic copies, reads, or moves
gateway credentials. Reversible ownership migration belongs to [#442](https://github.com/blevinn/aginctus/issues/442).


## Package-version diagnostics

The former `operator package-audit --owner system|user` command and its
NixOS/Home Manager `operator-package.json` parity manifests were retired.
They compared package *declarations* to the current CLI executable, not to
running services, authenticated project sessions or protocol compatibility.
An ordinary Nix profile switch remains an installation concern; it is not
an accepted-state promotion or Incus project authorization grant.

Inspect actual running service identity, project-level accepted revisions and
independently authenticated Incus credentials through their owning subsystems.
No replacement package-identity check has been activated in this cleanup.
