# Restricted Incus TLS read verification (Phase 1)

**Status: implementation candidate / hermetic contract only. NOT activated in the
standalone CLI.** Related: [#695](https://github.com/blevinn/aginctus/issues/695),
[#696](https://github.com/blevinn/aginctus/issues/696), [#704](https://github.com/blevinn/aginctus/issues/704),
and [ADR 0006](../adr/0006-environment-incus-authority.md).

`environment.RestrictedIncusProjectVerifier` implements the
`environment.BindingVerifier` contract using a **remote, pinned Incus TLS**
connection. It does not use a Unix/admin socket or an ambient default remote.

The caller must supply an `incusclient.InstanceServer` constructed using
**independently provisioned restricted client credentials**, authenticated
Incus TLS (no `InsecureSkipVerify`), and a separately trusted server
certificate fingerprint. The pin must come from the administrator's trusted
configuration, **never** an environment candidate, display name, label, or
project flag. The verifier alone cannot inspect the TLS construction options,
client private-key provenance or the server trust-store restrictions; its
positive results require those independent prerequisites and real-VM tests.

The verifier:

1. Rejects nil/canceled calls, default/invalid project selection, missing
   trusted pins, socket paths, and non-HTTPS connection metadata.
2. Confirms an exact project-scoped remote connection with a pinned server
   X.509 certificate (SHA-256 of leaf DER, lowercase hexadecimal).
3. Requires Incus to report the current client as authenticated, and obtains
   the exact requested project through an Incus API read.
4. Makes a **negative named GET** for the always-existing `default` project,
   requiring an explicit API 403 or 404. A 200 response, transport failure or
   unrelated error is a failure. This is a limited, safe rejection of broad
   credentials—not a proof against every alternate authorization backend.
5. Rechecks the connection metadata before releasing the binding, yielding
   `serverId = sha256:<lowercase-hex>` and the exact project name.

No Incus modifications, state-service calls, revision reads, enrollment
registration or lifecycle writes are performed.

## Required acceptance before production use

The server's effective `authorization.client.tls-restricted` policy must be
inspected on the disposable NixOS/Incus 7.5.1 guest, rather than assuming that
`GetProject(default)` denial alone implies the whole requested policy.
Run with a genuinely restricted certificate for one nondefault project:

- Verify the pinned server certificate and the exact authorized project.
- Verify `default`, an additional populated foreign project, and global
  host/network/pool API queries are inaccessible under that same identity.
- Repeat with unrestricted/admin credentials and require refusal.
- Revoke the certificate between verifications, including persistent SDK
  connections, and verify no accepted-state payload escapes.
- Exercise connection retargeting, project rename, policy changes, transient
  403/404/unavailable responses, and credential rotation.
- Independently inspect the project-owned service/container/volume and prove
  service processes never possess host-administrative Incus credentials.

**Outstanding:** The independently authenticated enrollment source and
project-state transport are not yet implemented by this verifier. The
`RestrictedPublishedSource` and ordinary CLI remain fail-closed. Do not
record the Phase 1 read acceptance gate as passing based on this PR alone.

## Explicit SDK TLS connector

`OpenExplicitRestrictedIncusProject` provides a narrower SDK constructor
requiring the caller to supply all credentials, exact HTTPS endpoint, project,
trusted server PEM and separately obtained fingerprint. It validates the
certificate hostname and validity interval and the client keypair **before**
attempting a network connection, then uses Incus SDK's verified TLS connection
and the restricted project verifier above. No ambient Incus remote, default
project, Unix admin socket or auto-generated credential is consulted. Connection
failure never falls back to an administrative transport.

The returned SDK client can itself make Incus mutations, so it belongs only in
the trusted composition process, not in a lower-privilege HTTP handler, a
candidate parser, or a generic project-state service method.

This does not implement the accepted-state service transport or grant access
to a project volume; it is only the Incus principal and enrollment read
connection. Real TLS denial and revocation evidence remains required.
