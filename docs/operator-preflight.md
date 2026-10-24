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
