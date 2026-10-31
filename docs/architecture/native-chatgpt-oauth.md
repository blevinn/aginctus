# Native LiteLLM ChatGPT OAuth gateway decision (v0.2)

Status: accepted architecture, **integration not implemented end to end or deployed**. Tracked in [#187](https://github.com/blevinn/aginctus/issues/187), with credential ownership in [#171](https://github.com/blevinn/aginctus/issues/171), Responses fidelity in [#172](https://github.com/blevinn/aginctus/issues/172), and provider tracking in [#169](https://github.com/blevinn/aginctus/issues/169). The latest scope decision places this optional integration in [v0.2 #173](https://github.com/blevinn/aginctus/issues/173), independent of the approved non-OAuth v0.1 path. It supersedes unmerged #186 and the custom OpenAI inference daemon from #170. LiteLLM remains the sole workload-facing inference gateway; existing API-key providers and workload-ingress authentication remain required.

**External blocker:** the operator has confirmed that a documented permitted-use
basis for the native route has not been established. This is not an approval to
enroll an account, import credentials, or enable inference. The authorization
evidence required to resume activation is described below.

## Superseded standalone-daemon prototype

The earlier `cmd/aginctus-oauth-gateway`, `internal/oauthgateway`, and optional
`nix build .#oauth-gateway` guest were a **fail-closed prototype**, not an
active OAuth inference path. The standalone guest required a protected mounted
credential directory and exposed loopback liveness on port 4081; liveness
could return HTTP 200 while inference/import/readiness returned 503. No
gateway Compose path activates this daemon. Keep these artifacts distinct
from the accepted LiteLLM-native integration; don't deploy them as a shortcut
around enrollment, exclusive refresh ownership, and live Responses acceptance.
The earlier design discussion is preserved in Git history and issues #170/#171.

## Verified upstream capabilities and remaining uncertainties

Aginctus pins `ghcr.io/berriai/litellm:v1.103.0`. The upstream GitHub tag `v1.103.0` includes `litellm/llms/chatgpt/authenticator.py` and the ChatGPT provider package:
https://github.com/BerriAI/litellm/tree/v1.103.0/litellm/llms/chatgpt

LiteLLM's ChatGPT subscription provider documentation:
https://docs.litellm.ai/docs/providers/chatgpt/
documents `chatgpt/` models, `model_info.mode: responses`, native Responses routing and persisted authentication configured by `CHATGPT_TOKEN_DIR` and `CHATGPT_AUTH_FILE`. This describes the provider; **do not assume every current documentation detail is available in the pinned tag**. Check streaming events, tool-call events, statuses, supported model names, and credential-refresh behavior on the pinned image, then pin/update to a verified compatible release if necessary.

## Ownership and topology

```text
OpenCode / Hermes -> LiteLLM (private endpoint, v0.1 shared key)
                              |
                              +--> conventional API-key providers
                              |
                              +--> native chatgpt/ Responses provider
                                      |
                                      +-- managed credential volume
                                      |   (LiteLLM only)
                                      +--> ChatGPT subscription backend
```

The managed volume `aginctus-gateway-<id>-credentials` belongs to the **LiteLLM process acting as the credential manager**. Never attach it to PostgreSQL or workloads. A writable mount is needed because LiteLLM refreshes tokens. A successful Incus volume creation alone says nothing about secure mounts. Existing daemon images may remain available for development but must not be deployed for OAuth inference.

LiteLLM must be the sole credential and refresh-token owner. Use controlled native device-flow enrollment, not a separate host OAuth client, importer, or token daemon. Require explicit user authorization and account/workspace selection before enrollment; no authentication as a side effect of inference. An interrupted or ambiguous enrollment must fail closed without replacing a valid session. Never put OAuth tokens in generated Compose/Jsonnet, Nix store, process arguments, Incus instance configuration, or logs.

It is **not enough** to mount arbitrary host authentication files or copy tokens into the volume. Native enrollment and refresh need tested private storage, atomic replacement, and exclusive ownership. Do not reuse Codex CLI caches or the superseded SIWC credential representation: compatibility and authorization are not implied by either being an OAuth record.

## v0.2 implementation checkpoints

1. Add opt-in native ChatGPT Responses route to LiteLLM config; preserve current key providers and default behavior.
2. Securely attach only the managed volume to LiteLLM with writable ownership and restart persistence. Validate Incus project/pool/device placement; no volume in PostgreSQL or workloads.
3. Implement explicitly authorized native device-flow enrollment, single refresh owner, account binding, logout/revocation, and recovery rules.
4. Prove pinned-version Responses streaming, tool-call events and completion statuses with actual OpenCode traffic through LiteLLM.
5. Verify that the `aginctus-egress` bridge is present or reconcile it through the existing gateway provisioning path before attempting live deployment. The earlier #186 daemon deployment failed because the bridge was missing, not because provider credentials were invalid.


## Local model alias registration (inert, pre-activation)

Aginctus can now persist a workload-facing alias intent **without** activating a LiteLLM deployment, starting OAuth, selecting an upstream account, or asserting that a model name is available:

```sh
aginctus gateway provider chatgpt register --alias=personal
aginctus gateway model coding register --provider=personal --source=chatgpt/OPERATOR_VERIFIED_MODEL
aginctus gateway model list
aginctus gateway model coding show
```

Replace `OPERATOR_VERIFIED_MODEL` only after independently checking account entitlements and the pinned provider's supported model identifiers. `gateway model ... register --dry-run` still previews only the nonsecret LiteLLM Responses model fragment and does not persist state. Bare `chatgpt/MODEL` registration defaults to the `chatgpt` provider identity, which must first be registered.

The model registry is `${XDG_STATE_HOME:-$HOME/.local/state}/aginctus/gateway/models.json` (0700 parent, 0600 file), separate from `providers.json`. It validates aliases, requires a previously registered ChatGPT provider alias, serializes writes, rejects suspicious files and duplicate conflicting registrations, and provides read-only inspection. The records are nonsecret **intent**, not deployed routes; downstream #123 must receive only a separately authorized workload-facing alias and LiteLLM key. #124 must verify the alias against a *real terminal Responses event*, not registration success. This stage does not add a new public listener or modify gateway networks.

### Render the registered model intent for review (not activation)

Once a provider identity and an alias have been recorded, inspect the exact
nonsecret LiteLLM Responses model definition the alias would require:

```sh
aginctus gateway model coding render
```

The output is a single JSON model entry with `model_name`, a
`litellm_params.model` value of the registered `chatgpt/...` source, and
`model_info.mode: "responses"`. This resolves the **stored** alias, rather
than accepting a new source at render time, and never modifies a gateway,
token store, Incus resource, or local registry. The command prints a
separate stderr reminder that authentication and gateway activation remain
pending.

LiteLLM v1.103.0 currently uses one native credential directory and auth
record for all `chatgpt/` routes. Accordingly, route rendering fails closed
unless **exactly one** ChatGPT provider identity exists locally and the model
references that identity; multiple local provider aliases cannot safely imply
multiple upstream accounts. This is a limit of the current deployment
architecture, not a restriction on recording nonsecret intentions.

**Do not submit the rendered JSON to LiteLLM's model registration API yet.**
This command does not check whether the upstream model is entitled, whether
the chosen account is authorized to perform autonomous inference, or whether
native token refresh/streaming is safe. The explicit activation and live
acceptance gates below still apply.

### Read-only credential attachment audit

Once the owned gateway Compose project and credential volume have been created,
run the following on the trusted NixOS host using its local Incus socket:

```sh
aginctus gateway credential-storage audit
```

This **does not create a volume or attach a device**. It checks the actual
Incus project ownership markers and isolated storage feature, the owned
`litellm-1` instance and exact `/var/lib/aginctus/chatgpt` disk mapping,
the custom volume's ownership/type and its Incus-reported `UsedBy` consumer
list. A pass requires **exactly one** consumer: `litellm-1` in the
Aginctus-managed Compose project, not a profile, PostgreSQL, another instance
or another project. Missing objects and ambiguous metadata fail closed.

The audit can inspect a **stopped** guest. It neither opens the volume's files
nor logs credentials; it cannot prove `auth.json` permissions, LiteLLM
process/workers, crash-safe refresh, user consent, backend readiness or an
authenticated inference response. Thus a pass is **not** permission to enable
`CHATGPT_TOKEN_DIR` or a `chatgpt/` model. Guest file preparation remains
required under #171/#187. PR #217 was closed without merge; it is historical
reference, not an available implementation. Database readiness and effective
gateway firewall/egress checks remain independent prerequisites.

### Offline Responses event contract verifier (no gateway activation)

`aginctus gateway responses verify` accepts a **Responses SSE transcript**
on standard input and checks event/payload consistency, JSON framing,
bounded input, an explicit `response.completed` event with
`response.status=completed`, and that no failure, incomplete status,
duplicate completion or subsequent events occurred. A trailing `[DONE]`
marker is allowed **only after** the terminal completion. To require a
completed function-call output item with nonempty `name`, `call_id`, and
valid JSON-object `arguments`, use `--require-function-call`. Examples with
*synthetic nonsecret* events:

```sh
cat <<'SSE' | nix develop -c go run ./cmd/aginctus gateway responses verify
event: response.completed
data: {"type":"response.completed","response":{"status":"completed"}}

SSE

```

The verifier reads from stdin once and prints only completion/event counts,
or a generic rejection; it never prints payload text, prompts or error
details from the transcript. It deliberately permits future `response.*`
events not relevant to terminal/call checks and does not interpret model
outputs. It enforces a bounded stream (8 MiB) and line (1 MiB). This
allows independent offline regression tests and operator-controlled
verification of a previously observed streaming event sequence **without**
introducing an HTTP proxy, credential handler, network access or model
activation.

**This is not an actual inference acceptance test.** A synthetic stream may
pass without any model request. The future controlled live acceptance
(#172/#187/#123/#124) must securely establish the source was the pinned
LiteLLM gateway, verify authenticated workload ingress, upstream model
authorization, exact request routing, tool execution and terminal events.
Do not capture or commit live raw SSE transcripts containing user prompts,
provider responses or tool arguments; if used live, pipe directly to the
validator without logging, `tee`, or exposing secrets. Keep all native
OAuth activation blockers unchanged.

### Activation blockers verified against pinned LiteLLM v1.103.0

The upstream `litellm/llms/chatgpt/authenticator.py` implementation directly rewrites `auth.json` via `open(..., "w")`, does not implement interprocess locking or atomic token replacement, and can trigger device-code login as a side effect of model resolution. It also embeds some upstream error details in exception messages. Consequently **do not enable `CHATGPT_TOKEN_DIR`, configure an active `chatgpt/` route, or run live OAuth yet**, even after #217 prepares a private file. A later, explicitly reviewed activation must enforce one refresh owner, safe persistence, token-safe errors, and a controlled authorization boundary, ideally through a confirmed compatible upstream release rather than a duplicate OAuth implementation.

OpenAI's individual Terms of Use (https://openai.com/policies/terms-of-use/) restrict automated/programmatic output extraction. The applicability to routing a personal ChatGPT subscription through LiteLLM for workload inference is not established here; obtain a documented authorization basis before enabling this route. Use an approved API-key-backed provider as a separate supported route when appropriate.

### Verified native provider contract

The source references below describe the pinned provider's behavior, not a
successful Aginctus integration or a grant of permission:

- [Native constants and headers](https://github.com/BerriAI/litellm/blob/v1.103.0/litellm/llms/chatgpt/common_utils.py)
  identify a Codex device flow at `auth.openai.com`, a Codex client identity,
  and the inference base `https://chatgpt.com/backend-api/codex`. This is
  neither OpenAI Platform API-key authentication nor the historical SIWC
  dynamic-registration flow. Do not substitute the historical SIWC scopes,
  client registration, or `api.openai.com/v1` inference contract.
- [Native authentication](https://github.com/BerriAI/litellm/blob/v1.103.0/litellm/llms/chatgpt/authenticator.py)
  requests a device code, polls for an authorization code and PKCE material,
  then exchanges it using the provider's device callback URI. There is no
  Aginctus loopback browser callback in this path. JWT claims are decoded
  without signature, issuer, or audience validation; claim extraction is not
  validated account identity. The persisted record contains `access_token`,
  `refresh_token`, `id_token`, `expires_at`, and `account_id`.
- [Native Responses transformation](https://github.com/BerriAI/litellm/blob/v1.103.0/litellm/llms/chatgpt/responses/transformation.py)
  obtains authentication from that native record, adds bearer authentication
  and `ChatGPT-Account-Id`, uses `/responses`, and forces `store:false` and
  `stream:true` upstream. It filters unsupported request fields. Verify actual
  streaming, cancellation, retries, function-call continuation and terminal
  completion on the pinned runtime, rather than treating this transformation
  as end-to-end acceptance.
- The auth file is process-global, not per virtual key or model. Local aliases
  do not bind separate ChatGPT accounts. All authorized callers of active
  `chatgpt/` routes would use the same enrolled account/workspace. Workload
  ingress keys must never become provider tokens or permit arbitrary account
  selection. Review client-header forwarding as part of that boundary.
- Refresh uses the stored refresh token and preserves it if the response omits
  a replacement. Native writes are in-place and write failures are logged
  rather than propagated. A refresh failure can fall through to interactive
  enrollment; malformed token responses can appear in exception messages.
  Private volume attachment and a single guest do not fix concurrent workers,
  threads, interrupted writes, or lost refresh-token rotation.
- The provider does not implement logout or remote revocation. Local removal
  must stop use and prevent an in-flight refresh from recreating credentials;
  it must be distinguished from upstream revocation. Do not invent a revoke
  endpoint or promise remote invalidation from local deletion.

The [current LiteLLM provider documentation](https://docs.litellm.ai/docs/providers/chatgpt/)
also describes the global account scope, pre-enrollment requirement, refresh
races and in-place writes. Its newer-release behavior is not evidence that
the pinned runtime has been fixed. OpenAI's
[authentication documentation](https://developers.openai.com/codex/auth/)
describes device-login enablement in personal security settings or workspace
permissions; enablement alone does not resolve authorization for this relay.

### Remaining lifecycle and acceptance checklist

| Area | Implemented here | Required before end-to-end acceptance |
| --- | --- | --- |
| Provider/model selection | Private nonsecret registrations and read-only Responses route rendering | Approved account/workspace and entitled model binding; optional native deployment; no fallback to API credentials or another account |
| Authorization and consent | Activation prohibition and documented external blocker | Recorded permitted-use basis and explicit consent for the selected workloads, including autonomous/background access |
| Enrollment | No active Aginctus login command | Controlled native device flow; bounded network/polling deadlines; cancellation; validation of provider response/PKCE/identity invariants; failed reauthentication preserves the valid record |
| Storage | Owned project-local volume and exclusive attachment metadata audit | Verified runtime UID/GID; private 0700 directory and 0600 regular non-symlink file; safe existing-file checks; atomic durable updates and recovery |
| Refresh | Upstream native implementation only, not activated | Sole owner across enrollment and runtime; serialized reread/refresh/write across workers; validated replacements; safe rotation; revoked-token diagnostics without automatic login or secret leakage |
| Restart and disconnect | No OAuth lifecycle acceptance | Recovery of persisted rotated state; race-safe local logout/removal; documented upstream revocation procedure; predictable reauthentication |
| Model execution | Offline terminal Responses/function-call verifier | Real authorized OpenCode → private LiteLLM → native provider inference; streaming/tool continuation and actual tool side effect; terminal completion; cancellation/retries/errors; workload secret isolation |
| Regression coverage | Existing registration, volume and synthetic Responses tests | Deterministic native lifecycle tests covering success, rejection, malformed responses, exchange/refresh failures, concurrency, rotation, restart, logout and API-key compatibility |

Do not activate merely to discover whether these requirements hold. Prefer a
verified compatible native provider implementation; any local runtime fix must
stay inside the LiteLLM-native path and ship with its deployment and tests,
not as an orphaned module or a new OAuth proxy.

### External authorization evidence needed to resume activation

Record the decision in #187 or link a reviewed, nonsecret authorization record
from it. It must identify:

1. The applicable provider permission/terms basis for third-party native
   subscription inference through LiteLLM using this Codex device flow and
   backend, rather than assuming SIWC permission applies to it.
2. The consenting account owner and selected workspace, with any required
   workspace-admin approval and device-login prerequisites. Do not publish
   tokens, personal account details, or device codes in the issue.
3. The permitted workloads/callers and model scope, including whether
   autonomous/background use is authorized. Multiple workload keys still
   consume one ChatGPT account; consent must cover that scope.
4. Approval for a controlled live enrollment and inference acceptance run in
   the managed gateway topology, after credential-safety fixes are verified.

An operator acknowledgement cannot establish provider permission by itself.
This document does not conclude that native subscription inference is
universally prohibited or permitted. Until the basis is established, keep
native authentication disabled and the acceptance issues open. There is no
supported Aginctus OAuth login, status, reauthentication, or logout sequence to
advertise yet. The registration and audit commands above inspect intent and
metadata only; they cannot exercise the requested user journey.

## Scope guardrails

Do not add another OpenAI Responses proxy or multiple LiteLLM instances by default. Use only supported ChatGPT models and account permissions. Explicitly error on unsupported features. Coordinate per-workload LiteLLM identities with v0.2 #177. No live OAuth credential deployment occurs as a side effect of merging this document.
