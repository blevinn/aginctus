# Incus operator capability preflight

Status: **read-only diagnostic** (initial #437 slice; no authorization grants).

The CLI can check daemon visibility and selected-project instance read access
without changing the user's ambient project:

```sh
aginctus operator preflight --project aginctus
```

The project is **required** and explicit. The command runs only
`incus info`, `incus list --project NAME --format json`, and optionally
`incus project list --format json`, with a bounded timeout. It does not create,
delete, configure, or switch projects, bridge networks, volumes, instances,
trust grants, or firewall objects, and never calls `sudo`. Incus authorization
or daemon initialization can have server-side effects beyond this CLI's
control; this is not a substitute for a separately provisioned identity.

A successful preflight proves **only** daemon and selected-project read
access. The number of visible projects is diagnostic context, **not** proof
of write permission or unrestricted administrator status. A denied project
read fails before any subsequent probe. No raw subprocess diagnostic data or
credential material appears in output. This deliberately cannot certify
permissions for image alias updates, storage, host Linux bridges, or root
firewall operations; the operations remain disabled pending #200 and #438/#439.

Incus documents that local `incus-admin` members have full host-equivalent
API access while confined `incus` group members are restricted to personal
projects. Restricted TLS clients may see one or more approved projects.
The current Incus authorization backend and effective permissions cannot be
proven merely from Unix group membership or a successful read. For an
unavailable daemon, inspect the selected Incus remote and host-side
authorization with an administrator; do not grant `incus-admin` as a
routine troubleshooting step.

For full Home Manager operator setup see [Home Manager](home-manager.md).
#437 also tracks future project-aware authorization integration, read-only
identity classification beyond visibility, and consumer unit guards.


## Dedicated project policy planning (#200)

To inspect the proposed v0.2 single-deployment project policy without
creating or changing Incus objects, run:

```sh
aginctus operator project-plan --project aginctus
```

`operator project-plan` accepts only an explicit valid **non-`default`**
project identity. It issues exactly `incus project list --format json`
with a bounded timeout and produces one machine-readable JSON object:

```json
{
  "project": "aginctus",
  "status": "compatible",
  "desiredConfig": {
    "user.aginctus.managed": "true",
    "user.aginctus.resource": "deployment-project",
    "user.aginctus.deployment": "aginctus",
    "features.images": "true",
    "features.profiles": "true",
    "features.storage.volumes": "true",
    "features.networks": "false"
  }
}
```

The status may be `compatible`, `foreign`, `policy-drift`, or
`not-visible`. Only `compatible` exits successfully. An existing
same-name project whose ownership markers do not match is `foreign`
even when its feature policy matches. Missing required feature flags or
changed values are `policy-drift`. Because project enumeration can be
restricted, `not-visible` **never proves a project is absent** or safe
to create. Enumeration failures, malformed responses, and duplicate
identities fail without claiming a valid plan.

This policy selects project-local images, profiles and custom volumes;
it intentionally keeps `features.networks=false`: the existing
Btrfs/Incus-managed Linux bridge stays a **host-level** resource and is
not adopted by an operator-owned project. The storage pool itself
remains host-managed; project-local volume names do not imply isolated
pool allocation. Do not assume an inherited `default` profile or
introduce OVN automatically.

**Deployment compatibility:** This first slice is a read-only planning
API, **not** the active v0.2 project selector. Existing v0.1 CLI
apply/remove/status, guest instances and the LiteLLM Compose project
retain their current behavior. #200 still needs carefully gated project
provisioning and explicit project identity threaded through every API
and subprocess. #201 co-locates the gateway and workloads and #202
owns verified migration with backups and rollback. Do not run these
operations on the live pilot merely because this diagnostic passes.


## Explicit local project provisioning (#200, second slice)

`project-plan` remains read-only. The separately invoked
**create-only** command provisions a new dedicated Aginctus project on
the **local** Incus daemon:

```sh
aginctus operator project-ensure --project aginctus --confirm-create
```

This is a **real mutation**. Run it only on an explicitly authorized
disposable test host or as a deliberate provisioning step. It never
uses `sudo`, `incus project switch`, or the caller's current Incus
project. The Incus invocation includes `--force-local`; it will not
implicitly create a remote project. It requires the explicit
`--confirm-create` flag even when an existing project might be reused.

The command first inspects `incus --force-local project list
--format json` and applies the same exact policy defined above.
If the desired project is already **compatible**, it reports
`{"project":"aginctus","result":"existing","status":"compatible"}`
without making any change. If it is `foreign` or
`policy-drift`, it refuses to modify it: no adoption, metadata repair,
renaming, or deletion.

If enumeration reports `not-visible`, **do not infer absence**.
Only an explicitly requested, atomic `incus --force-local project
create` with all required `--config` flags is attempted. Incus
itself rejects an existing name, including a foreign project that
was hidden from the initial enumeration. A successful creation is
reinspected and checked against the exact ownership and feature policy
before success is reported. If verification fails, the command reports
an uncertain result and **never automatically deletes or rolls back**
the created project. The administrator must inspect it before retrying.

The command creates **only** the project object. It does not initialize
profiles, configure a root disk, grant network access, import images,
create volumes/instances, or make a host bridge. Project-local images,
profiles and storage volumes are selected by the policy, but the
existing Linux bridge and Btrfs pool remain host-level. Existing v0.1
Aginctus lifecycle commands still target their prior projects until
#200's explicit scope threading, followed by #201 gateway co-location
and #202 migration. A successful ensure is **not** authorization to
reconcile workloads or move persistent state.
