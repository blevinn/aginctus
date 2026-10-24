# Authenticated LiteLLM Responses smoke (#122)

This script is an **opt-in live provider request** from the trusted NixOS
pilot host, not a synthetic #124 Herdr acceptance run. It uses only a
restricted LiteLLM workload-facing key; it never sends the LiteLLM master
or upstream provider secret. It neither deploys nor repairs the gateway.

## Preconditions and security boundary

- The existing LiteLLM instance is Running and its application
  /health/liveliness check passes. PostgreSQL is reachable by its managed
  hostname, the NixOS firewall backend is active, and the management bridge
  keeps NAT and routing disabled. Follow docs/firewall-pilot-validation.md.
- An **authorized API-key model** is actually registered and exposed under the
  selected alias. #247 merged a guarded opt-in model registration utility;
  its previous validation was **dry-run only** with the deliberately
  unverified `openai/placeholder` source. **No live model or provider key was
  registered by that dry-run.** Confirm an actual approved model and key
  separately before the live smoke. Never activate unapproved ChatGPT OAuth.
- The operator has already created a **separate LiteLLM virtual key**, scoped
  only to the selected model alias, with a limited lifetime and revocation
  procedure. It is not the master key. Store it in a caller-owned 0600 regular
  file outside Git, /nix/store and public home directories. The script reads
  the already-persisted gateway master state **only** to compare and reject
  accidental admin-key use; the master is never transmitted.
- The trusted pilot host can address the **observed** private numeric LiteLLM
  IPv4 address on TCP 4000. Aginctus currently uses plain HTTP on the private
  management bridge, so the workload key crosses that local bridge without
  transport encryption. **Do not** use the tool from a public or untrusted
  client or expose the private port on the internet. Provider TLS and egress
  must be separately verified from actual gateway-side observations.
- The test makes one small billable Responses inference request. Run only
  with an approved upstream model and spending authorization.

## Pilot state is not a script precondition

Do not rely on historical instance IPs, registration attempts or model/key availability recorded in earlier PRs. Inspect the **current** gateway on the operator-approved host and follow [#257](https://github.com/blevinn/aginctus/issues/257) for egress readiness and [#122](https://github.com/blevinn/aginctus/issues/122) for permission to resume live/billable inference. Neither configured routes, issued scoped keys nor HTTP health checks prove a successful upstream request.

## Operator workflow

A dry-run requires no secret, network, or gateway readiness:

~~~sh
# Replace the illustrative IP with the OBSERVED private LiteLLM address.
nix develop -c python3 scripts/gateway/authenticated-inference-smoke.py \
  --endpoint http://10.42.40.15:4000 \
  --alias aginctus-verified-openai \
  --workload-key-file /absolute/operator/private/workload.key
~~~

After independently verifying all prerequisites above, append --run to the
same command. It runs, in order:

1. A GET on /health/liveliness.
2. Two GET /v1/models negatives, without a token and with a fresh invalid
   bearer token; both must return HTTP 401 or 403.
3. Authorized GET /v1/models with the restricted workload key; the selected
   alias must appear.
4. Exactly one POST /v1/responses using that key and alias with stream=true
   and a fresh challenge marker in the prompt. The script requires a terminal
   response.completed/status=completed and a nonempty output item that actually
   contains the challenge marker. HTTP 200 alone is rejected.

The script permits only private numeric IPv4 destination addresses at HTTP
port 4000, blocks HTTP redirects and proxy environment routing, rejects
loose/symlinked secret files and payloads over 8 MiB, and prints only a safe
status, event count and **nonsecret** correlation identifier. It never prints
provider output, error payloads, model text or credential bytes.

On an HTTP error from `/v1/responses`, the script reads at most 16 KiB of
response data and reports **only one fixed classification**:
`missing-session-id`, `unsupported-parameter`, `unknown-model`,
`provider-authentication`, or `unclassified`. The raw error body,
exception text and HTTP reason are never printed. Classification is
best-effort; an `unclassified` response is not proof that a particular
error did not occur. An observed `missing-session-id` does not establish
whether the client omitted the header or LiteLLM dropped it.

**OpenCode Go caveat:** This host-side nonce-echo smoke intentionally does not
fabricate an OpenCode conversation ID or pretend to be a coding agent. The
pinned real-OpenCode loopback test in `guest/opencode/test_real_runtime.py`
checks for an identifying `User-Agent`, the presence of a native OpenCode
session header, and stability across the requests in one run. That test
establishes the **client-to-mock** contract, not LiteLLM-to-Go forwarding.
The pinned LiteLLM v1.103.0 default does not blindly forward client `x-*`
headers. Its `model_group_settings.forward_client_headers_to_llm_api`
option is model-scoped but forwards a broad collection of client `x-*`
headers and does not by itself preserve `User-Agent`. Do not globally
enable forwarding to make the smoke pass; a separate restricted, tested
header-forwarding mechanism is necessary before repeating paid Go inference.

A passing transcript **does not independently prove upstream provenance**.
The operator must inspect trusted LiteLLM-side telemetry or provider-side
usage, correlate the generated identifier with the request, verify upstream
TLS/connectivity via gateway-only egress, and verify the workload/PostgreSQL
cannot reach provider endpoints. Do not claim all-up acceptance from a
fabricated SSE transcript or an instance status. Provide redacted evidence to
#123 (private endpoint, real alias and revocable workload key procedure) and
#124 (provider request correlation and negative checks) only after those
checks are complete. Do not paste provider keys or live transcripts into
GitHub comments or chat. Revoke the temporary virtual key when complete.

## Full offline validation

~~~sh
nix develop -c python3 -m unittest discover \
  -s scripts/gateway -p 'test_authenticated_inference_smoke.py' -v
nix develop -c python3 -m py_compile \
  scripts/gateway/authenticated-inference-smoke.py \
  scripts/gateway/test_authenticated_inference_smoke.py
just check-all
git diff --check origin/main...HEAD
git status --short
~~~

Mocked tests deliberately cover stream terminal status, challenge text,
invalid/missing key refusal, admin key equality, redirects, private endpoint
restrictions and protected-file safety. They do **not** validate live provider
authorization or spend. This script is separate from #124's full Herdr
OpenCode acceptance dispatcher.
