# Pilot API-key model registration (issue #122)

Status: opt-in, operator-managed route registration; **not** a successful live
inference test. This avoids rebuilding or reapplying an existing Compose gateway.
The pinned LiteLLM version is v1.103.0: confirm the actual pilot's
/model/new and /v1/models behavior before treating registration as ready.

## Preconditions

1. On the pilot, confirm PostgreSQL, guest DNS, LiteLLM application readiness
   and gateway-only egress per docs/firewall-pilot-validation.md.
   LiteLLM was last observed **Stopped**. Do not run this registrar before
   it is running and application-ready; never use gateway apply as a repair.
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

## Full validation

~~~sh
git fetch origin
git switch feat/122-api-key-model-registration
git pull --ff-only
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
real LiteLLM instance or an upstream provider. Run the above validation on
the PR head before approving any live state-changing operation.
