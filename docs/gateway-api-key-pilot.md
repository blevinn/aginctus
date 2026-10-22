# Pilot API-key model registration (issue #122)

Status: opt-in, operator-managed route registration; **not** a successful live
inference test. This avoids rebuilding or reapplying an existing Compose gateway.
The pinned LiteLLM version is v1.103.0: confirm the actual pilot's
/model/new and /v1/models behavior before treating registration as ready.

## Preconditions

1. On the pilot, confirm PostgreSQL, guest DNS, LiteLLM application readiness
   and gateway-only egress per docs/firewall-pilot-validation.md.
   On the refreshed pilot, both guests were Running and LiteLLM
   `/health/liveliness` and `/health/readiness` returned HTTP 200 on
   2026-10-17. Reconfirm before live use; never use gateway apply as a repair.
2. Select an explicit provider/model that the operator is authorized to call
   with an independently obtained API key (not ChatGPT subscription OAuth).
   Confirm the chosen upstream supports Responses when needed for OpenCode.
3. Confirm the gateway is reachable from the **trusted pilot host** at its
   observed private IPv4 address on port 4000. Do not publish or forward it.
   This deployment uses plaintext HTTP on the private management bridge,
   so operator credentials traverse that bridge. Do not invoke from a workload,
   public machine or untrusted network. Validate **upstream TLS** separately.
4. Store the authorized provider key in an operator-owned 0600 regular file
   **outside the repository and /nix/store**. Never paste its value into a
   command, log, CI output or chat. The registrar reads the existing protected
   gateway master/salt state and never rotates or recreates credentials.
5. Verify the pinned image's model-list response follows the documented
   data array of entries containing string id; unexpected responses fail closed.

## Operator commands

Preview (read-only; does not read credentials or contact LiteLLM):

~~~sh
# Replace the illustrative IP with the observed private gateway address.
nix develop -c python3 scripts/gateway/register-openai-model.py \
  --endpoint http://10.42.40.15:4000 \
  --alias aginctus-verified-openai \
  --source openai/gpt-4o-mini
~~~

The address above is illustrative, **not** a statement of pilot configuration.
When the operator has approved the model, key, and live mutation, append
the following options to that same reviewed command:

~~~sh
  --upstream-key-file /absolute/operator/private/provider.key --apply
~~~

**Mutation:** --apply uses the existing LiteLLM master key to register only the
explicit model through the administrative POST /model/new API. LiteLLM stores
the API key in its PostgreSQL-backed model registry and documents encryption
using the unchanged LITELLM_SALT_KEY. Never rotate or regenerate that salt.

The command fails closed for preexisting aliases, public/hostname endpoints,
redirects, ambiguous listing formats, loose or symlinked secret files, and
missing secrets. HTTP proxy environment settings are explicitly ignored.
The command prints neither secrets nor untrusted HTTP response bodies.
It neither changes Incus nor starts or recreates LiteLLM or PostgreSQL.

A timeout after POST is **ambiguous**: inspect the gateway's model aliases
before manually deciding whether to retry. There is no automatic rollback.
Deletion or replacement of a model is a separately reviewed operator action
with its own consumer and credential impact.

Registration success only means the alias appears in the gateway's model
list. It **does not** establish upstream authentication, TLS, inference,
Responses contract fidelity, or workload authentication.

Before pilot inference, issue a *restricted and revocable* LiteLLM virtual
key that permits only the selected model alias; never use the master key as
a workload credential. Test that its permissions and expiry work under the
pinned image. Verify a real /v1/responses request reaches a terminal
response.completed event with status=completed and nonempty model output,
and verify invalid LiteLLM credentials fail. The existing
aginctus gateway responses verify command can validate the SSE contract
of a stream captured through a trusted operator path; it cannot attest
that the stream originated from an upstream provider.

#123 owns OpenCode guest endpoint and credential installation; #124 owns
Herdr/OpenCode end-to-end acceptance. This registrar does not touch native
ChatGPT OAuth, its token volume, or the other sessions' branches.

## Local Aginctus CLI registration (offline)

The Go CLI also supports OpenCode Go **nonsecret local provider and model
intents**. This is separate from the LiteLLM database registration above:

~~~sh
aginctus gateway provider opencode-go register --alias=go
aginctus gateway model go-coding register \
  --provider=go --source=openai/gpt-6-luna
aginctus gateway provider list
aginctus gateway model go-coding render
~~~

The rendered definition includes the fixed OpenCode Go API base and
`mode=responses`, but **no API key**. Add `--dry-run` to either
`register` command to preview without changing local state. The accepted
OpenCode Go model IDs match the guardrails in the pilot registrar. Local
registration does not apply or synchronize the model with LiteLLM, authorize
a Go account, install a credential, or verify network access. Only the
separately approved Python `--apply` path changes the running LiteLLM model
registry. No live request is part of these commands.

## OpenCode Go pilot: native Responses endpoint

The operator selected **OpenCode Go** for #122 (2026-10-17), rather than a
direct OpenAI provider account. The canonical
[OpenCode Go documentation](https://opencode.ai/v2/docs/console/go/)
publishes native Responses at
`https://opencode.ai/inference/go/openai/v1/responses`
for `gpt-6-luna`, `gpt-5.6-luna`, `grok-4.7` and the supported
Muse Spark contributor models. This registrar now has a **fixed**
`--provider opencode-go` route for precisely those Responses-capable
model identifiers. The value stored as `litellm_params.api_base` is
`https://opencode.ai/inference/go/openai/v1`; the model stored as
`litellm_params.model` has the LiteLLM `openai/` provider prefix.
Other OpenCode Go models use different protocols and are rejected in
this native Responses-only pilot path. There is no arbitrary upstream
URL parameter or provider hostname override.

The default provider is still `openai`. That branch retains the original
behavior and does not add an API base to its model definition.

**Preferred initial pilot:** registered alias `aginctus-go-pilot`
maps to LiteLLM model `openai/gpt-6-luna`. This is the upstream
model ID `gpt-6-luna`, **not** the OpenCode UI model selector
`opencode-go/gpt-6-luna` (the latter is a client-side provider/model
identifier). The provider model list can change; verify in the
OpenCode Console before mutation.

A **non-network preview** against the previously observed private
gateway IPv4 (rediscover it before live use):

```sh
nix develop -c python3 scripts/gateway/register-openai-model.py \
  --endpoint http://10.144.44.70:4000 \
  --provider opencode-go \
  --alias aginctus-go-pilot \
  --source openai/gpt-6-luna
```

Only after the user has an authorized OpenCode Go subscription and API
key, confirmed the selected model and account entitlements, and approved
this specific LiteLLM model-registry mutation:

```sh
# The key is an already populated owner-only 0600 regular file,
# outside Git, the Nix store, and shell history.
nix develop -c python3 scripts/gateway/register-openai-model.py \
  --endpoint http://10.144.44.70:4000 \
  --provider opencode-go \
  --alias aginctus-go-pilot \
  --source openai/gpt-6-luna \
  --upstream-key-file /absolute/private/opencode-go.key \
  --apply
```

The new route never calls the upstream on dry-run or registration.
Successful registry lookup proves *only* that LiteLLM stored the alias.

### Required live Go compatibility gate — before claiming #122 complete

OpenCode Go states it is designed for coding-agent traffic and that
clients should send a real identifying `User-Agent` and a **stable**
`x-opencode-session` value for each conversation. OpenCode warns about
proxy/client session-header omissions. LiteLLM may recompose upstream
requests and headers; **neither the registrar nor its tests prove that
the pinned v1.103.0 upstream request preserves those headers**. Do not
spoof OpenCode's identity or silently mint a new session ID per request.
The actual model call requires operator-controlled inspection of safe
gateway-side header **presence and continuity** (never Authorization,
provider keys, or prompt bodies), along with provider-side request
confirmation. A missing session header is a compatibility problem for
a follow-on slice, not permission to bypass the proxy or claim
successful acceptance.

For the real test, issue a temporary LiteLLM alias-scoped virtual key
using `docs/gateway-workload-key-pilot.md`, then use
`docs/gateway-inference-smoke.md` with this real alias. The smoke
utility currently sends a single challenge request and does **not**
independently prove the Go session-header contract or ordinary coding
traffic. Follow-up #123 must validate true OpenCode session identity
end-to-end. Since the Go subscription has provider-managed usage
limits, do not assume LiteLLM's internal $1 virtual-key budget
accurately models provider credits unless cost accounting has been
confirmed. Keep the selected account's own usage controls in place.

No native ChatGPT OAuth, added network permissions, gateway restart,
public listener, Go subscription checkout or billable inference
belongs to this opt-in registration workflow.

## Full validation

~~~sh
nix develop -c python3 -m unittest discover \
  -s scripts/gateway -p 'test_register_openai_model.py' -v
nix develop -c python3 -m py_compile \
  scripts/gateway/register-openai-model.py \
  scripts/gateway/test_register_openai_model.py
git diff --check origin/main...HEAD
just check
nix flake check
git status --short
~~~

The Python tests are deterministic and offline; they **do not** contact a
real LiteLLM instance or an upstream provider. Run the above checks from the current checkout. They do not authorize any live state-changing operation.
