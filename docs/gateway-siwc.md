# Sign in with ChatGPT plan usage (SIWC)

Aginctus provides an **opt-in SIWC contract integration**, distinct from
LiteLLM's native `chatgpt/` provider. It uses Aginctus's issued dynamic client
registration and public `https://api.openai.com/v1/responses`, not Codex's
fixed client identity, device endpoints, credential cache, or ChatGPT's
`backend-api`. LiteLLM remains the only workload-facing gateway. The SIWC
transport and credential owner run inside its process, with no extra daemon,
listener, proxy, or workload provider token.

**Validation:** deterministic enrollment/lifecycle tests and a real pinned
LiteLLM proxy with a synthetic signed OAuth provider exercise authenticated
Responses, tool-call continuation, refresh and isolation. An optional real
OpenCode process exercises the generated client profile against that synthetic
gateway. These are not live OpenAI authorization, deployed OCI/Incus acceptance,
or model-driven filesystem execution. Those remain release gates under
[#187](https://github.com/blevinn/aginctus/issues/187),
[#171](https://github.com/blevinn/aginctus/issues/171), and
[#172](https://github.com/blevinn/aginctus/issues/172).

## Prerequisites and permitted scope

- Review the [SIWC terms](https://openai.com/policies/sign-in-with-chatgpt-terms/)
  and [plan-usage documentation](https://developers.openai.com/siwc/token-sharing-open-source).
  Requests must serve the authenticated user's **connected Aginctus application**,
  from a runtime only that user controls. They must not provide general-purpose
  API access to unrelated tools, other people's requests, pooled accounts, or
  resold plan usage. Background use needs express consent.
- Choose the account/workspace in OpenAI's browser consent screen. A registration
  is keyed by a local label and validated subject plus issued client ID; email
  is not an account/workspace key. Workspace policy, model eligibility and plan
  limits still apply. No automatic alternative billing or account fallback.
- Linux is required for protected filesystem state and `flock`. Enrollment and
  administration are Go code built into `aginctus`, using Cobra commands and
  flags (`aginctus gateway siwc --help`). These workflows require no Python
  interpreter or Python packages. Python, PyJWT and cryptography are confined to
  the in-process LiteLLM refresh/transport extension and test environment. The
  LiteLLM runtime must provide those dependencies and the verified LiteLLM
  `1.103.0` extension APIs. The extension rejects other versions until verified.
- For gateway use, provision and audit the existing owned, exclusive credential
  volume and gateway-only egress through the
  [operator path](gateway-operator-path.md). This feature adds no network grants.
  Management remains unrouted/NAT-disabled; PostgreSQL/workloads must not receive
  the volume. Inspect runtime UID/GID and use that identity for all guest state
  operations. Do not assume a UID from an image name.
- Use **one LiteLLM worker**, not multiple workers/replicas sharing a session.
  Its lifetime owner lease rejects a second worker and blocks local mutating
  commands. Refreshes within the owner are additionally serialized across
  threads/processes. Stop the owner before re-enrollment, handoff or logout.

## Enroll on the trusted browser host

Use an absolute private directory outside the Nix store, source tree, shared
workspace and backups. Its ancestors must be trusted operator-controlled paths.

```sh
STATE="$HOME/.local/state/aginctus/siwc"
aginctus gateway siwc --state-dir "$STATE" init
aginctus gateway siwc --state-dir "$STATE" login --account personal
aginctus gateway siwc --state-dir "$STATE" status
aginctus gateway siwc --state-dir "$STATE" models --account personal
```

`login` starts `127.0.0.1` on an available port at `/auth/callback`, then opens
the system browser and displays **Continue with ChatGPT**. Open the printed
link on the same host if no graphical opener is available. New registration
uses `dynamic_agent_client` with `agent_name_hint=Aginctus`; returning sign-in
reuses the saved issued client ID and stable host ID. No client secret is used.
Authorization URLs deliberately omit ID-token hints so no retained credential
is printed or placed in browser process arguments.

Each attempt has new cryptographic state, nonce and S256 PKCE material. The
listener validates the exact callback path, Host, state and singleton response
parameters. ID and access tokens are signature-verified against issuer-discovered
JWKS, with issuer/audience/time/identity/client binding checks. A failed returning
sign-in preserves the valid session. An issued new registration is retained if
code exchange fails, without claiming a validated identity.

If identity succeeds without `chatgpt.tokens.use.direct`, status reports plan
usage disabled and inference is rejected. To explicitly request consent again:

```sh
aginctus gateway siwc --state-dir "$STATE" login --account personal --enable-plan
```

This uses the documented `prompt=consent`, not undeployed `force_reconsent`.
The callback waits at most five minutes; provider calls have 15-second network
timeouts. Ctrl-C closes the listener and releases locks. No login occurs during
model resolution or an inference request.

## Bind a model and authorized Aginctus workload

Choose an exact slug returned by `models`, then bind a dedicated, existing
LiteLLM workload key, scoped to this alias with the existing
[key lifecycle](gateway-workload-key-pilot.md). The file contains the workload
ingress key, **never** an OpenAI token. Both consent flags are intentional
statements of the connected-application and background-use scope described above.

```sh
aginctus gateway siwc --state-dir "$STATE" bind \
  --account personal --route siwc-coding --model ACCOUNT_CATALOG_SLUG \
  --workload-key-file /private/aginctus-workload-key \
  --consent-connected-app --consent-background
aginctus gateway siwc --state-dir "$STATE" render > /private/siwc-config.json
aginctus gateway siwc bundle /private/new-siwc-bundle
```

Replace `ACCOUNT_CATALOG_SLUG` with the selected account's catalog slug. No model
name is assumed. Aliases start with `siwc-` to select the packaged OpenCode
request profile. The private binding records the issued client ID, validated
subject, model and SHA-256 workload-key identity. It cannot be silently rebound;
use `unbind --route siwc-coding`, then bind again after reviewing consent.

`render` emits only public LiteLLM configuration: Responses model aliases,
placeholder credentials, the SIWC callback, and disabled retries/fallbacks.
`bundle` writes the exact embedded `siwc.py` (runtime refresh/state support only)
and `litellm_siwc.py` sources into a **new** directory. Neither command deploys
or enrolls anything. Preserve
existing ordinary provider configuration when incorporating this public config.

The runtime checks LiteLLM's authenticated key identity before accepting a
bound alias. It does not trust caller account headers, metadata, destination,
credentials, provider overrides, or fallback settings. A master/admin credential
does not satisfy a workload-key binding. Rotating a workload key requires an
explicit new binding while the owner is stopped.

## Exclusive transfer into the managed gateway

Follow OpenAI's [self-hosted VM procedure](https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms).
Use the existing ownership/attachment audit before touching the volume:

```sh
aginctus gateway credential-storage audit
```

Use the project's approved Incus administration procedure to deliver the two
**public** bundle files and rendered config into an owned directory such as
`/app/aginctus-siwc` in the verified `litellm-1` instance. Also deliver the reviewed
Go `aginctus` executable for the guest's architecture through that approved
administration path; it is required for local state administration. The executable
and Python files must not be writable by workload users. Create a mode-0700 SIWC subdirectory inside
the audited private mount, owned by the actual LiteLLM worker UID/GID, for example
`/var/lib/aginctus/chatgpt/siwc`. Do not use an arbitrary host bind mount.

Before enabling the callback, run inside that guest **as the LiteLLM identity**:

```sh
aginctus gateway siwc \
  --state-dir /var/lib/aginctus/chatgpt/siwc init
```

Keep the printed, nonsecret `urn:uuid:...` target host ID. On the enrollment host:

```sh
aginctus gateway siwc --state-dir "$STATE" export \
  --account personal --target-host-id TARGET_HOST_ID \
  --capsule "$STATE/handoff.json"
```

The source durably removes its tokens **before** publishing the private handoff
capsule. Source status now reports signed out. Transfer the capsule over the
existing authenticated local Incus file API or approved SSH channel, preserving
0600 mode and assigning the actual target runtime owner. Do not print it, pipe
it through logs, put it in environment/configuration, or retain it as a backup.
Use a new destination filename in the protected guest directory. Then, inside
the guest as the runtime identity:

```sh
aginctus gateway siwc \
  --state-dir /var/lib/aginctus/chatgpt/siwc receive \
  --capsule /var/lib/aginctus/chatgpt/siwc/handoff.json
```

Receive verifies the destination host ID, signed credentials and route identity;
it refuses replay, existing account labels or route collisions. Transfer promptly
while the access/ID tokens remain valid. Remove **both** capsule files after
confirmed receive. If transfer is interrupted, do not activate a second copy:
inspect source/target status and reauthorize if necessary. Manual copies and
restored backups can defeat exclusivity; trusted administrators remain in scope.

## Activate and use the gateway route

Activation is a separately reviewed gateway configuration change. Do not run
unreviewed whole-gateway convergence over a held pilot deployment. Once public
bundle/config delivery and exclusive receive are verified, configure the owned
LiteLLM instance with these **nonsecret** runtime settings and restart it:

```text
PYTHONPATH=/app/aginctus-siwc
CONFIG_FILE_PATH=/app/aginctus-siwc/config.json
AGINCTUS_SIWC_STATE_DIR=/var/lib/aginctus/chatgpt/siwc
```

Use one worker and retain LiteLLM ingress authentication, database/key policy,
existing provider routes and gateway-only egress. Do not set `OPENAI_API_KEY` to
an OAuth token or enable the native `chatgpt/` route. If other callbacks such as
selective Go forwarding are active, merge their public callback entries and
module paths deliberately. Do not configure fallback targets for SIWC aliases.

Only select `siwc-coding` through the existing OpenCode workload gateway
configuration with `protocol: responses` and its dedicated LiteLLM key. Workload
configuration contains the private LiteLLM endpoint and alias, not SIWC tokens.
Quit and restart OpenCode after updating the launcher/configuration so it loads
the packaged SIWC profile.

The profile omits OpenCode's generated token limit and sampling controls, and
sets `store:false`. The gateway translates system messages to developer messages.
It accepts only array input, `stream:true`, `store:false`, local function/custom
tools (including namespaces), and a constrained Responses field set. It preserves
tool-call IDs, arguments and continuation items. Hosted tools and unsupported
fields are rejected instead of silently changing their semantics.

Access-token refresh is transparent at the final HTTP send, not injected into
LiteLLM model configuration or logging kwargs. Authorization is attached only to
the fixed public Responses URL. The stream verifier requires valid terminal
framing and withholds successful completion until the upstream stream ends;
interrupted/malformed streams fail instead of claiming completion. No automatic
retry or provider/account/billing fallback follows an inference failure.

## Status, recovery and logout

Run `status` against the authoritative guest store for local expiry/permission
state. It does not probe provider validity or refresh. An expired but renewable
session refreshes on the next request. Do not inspect tokens to troubleshoot.

Stop the SIWC-enabled LiteLLM owner before administrative mutation. To sign out
inside the guest as its runtime identity:

```sh
aginctus gateway siwc \
  --state-dir /var/lib/aginctus/chatgpt/siwc logout --account personal
```

Logout attempts issuer-discovered refresh-session revocation with bounded
backoff, then removes local access/refresh/ID tokens while retaining the
registration and host ID. Unconfirmed remote revocation returns a nonzero exit
and directs the user to disconnect Aginctus in ChatGPT Settings. Disable/remove
the route or leave it fail-closed until explicitly re-enrolled. Stop requests
before logout; local deletion cannot recall already completed upstream work.

| Symptom | Action |
| --- | --- |
| `plan_disabled` / scope not granted | Explicitly sign in with `--enable-plan`; do not switch billing implicitly |
| Owner busy / second worker fails startup | Stop the authoritative worker; never unlink lock files or share the session with replicas |
| `recovery_required` / uncertain refresh outcome | Stop owner and reauthorize; a consumed rotating token must not be replayed |
| Terminal refresh error | Tokens are cleared; reauthorize using the retained issued client registration |
| Temporary provider 5xx | Credentials remain; retry later with bounded operator-controlled backoff |
| 401/403 admission | Check account, granted permission, workspace policy and request ID; do not loop through OAuth |
| Usage limit / eligibility error | Inspect the structured error code and [ChatGPT Usage settings](https://chatgpt.com/settings/usage); do not rotate accounts |
| Unsupported request field/tool | Correct the client request/profile; no Chat Completions or hosted-tool compatibility is claimed |
| Binding identity changed | Review account/workspace consent, stop owner, explicitly unbind/rebind |

## Storage and security boundary

The store is a 0700 owned non-symlink directory containing a 0600 regular,
single-link `state.json` and persistent private advisory locks. Account records
are label-separated; issued client/subject identity is enforced at reauthorization
and route binding. State writes use fsynced private temporary files, atomic rename
and parent-directory fsync. Refresh rereads under the lock and persists rotated
tokens together. No API key, Codex cache or environment credential fallback exists.

A durable `refresh_pending` marker precedes the network exchange. An interruption,
malformed successful response, or ambiguous transport failure leaves the original
record protected but unusable until reauthorization. This cannot recover a
replacement refresh token lost between server rotation and local persistence;
it prevents destructive retry/reuse rather than claiming impossible distributed
atomicity. Known temporary OAuth error responses preserve the previous session.

The gateway alone holds subscription credentials. Capsule transport and
administrative files are trusted operator operations; this implementation does
not add a generic token API, automated Incus rollout, tenant isolation from an
Incus/root administrator, or encryption at rest beyond host storage protections.
Do not enable unrestricted debug/HTTP-header telemetry or back up live token
stores. Inference failures preserve HTTP status and recognized error codes with
fixed diagnostics; arbitrary provider messages are redacted to avoid credential
reflection. Provider tokens are removed from retained HTTP request headers.
The pinned LiteLLM proxy uses HTTP status in its outer `error.code`; the
provider's recognized machine code and parameter are preserved under
`error.provider_specific_fields.siwc.error` and in the
`x-aginctus-siwc-error-code` header. A valid upstream request ID is retained in
that structured SIWC object and `x-request-id`. This adapter-specific error
envelope is not a claim of byte-for-byte upstream error passthrough.

## Automated and live verification

```sh
nix --quiet develop --no-update-lock-file -c go test ./...
nix --quiet develop --no-update-lock-file -c just check all
```

The standard suite includes Go signed-JWT/mock-provider enrollment, callback,
binding, transfer, refresh and logout tests; Cobra help/validation and Python-free
CLI tests; a cross-language state/lock compatibility probe; and Python tests of
the LiteLLM refresh owner. The actual proxy suite requires an explicitly supplied environment with
`litellm[proxy]==1.103.0` and its runtime dependencies, including Prisma. It uses
synthetic OAuth responses and an injected HTTP mock, never real credentials:

```sh
AGINCTUS_TEST_PINNED_LITELLM=1 LITELLM_LOCAL_MODEL_COST_MAP=True \
  python -m unittest discover -s scripts/gateway -p test_siwc_litellm.py -v
```

Optionally set `AGINCTUS_TEST_OPENCODE_BINARY` to a reviewed OpenCode executable
to exercise the real launcher/client against the synthetic proxy. That mode
requests a fixed text response and does **not** execute model tools. Actual tool
execution belongs in the existing disposable VM acceptance estate.

After explicit account consent and deployment approval, use the existing
secret-safe smoke helper with the SIWC contract:

```sh
python scripts/gateway/authenticated-inference-smoke.py \
  --endpoint http://PRIVATE_GATEWAY_IP:4000 --alias siwc-coding \
  --workload-key-file /private/aginctus-workload-key --siwc --run
```

The helper also needs the existing protected gateway master-key state for its
ingress authorization checks. It prints only a predetermined completion result
and correlation marker, not model text or tokens. This consumes plan usage and
is never a normal CI step. Follow with real Herdr/OpenCode tool execution in the
approved environment, gateway restart/persisted refresh, logout/revocation,
negative-key/model/account checks and workload credential/egress isolation.
Keep #187/#171/#172 open until those live gates are actually observed.
