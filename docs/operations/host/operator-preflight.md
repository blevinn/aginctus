# Incus operator capability preflight

Status: **read-only diagnostic** (initial #437 slice; no authorization grants).

The CLI can check daemon visibility and selected-project instance read access
without changing the user's ambient project:

```sh
aginctus operator preflight --project aginctus
```

The project is **required**, explicit and non-`default`. The command uses
GET-only `incus query --raw --request GET` requests for `/1.0?project=NAME`
and `/1.0/instances?project=NAME&recursion=1`, plus optional
`incus project list --format json`, with a bounded timeout. When the daemon
identifies the caller by a TLS certificate fingerprint, it also attempts a
GET of that exact `/1.0/certificates/FINGERPRINT` to inspect its restriction.
It does not create,
delete, configure, or switch projects, bridge networks, volumes, instances,
trust grants, or firewall objects, and never calls `sudo`. Incus authorization
or daemon initialization can have server-side effects beyond this CLI's
control; this is not a substitute for a separately provisioned identity.

A successful preflight proves **only** a trusted daemon identity and
selected-project instance read access. For TLS identities with a readable
self-certificate, diagnostics distinguish a restricted credential that
declares the selected project from a broad unrestricted credential. A missing
restriction, mismatched fingerprint or denied certificate read remains
unproven. Unix, OIDC and other identities are not classified as administrators
from their authentication method or username. Even an unrestricted TLS
credential is **not proof of effective administrative capabilities**: an
external authorizer may impose additional restrictions.

The number of visible projects is diagnostic context, **not** proof
of write permission or unrestricted administrator status. A denied project
read fails before any subsequent probe. No raw subprocess diagnostic data or
credential material appears in output. This deliberately cannot certify
permissions for image alias updates, storage, host Linux bridges, or root
firewall operations; the operations remain disabled pending #200 and #438/#439.

Structured API 401 and 403 responses distinguish unauthenticated and denied
requests. Missing CLI, local permission denial, cancellation, deadline and
invalid responses have separate sanitized statuses. A subprocess transport
failure remains `unavailable-or-denied`: the CLI may fail before returning a
structured API response, so its stderr is neither parsed nor printed as an
authorization oracle. Verify remote reachability and pre-provisioned identity
with the host administrator. Cancellation during an optional probe fails the
whole check rather than reporting success. No fallback identity is attempted.

Exit status zero remains a **read-only diagnostic success**, never a write
authorization or a prerequisite sufficient to enable user lifecycle services.

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
project. Mutation uses the official Incus SDK with a verified local Unix
connection and explicit project identity; it will not implicitly create a
remote project. It requires the explicit
`--confirm-create` flag even when an existing project might be reused.

The command first inspects `incus --force-local project list
--format json` and applies the same exact policy defined above.
If the desired project is already **compatible**, it reports
`{"project":"aginctus","result":"existing","status":"compatible"}`
without making any change. If it is `foreign` or
`policy-drift`, it refuses to modify it: no adoption, metadata repair,
renaming, or deletion.

If enumeration reports `not-visible`, **do not infer absence**.
The native helper independently reads the exact project before preparing a
mutation and again at the mutation boundary. It requires a confirmed request,
local transport and the selected nondefault project. Denied or unknown reads
fail closed. Only a verified not-found response permits an atomic SDK
`CreateProject` request with ownership markers and feature flags in the initial
body. Incus itself rejects an existing name, including a foreign project hidden
from the initial enumeration or created during the final race window. No
metadata is stamped onto a preexisting object. A successful creation is
reinspected natively and checked against the exact ownership and feature policy
before success is reported. If verification fails, the command reports
an uncertain result and **never automatically deletes or rolls back**
the created project. A lost or canceled create response can also leave a project
present; cancellation is an error, not a successful no-op. The administrator must
inspect the selected project before retrying. The SDK helper's partial result
distinguishes an attempted request, a confirmed create and verified postflight.

The command creates **only** the project object. It does not initialize
profiles, configure a root disk, grant network access, import images,
create volumes/instances, or make a host bridge. Project-local images,
profiles and storage volumes are selected by the policy, but the
existing Linux bridge and Btrfs pool remain host-level. Existing v0.1
Aginctus lifecycle commands still target their prior projects until
#200's explicit scope threading, followed by #201 gateway co-location
and #202 migration. A successful ensure is **not** authorization to
reconcile workloads or move persistent state.
