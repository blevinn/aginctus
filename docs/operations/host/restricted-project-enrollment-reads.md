# Restricted project enrollment reads from installed Aginctus CLI

**Status:** opt-in, read-only enrollment integration; not accepted-state or
logical workload production completion. Requires administrator-provisioned
restricted Incus TLS credentials, project enrollment manifest, and the
disposable NixOS/Incus negative tests in
[go-client-read-v02.md](../../testing/acceptance/go-client-read-v02.md).

The ordinary `aginctus` binary can now use an explicitly selected
private operator TLS configuration to list/show enrolled environments
from the actual restricted Incus API, rather than a test-injected reader.

The file MUST be an absolute, final-component non-symlink, regular file
owned by the invoking UID and readable by no group/other users (mode 0600).
Do not store the private key in the Nix store, a Home Manager text setting,
version control, logs or command arguments.

```json
{
  "apiVersion": "aginctus.dev/restricted-operator-read/v1alpha1",
  "endpoint": "https://incus.example.invalid:8443",
  "project": "aginctus",
  "serverCertificatePem": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n",
  "trustedServerSha256": "64-lowercase-hex-digits-from-independent-trusted-provisioning",
  "clientCertificatePem": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n",
  "clientPrivateKeyPem": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n"
}
```

All example certificate data and fingerprints are placeholders. Obtain the
actual server certificate fingerprint independently from a trusted operator
channel, and the project-restricted client key from its legitimate owner.
The Incus project must contain the administrator-provisioned versioned
`user.aginctus.environments` enrollment manifest. Missing manifest fails
closed; an empty list requires an authenticated explicit `items: []`.

```sh
chmod 600 /absolute/private/path/operator-read.json
AGINCTUS_RESTRICTED_READ_CONFIG=/absolute/private/path/operator-read.json \
  aginctus --incus-project aginctus environment list -o json

AGINCTUS_RESTRICTED_READ_CONFIG=/absolute/private/path/operator-read.json \
  aginctus --incus-project aginctus environment show <immutable-uuid> -o yaml
```

This env var selects only an explicit private configuration file. The CLI
proves server identity via pinned TLS and independently verifies the
restricted principal and exact Incus project before each enrollment read.
Specifying another project, a revoked credential, a changed server
certificate or missing manifest MUST yield an error.

**Security boundary:** The connection uses the Incus SDK, which has write
methods even though the CLI receives only `client.EnvironmentReader`.
Never pass that SDK handle to candidate parsers, untrusted RPC handlers,
agents or generic tools. No administrator Unix socket or fallback is used.

**Not yet operational:** configured state, accepted state, diffs and logical
workloads still need a production authenticated state-service transport
and authoritative candidate/observation readers. Those commands remain
unavailable; a missing reader must never produce empty success. This change
does not authorize acceptance, project mutation, reconciliation or adoption.

## Optional pinned project-state reads

The same owner-private file can opt into a separately pinned mTLS
project-state service by adding **all four** of these fields. Partial
configuration is rejected; the server certificate and fingerprint must be
obtained from independently trusted provisioning, not from a candidate or a
service response.

```json
{
  "stateServiceEndpoint": "https://state.example.invalid:8443",
  "stateServiceCertificatePem": "-----BEGIN CERTIFICATE-----\\n...\\n-----END CERTIFICATE-----\\n",
  "stateServiceSha256": "64-lowercase-hex-digits-from-independent-trusted-provisioning",
  "configuredCandidateFile": "/absolute/path/to/selected-nonsecret-candidate.json"
}
```

Add these properties to the same top-level JSON object as the Incus TLS
credentials; this snippet is not a standalone configuration document. It
still cannot grant project rights on its own. The ordinary CLI then uses:

- The **Incus project's independently verified restricted principal** to
  discover enrollment, recheck binding and detect revocation.
- The **separately pinned state-service HTTPS endpoint** and mTLS client
  certificate to read a versioned accepted revision and candidate.
- The **explicit selected nonsecret candidate file**, validated against
  the enrolled immutable UUID, to project configured state and field-only
  configured-versus-accepted drift.

Examples (available only once the independently authenticated service is
actually deployed, not through the same-UID fixture):

```sh
AGINCTUS_RESTRICTED_READ_CONFIG=/absolute/private/operator-read.json \\
  aginctus --incus-project aginctus --environment <immutable-uuid> state show -o json
AGINCTUS_RESTRICTED_READ_CONFIG=/absolute/private/operator-read.json \\
  aginctus --incus-project aginctus --environment <immutable-uuid> state diff -o json
```

The client verifies that a 200 response is well-formed, that its published
candidate matches the accepted revision digest, and that the Incus
server/project binding is still authorized before returning results.
A failed service connection or a denied/revoked principal **never becomes**
`hasAccepted: false`.

**Deployment remains blocked:** the state-service must authenticate the
actual TLS peer against the current Incus trust/authorization policy using
only non-administrator capabilities, restrict its project-owned storage,
survive volume/service restart, and pass the disposable VM credential
revocation and foreign-project negative tests. Merely filling these fields
does not establish those properties. Logical workload status/observations
remain independently gated and are not inferred from candidate instance
names or labels. No automatic state service is started by this CLI.
