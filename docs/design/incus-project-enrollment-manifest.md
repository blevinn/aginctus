# Incus project enrollment manifest (Phase 1)

**Status:** proposed read-only enrollment discovery; requires disposable restricted
Incus authority evidence before production activation. This is not an
accepted-state service, nor a substitute for authenticated state transport.

An independently authorized **project administrator** writes one versioned,
bounded record into the Incus project's own `user.aginctus.environments`
configuration key. A project-restricted TLS client reads it via the Incus
API and a separately pinned server certificate. No candidate file, instance
label, ambient remote, user home directory, or accepted-record filename
determines the enrollment inventory.

Example, where `SERVER_ID` is the independently trusted
`sha256:<lowercase SHA-256 fingerprint of the DER Incus server certificate>`:

```json
{
  "apiVersion": "aginctus.io/environment-enrollment/v1alpha1",
  "items": [
    {
      "id": "11111111-1111-4111-8111-111111111111",
      "name": "dev",
      "binding": {
        "serverId": "sha256:SERVER_CERT_FINGERPRINT",
        "project": "team"
      }
    }
  ]
}
```

The example placeholder is not a valid fingerprint and must be replaced with
a verified value from an independently trusted administrative channel.

The manifest is explicit even for an empty inventory (`items: []`).
**Missing manifest is an error**, not proof of no environments. UUIDs are
immutable identities; names are display labels and may duplicate. The client
uses only the UUID for an exact lookup; name resolution requires unique
matching and a subsequent verified read.

`IncusProjectEnrollmentSource` enforces strict JSON with no duplicate or
unknown fields, a 64 KiB document bound, canonical UUID and name validation,
exact server/project binding, and a repeat project ETag/config inspection.
`Catalog` additionally rejects duplicate UUIDs, foreign records and
non-unique identity. The manifest does not contain accepted state or secrets.

## Operator acceptance and unresolved authority

An unrestricted administrative Incus client **must not** be accepted as an
ordinary operator principal. In a dedicated disposable NixOS/Incus VM:

- Provision the manifest with independent administrator credentials, using a
  project other than `default`.
- Confirm a genuinely restricted TLS certificate can read that project and
  the expected manifest, but cannot read populated foreign/default projects,
  mutate project configuration (including this `user.*` key), or modify
  global trust/network/pool settings.
- Attempt forged project names, duplicate/foreign environment IDs, stale
  server pins, revoked TLS certificates, manifest replacement races and
  post-restart recovery.
- If a restricted certificate can alter this manifest, **do not deploy this
  enrollment source** until an immutable administrator-owned alternative
  is selected. Reading a marker cannot itself establish authorization.

This source does not provide the protected project-owned service's accepted
revision records. A separate authenticated `RestrictedProjectReadSession`
and ordinary CLI composition are still required under #695/#696/#704.
