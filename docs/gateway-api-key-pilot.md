# Pilot API-key model registration (issue #122)

**Stage 3** of the [gateway operator decision path](gateway-operator-path.md#3-register-an-authorized-upstream). Finish read-only network and isolation verification before separately approving registration. This is not an inference test; the live #122 operator hold remains in force.

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

### Declarative model-scoped forwarding (#456)

Aginctus supports [declarative client header forwarding](configuration.md#declarative-client-header-forwarding)
globally or for selected model groups. For the pilot's one approved alias,
configure a list on the operator-managed host:

```nix
services.aginctus.settings.gateway.litellm.forwardClientHeaders = [ "aginctus-go-pilot" ];
```

The default is `false`; `true` selects global forwarding and a string list
selects model groups. This pilot example uses only the listed alias. Both
`aginctus gateway render` and the actual Jsonnet orchestration plan declare
the same LiteLLM config, delivered by pinned incus-compose's inline `configs`
support to `/app/aginctus-client-headers.yaml`. Only LiteLLM receives that file and
the matching `environment.CONFIG_FILE_PATH`; PostgreSQL and runtime secrets
are unchanged. The pilot list renders stock LiteLLM YAML:

```yaml
model_list: []
litellm_settings:
  model_group_settings:
    forward_client_headers_to_llm_api:
      - aginctus-go-pilot
```

The existing `deploy/litellm-go/model-group-forwarding.yaml` remains an
offline reference; tests compare its parsed policy with the declared content
from **both** renderers. No host bind mount, custom Python callback, alternate
gateway image, new model registration or provider secret is introduced.
`model_list: []` does not remove or re-register the separately managed
database model alias.

**Pilot migration and rollback:** a manually staged
`/app/aginctus-go-pilot.yaml` and its `environment.CONFIG_FILE_PATH` on owned
`aginctus-gateway/litellm-1` differ from the generic declared target above.
The old path is drift, not an automatically adopted equivalent policy.
After a human reviews the exact running instance ownership, attached volumes,
image fingerprint, gateway/PostgreSQL readiness, the current source YAML and
the proposed render, update the pinned Aginctus package and host NixOS
settings. Keep automatic gateway reconciliation disabled until the rollout
is explicitly approved and planned. Plan an ownership-checked, LiteLLM-only
config-path update and controlled stop/start, preserving the old file and
path for rollback; do not run `aginctus gateway apply`
to repair drift blindly. The pinned Compose driver rejects incompatible
existing instance settings and will not automatically recreate it. The
inline config is written to newly created or stopped/started instances; do
not mistake rendering or a no-op apply on an *already running* instance for
proof that its on-disk file was rewritten. Inspect the new policy file,
effective config path, active config, and model registry after the change.
Retire the old staged file only after confirming activation and rollback.

If the policy must be disabled, first set the declared option to `false`,
then plan a **LiteLLM-only**, ownership-checked removal of the previous
`environment.CONFIG_FILE_PATH` and staged file with a restart, separately
verifying application/database readiness; Incus Compose does not
automatically delete surplus environment fields or files. Never recreate
PostgreSQL, reset salts, rotate master/provider keys, or broaden network
routing as part of this migration. This is a controlled pilot adoption,
not a completed production reconciliation acceptance.

**Security limitation:** The list selects model groups, not individual
headers; boolean `true` forwards globally without per-model opt-outs.
LiteLLM's pinned forwarding helper admits eligible
`x-*` headers except `x-stainless*`, and `anthropic-beta`. That may leak
synthetic `x-custom-secret`, tracing, or workload metadata. Its separate
credential-cleaning behavior must be verified against adversarial inputs
before this feature is approved. The helper does **not** select the
original `User-Agent` or canonicalize `x-opencode-session-id` into
`x-opencode-session`; native upstream compatibility remains uncertain.

Run these no-provider checks first:

```sh
nix --quiet develop -c go test ./internal/gateway -run 'TestClientHeaderForwarding' -v
nix develop -c python3 -m py_compile scripts/gateway/probe_model_group_forwarding.py

# Optional pinned stock image probe with networking disabled; this tests the
# real LiteLLM forwarding helper, not the full native Responses transport.
docker run --rm --network none --read-only --tmpfs /tmp \
  -e PYTHONDONTWRITEBYTECODE=1 \
  --mount type=bind,src="$PWD/deploy/litellm-go/model-group-forwarding.yaml",dst=/run/aginctus-go.yaml,readonly \
  --mount type=bind,src="$PWD/scripts/gateway/probe_model_group_forwarding.py",dst=/run/aginctus-probe.py,readonly \
  --entrypoint /app/.venv/bin/python ghcr.io/berriai/litellm:v1.103.0 \
  /run/aginctus-probe.py /run/aginctus-go.yaml
```

The probe prints only synthetic **header names** and forwarded/not-forwarded
classifications, never values. It verifies stock model-scoped helper
behavior but **not** an actual outbound native Responses HTTP request, DB
model registration, or provider-credential separation. Those remain
mandatory, offline/mock-upstream gates under #456. A declarative opt-in
does not by itself authorize pilot activation, gateway rebuild, global
forwarding, wildcard groups, broad provider-auth forwarding or billable
inference. If
actual pinned tests show missing identifying `User-Agent`, incompatible
session-header spelling, or unacceptable `x-*` leakage, reassess the
inactive selective hook merged in #470 as a targeted fallback.

### Opt-in selective Go header callback (offline policy increment; #456)

`scripts/gateway/opencode_go_forwarding.py` and
`scripts/gateway/litellm_opencode_go_hook.py` implement an **inactive, optional fallback** LiteLLM v1.103.0
`CustomLogger.async_pre_call_hook` contract. Prefer the built-in, model-scoped
configuration above unless the pinned acceptance gates require this hook.
For native `aresponses` targeting the approved pilot model alias
`aginctus-go-pilot`, it reads only the proxy-generated, credential-redacted
`proxy_server_request.headers`; requires a native `ses_...` session in
`x-opencode-session-id` or `x-opencode-session` plus a recognizable
`opencode/VERSION` User-Agent; and replaces the provider-bound header map
with **only** `x-opencode-session` and `User-Agent`. Two conflicting native
headers, invalid/missing/oversized values and generic HTTP-library User-Agents
fail closed. It never issues its own session ID or forwards the workload
bearer token, `x-api-key`, arbitrary `x-*`, or body-supplied
`extra_headers`. Other aliases and non-Responses calls are not modified.

This provides a verifiable *policy and callback contract*; it is **not an
activated runtime integration**. To run the callback, the two Python files
must be packaged in the actual LiteLLM image's Python import path, and an
explicit `litellm_settings.callbacks` configuration must point to
`litellm_opencode_go_hook.proxy_handler_instance`. No current Aginctus
Compose deployment installs either file or enables this callback. The pinned
LiteLLM callback-loading behavior, dispatch order, actual header metadata,
Responses adapter, provider credentials and upstream HTTP delivery must be
verified with the *real* pinned LiteLLM image and an offline mock provider
before applying any configuration to the pilot. The callback's narrow header
allowlist is a different policy from the built-in global or model-group
forwarding modes. Selecting global mode requires accepting eligible-header
exposure across all upstreams; it does not provide this callback's allowlist.

Run the local, offline unit suite:

```sh
nix develop -c python3 -m unittest discover \
  -s scripts/gateway -p 'test_opencode_go_forwarding.py' -v
```

The hardcoded pilot alias and conservative native `ses_...` and
`opencode/VERSION` header formats intentionally fail closed if the pinned
OpenCode client uses different values. Check the actual pinned executable
with `guest/opencode/test_real_runtime.py` before runtime integration, not
by weakening the filters. #456 remains open until the runtime hook is
**installed and exercised** through a controlled, credential-safe mock
upstream. A real OpenCode Go inference request remains separately gated by
#122 and #123; existing pilot credentials are revoked.

### Isolated pinned LiteLLM native Responses transport audit (#456)

The merged #518 client integration proves real OpenCode sends the session header
to a synthetic local Responses endpoint, **not** that LiteLLM sends it to
the upstream Go provider. The `probe_pinned_litellm_responses.py` utility
executes **actual stock LiteLLM v1.103.0** with a locally configured static
Go alias and an unrelated alias, plus a local mock Responses provider, inside
one OCI container. Container networking is **disabled**, and the script refuses
to execute unless the only interface is loopback and the explicit opt-in flag
is present. No provider credentials, live DB registration, host Incus changes
or paid model request are involved.

The probe reads and verifies the exact `model-group-forwarding.yaml` policy.
It creates synthetic *different* proxy and upstream bearer keys; verifies
unauthenticated requests are denied; issues multiple streaming native
`/v1/responses` requests using repeated and distinct synthetic session IDs;
checks the provider-bound session values are byte-for-byte identical, ensures
the unrelated model group has no forwarded Go session, checks provider auth
is replaced rather than leaked, and classifies `x-*`/`anthropic-beta` and the
original identifying `User-Agent`. Its terminal output contains only
PASS/RISK/FAIL classification, never header values or session IDs.

**This is a measurement, not security-policy acceptance.** A successful
transport may still print RISK for the stock selector's arbitrary eligible
client `x-*` header exposure or for loss of the OpenCode `User-Agent`. The
stricter `--require-safe` option exits nonzero for those risks. Such a
failure is evidence to evaluate the existing selective #470 callback, *not*
permission to enable global forwarding or relax the isolation policy.

Execute from the repository root. On NixOS, use **rootless Podman from Nix**:
no Docker daemon and no system-level Podman installation is required.
Podman obtained through `nix shell` on this host did not find either
`$HOME/.config/containers/policy.json` or `/etc/containers/policy.json`.
The previous suggestion to set `CONTAINERS_POLICY_JSON` was incorrect:
this environment variable does not select Podman's image policy.
For this audit, pass the supported **`podman run --signature-policy PATH`**
option, pointing to `deploy/litellm-go/podman-probe-policy.json`.
Podman uses that policy for this invocation only.
That file rejects all image sources by default and permits precisely
`ghcr.io/berriai/litellm:v1.103.0` for this fixture. The
`insecureAcceptAnything` entry means **no cryptographic signature check**
is performed on that one tagged image: it is not a verification of publisher
identity or immutable provenance. Do not use the file for production images
or install it as a global/default trust policy. Verify an immutable OCI
digest before any rollout, and keep this policy scoped to this disposable test.

Rootless Podman still requires the host's preexisting user-namespace/subuid
configuration and setuid `newuidmap`/`newgidmap` helpers; if these are absent,
do not modify the host for this test. Use the existing disposable test VM/
host-agent environment or report the prerequisite blocker. Neither Podman
nor the image download is allowed to make provider requests; **only the
isolated test container** is network-disabled.

The command below temporarily obtains Podman from Nix and runs the stock
LiteLLM image with exactly the same network and filesystem restrictions:

```sh
nix shell nixpkgs#podman --command podman run \
  --signature-policy "$PWD/deploy/litellm-go/podman-probe-policy.json" \
  --rm --network none --read-only --tmpfs /tmp:rw,nosuid,nodev \
  -e AGINCTUS_ISOLATED_LITELLM_PROBE=1 \
  -e PYTHONDONTWRITEBYTECODE=1 \
  --mount type=bind,src="$PWD/deploy/litellm-go/model-group-forwarding.yaml",dst=/run/aginctus-policy.yaml,readonly \
  --mount type=bind,src="$PWD/scripts/gateway/probe_pinned_litellm_responses.py",dst=/run/aginctus-probe.py,readonly \
  --entrypoint /app/.venv/bin/python \
  ghcr.io/berriai/litellm:v1.103.0 \
  /run/aginctus-probe.py /run/aginctus-policy.yaml
```

For a host where Podman is already installed, use `podman run` instead
of the `nix shell ... --command podman run` prefix. A Docker daemon is
also compatible but **not required**; substituting `docker run` preserves
the sandbox flags.

Then deliberately rerun the same invocation with `--require-safe` appended.
Do not confuse the expected policy rejection with native Responses transport
failure. For unit tests (no LiteLLM/Docker needed):

```sh
nix --quiet develop -c python3 -m unittest discover \
  -s scripts/gateway -p 'test_pinned_litellm_responses.py' -v
```

A version tag is **not an immutable image digest**; verify the OCI digest
before any promotion. This test intentionally uses transient *static* model
definitions, not the live database-registered Go alias. Model-registry
routing, restricted virtual keys, upstream provider attribution, adversarial
headers from the *actual* OpenCode client, and full Herdr-managed tool
execution remain separate release acceptance gates. The real paid pilot
must still be explicitly authorized and use a short-lived restricted key
whose post-test revocation is verified.

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
