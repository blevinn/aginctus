# Trusted live-acceptance receipts (proposed v0.2 contract)

Status: **proposal for review; not implemented**. Tracks
[#514](https://github.com/blevinn/aginctus/issues/514). Builds on the
read-only coverage view of [#491](https://github.com/blevinn/aginctus/issues/491),
without changing the live authorization gates in
[#122](https://github.com/blevinn/aginctus/issues/122),
[#257](https://github.com/blevinn/aginctus/issues/257), or the
[#118](https://github.com/blevinn/aginctus/issues/118) release checklist.

## Decision and hard boundary

For v0.2, use **operator-pinned Ed25519 issuer identities** to authenticate
a versioned, bounded *receipt of independent observations*. The receipt is
**not** authorization to run the underlying acceptance test, and a valid
signature alone does **not** prove that inference occurred. The trusted issuer
must enforce the evidence policy *before signing*. The separately installed
verifier checks issuer trust and receipt consistency, but does not repeat
billable/live operations. A green receipt is only evidence under the
configured issuer policy, not automatic release sign-off.

The issuer must run outside the agent-controlled guest, checkout and
Herdr-managed workspace, on a trusted operator-controlled service or
dedicated trusted worker authorized to access only the required observations.
An OpenCode process, workload-reachable MCP server, LiteLLM client, or
mutable testenv guest **cannot** issue a trusted receipt. Agent output,
gateway-health HTTP 200, mock markers and a successful private-bridge
preflight may be supporting observations, never sufficient authority.

The existing testenv worker's **32-byte policy HMAC key** is only for Incus
resource/run ownership. It is **not** the live-acceptance signing key and
must not be repurposed or shared with the acceptance issuer. The two trust
roots, purpose labels, key lifecycles and reports stay separate.

## Threat model

Assume the coding agent can fully control its workspace, prompts, tool
output, local files, request body and guest-provided correlation markers.
Assume a repository checkout and user-supplied JSON could also be attacker-
controlled. The agent must not turn any of those into a verified provider,
egress or cleanup claim by editing a result file or supplying a public key.

The issuer may be privileged and is therefore part of the trusted computing
base. The pilot Incus daemon and managed gateway have their own privileged
operators. A compromised issuer or compromised trusted observations can lie;
Ed25519 does **not** prevent a legitimate but dishonest signer. Independent
source checks, separation of authority, review/audit, short validity windows,
and key revocation are mandatory controls against that risk.

## Protocol: version 1

### Signed envelope

Use one small UTF-8 JSON object with *only* these fields:

```json
{
  "schema_version": 1,
  "kind": "aginctus.live_acceptance.receipt",
  "key_id": "live-issuer-2026-01",
  "payload_b64url": "<base64url of signed UTF-8 JSON payload>",
  "signature_b64url": "<base64url of Ed25519 signature>"
}
```

The displayed angle-bracket strings are documentation placeholders, not
working receipts. Signature input is the exact byte sequence
`ASCII("AGINCTUS_LIVE_ACCEPTANCE_RECEIPT_V1\n") || decoded_payload_bytes`;
no JSON reserialization is performed for signature verification. Reject
noncanonical base64url encodings, padding, duplicate JSON keys, unknown
fields, excessive nesting, nonfinite JSON numbers, invalid UTF-8,
unrecognized algorithms/versions or payloads exceeding **32 KiB**.
The outer envelope is bounded at **48 KiB**. Never attempt network key
discovery, issuer registration or trust-on-first-use from a receipt.

Do not allow algorithm negotiation: v1 is **Ed25519 only**. The
`key_id` selects an already pinned public key with policy scope. The
signed payload must repeat its own `kind`, `schema_version`, `key_id`
and `issuer_id`; reject envelope/payload mismatches. Domain separation
prevents a valid signature from another Aginctus protocol being reused.

### Signed payload inventory

Define strict typed fields for:

- `run_id`: host-generated, unique, unguessable identity matching the
  operator-authorized acceptance harness; not sufficient by itself.
- `issued_utc`, `started_utc`, `finished_utc`, `expires_utc`:
  offset-aware UTC timestamps, logically ordered and bounded by a
  policy-defined lifetime (maximum **24 hours** for v1).
- `issuer_id`, `key_id`, `policy_sha256`, and a fixed
  `approval_reference` to the separately authorized operator action.
  An approval reference is audit metadata, **not** a transferable permit.
- `provenance`: full git revision, dirty state, pinned Nix flake-lock
  SHA-256, deployed runtime/image identifiers, gateway instance identity,
  provider/model alias, and sanitized native OpenCode session correlation.
  Absent mandatory provenance blocks a passing receipt; never invent it.
- `observations`: the four fixed categories below, each with
  `status` (`pass`, `fail`, or `unverified`), independent source
  type, bound run ID, observation UTC time, and a fixed-length SHA-256
  commitment to a **separately access-controlled redacted evidence record**.
  Allowlists restrict any public evidence reference to opaque identifiers,
  not paths, URLs, payload contents or secret-bearing headers.
- `overall_status`: `pass`, `fail`, or `incomplete`, derived from
  verified observation statuses, never accepted as an independent assertion.

A `pass` receipt requires **every** mandatory observation to pass, exact
correlation and provenance binding, trusted signer policy and currently
acceptable signing key. `fail` and `incomplete` receipts remain valuable
audit records; signing those does not turn failed/unknown observations into
success. A receipt is per-run, not a wildcard authorization for a host/model.

### Four independently observed categories

| Category | Trusted observation required before PASS |
| --- | --- |
| Agent/tool activity | Trusted Herdr/control-plane record binds the launched native OpenCode session to the disposable run; independent fixture diff and trusted runner-confirmed `go test ./...` outcome. Agent narrative alone is insufficient. |
| Upstream inference | Gateway-side authenticated record binds the native session/request and scoped workload key to the expected model alias and completed **Responses** stream with actual upstream success. A request nonce or guest-provided `upstream-inference-confirmed` marker alone is insufficient. |
| Credential and egress boundary | Separately observed limited key scope, credential isolation, gateway-only positive route and denied direct-to-provider/forbidden destinations. Route or TLS connectivity alone is insufficient. |
| Owned cleanup | Independent checks of run-owned Herdr agent/workspace and any created instance, storage and ephemeral evidence resources. One successful deletion or an exit trap is insufficient; an uninspected resource is `unverified`. |

A single checker may coordinate evidence but must not take all four assertions
from the same untrusted guest source. The producer must record evidence
origin and fail closed on cross-run, cross-session, wrong-model, malformed
Responses or conflicting cleanup observations. Private raw evidence is
never put into the signed payload or safe reporting JSON.

## Issuer and verifier policy

- **Signing key:** provisioned *out of band*, private to the issuer, not
  in a Nix store derivation, guest, workspace, source checkout, workflow
  artifact or MCP response. Install via systemd credentials or equivalent
  operator-owned secret distribution. Use a separate signing process/service
  with a fixed evidence allowlist; do not expose `sign(arbitrary_bytes)`
  to an agent or general-purpose CLI.
- **Public trust:** verifier consumes operator-installed, pinned public
  keys and policy, with issuer identity, allowed evidence class, validity,
  and revocation status. This immutable/owned policy must not be writable
  by the agent, a PR checkout or the process that supplies the receipt.
  Do not discover trust anchors from the receipt.
- **Rotation/revocation:** distinct key IDs; old keys can remain valid for
  historical audits only as policy permits. Revoked or unknown keys,
  expired receipts, unknown policies and ambiguous timestamps fail closed.
  A `pass` signed under a revoked or untrusted key becomes
  `unverified` for acceptance purposes.
- **Replay:** verifier checks the intended run ID, expected revision,
  deployment and policy context supplied by a *trusted operator*, not by
  fields in the same untrusted receipt. A stale but correctly signed receipt
  must not sign off a new run.
- **Verifier:** read-only and offline, bounded CPU/input/file access and
  output allowlist. Report **signature_valid**, **issuer_trusted** and
  **evidence_policy_satisfied** as separate booleans/statuses; never
  collapse these into one cryptographic PASS. Invalid inputs cannot leak
  exceptions, evidence paths, credentials or gateway payloads.
- **Retention:** save signed receipts and redacted observation commitments
  in an operator-owned private evidence store with explicit TTL and
  documented recovery/revocation process. Do not assume a signature
  proves the referenced records are still retained or independently
  inspectable.

## Interaction with existing reporting and execution

The existing `just check ci`, `just check report-json`, VM evidence
viewer, stage-2 pilot preflight reporters, and `coverage` command **must
not execute live acceptance or import arbitrary signed-looking JSON**.
The current coverage report's four live tiers remain `not_run` and
`complete_acceptance=false` until a separate opt-in validator implements
this contract and operator trust policy has passed its review.

The live execution command remains the explicit
`AGINCTUS_ACCEPTANCE_LIVE=I_UNDERSTAND` + reviewed nine-hook dispatcher,
with all existing preconditions; no verification command, signing step, or
CI check may set that flag. Issuing a receipt is not authorization to run
live work; a verified receipt does not substitute for #118 operator release
sign-off. A v0.1 pilot is not blocked on this proposed v0.2 receipt design:
its currently required *human-reviewed* evidence remains tracked under
#122/#257/#118.

## Implementation sequence and independent tests

1. **Offline schema/verifier** (no issuer secrets): strict bounded parser,
   Ed25519 domain-separated verification and pinned trust policy supplied
   from an independent operator-owned source. Fixtures must cover one
   synthetic valid receipt; altered byte, forged issuer, wrong run,
   wrong model/revision, revoked/expired/unknown key, duplicate keys,
   oversized/malformed JSON, missing observation, partial cleanup and
   synthetic nonce-only upstream claim.
2. **Trusted observation collector and issuer:** fixed independent source
   adapters, explicit operator reference and narrowly authorized signer,
   with a bounded private evidence store. Unit tests use fake gateway,
   Herdr and isolation sources; no provider or Incus contact by default.
3. **Optional coverage integration:** operator-selected receipt ID only;
   read-only verifier output (not receipt-provided booleans) maps to live
   rows. Preserve separate `failed` / `unverified` / `not_run`
   semantics and keep `complete_acceptance` false without all required
   proof and operator policy match.
4. **Disposable pilot acceptance:** separately authorized read-only audit
   of receipt issuance, key rotation and revocation, wrong-run replay,
   gateway correlation and scoped cleanup on a **disposable** environment.
   Billable inference remains separately gated and absent from CI.

### Deferred decisions for operator review

The operator must approve: actual issuer service placement/identity,
permitted trust-policy deployment and rotation mechanism, whether a receipt
is retained with or separately from the authoritative run evidence, and
retention TTL. These choices affect authority and should not be guessed by
a verifier implementation. The proposed cryptographic envelope deliberately
does **not** require those deployment decisions to be encoded in the
reporting/CI subsystem.
