# Pilot API-key model registration (issue #122)

**Stage 3** of the [gateway operator decision path](gateway-operator-path.md#3-register-an-authorized-upstream).
This is opt-in model registration, not inference or permission to rebuild an
existing gateway. Complete read-only network/isolation verification before
separately approving registry changes.

## Preconditions

1. Confirm existing PostgreSQL connectivity, gateway DNS/application readiness
   and gateway-only egress using [firewall pilot validation](firewall-pilot-validation.md).
   Rediscover the owned LiteLLM instance's current management IPv4; old observed
   addresses are not configuration. Do not run gateway apply as a repair.
2. Select an explicitly authorized provider/model with Responses support and an
   independently obtained API key. Native ChatGPT OAuth is a different workstream.
3. Work from the trusted operator host over the private management bridge. Its
   administrative HTTP connection is plaintext on that bridge; do not port-forward
   it or invoke from untrusted networks. Validate upstream TLS separately.
4. Keep the provider key in a caller-owned 0600 regular nonsymlink file, under
   a private parent, outside Git and the Nix store. Use the existing protected
   gateway master/salt state belonging to the same operator authority. Do not
   print it, copy it to a workload, or rotate the salt as part of registration.
5. Confirm actual model-list and registration behavior against the selected pinned
   runtime. An unexpected response shape is a failure, not permission to proceed.

## Operator commands

Preview without reading credentials or contacting LiteLLM. `GATEWAY_ENDPOINT`
must be the rediscovered private IPv4 endpoint, including port:

```sh
nix --quiet develop -c python3 scripts/gateway/register-openai-model.py \
  --endpoint "$GATEWAY_ENDPOINT" \
  --alias aginctus-verified-openai --source openai/gpt-4o-mini
```

Only after explicit approval, append `--upstream-key-file` with the protected
provider-key path and `--apply` to that reviewed command. This uses the existing
master to POST `/model/new`, then checks alias presence. Model registration
stores the provider API key in the PostgreSQL-backed registry using the unchanged
LiteLLM salt; no secret goes into generated Nix or Compose content.

The tool rejects preexisting aliases, public/hostname endpoints, redirects,
ambiguous response formats, unsafe credential files and missing state. It ignores
HTTP proxy environment settings and prints neither credentials nor untrusted
response bodies. It neither starts/recreates services nor contacts the provider.
A timeout after POST is ambiguous: inspect the exact alias before deciding
whether to retry. Replacement/deletion is a separate operator action.

## Local Aginctus CLI registration (offline)

The CLI supports nonsecret local provider/model intents independently of the
gateway's database registry:

```sh
aginctus gateway provider opencode-go register --alias=go
aginctus gateway model go-coding register --provider=go --source=openai/gpt-6-luna
aginctus gateway provider list
aginctus gateway model go-coding render
```

Add `--dry-run` to either registration command to preview without local writes.
Rendering includes the fixed Go API base and Responses mode, but no key. Local
registration neither synchronizes with LiteLLM nor authorizes an account or
verifies network access; only the deliberately approved Python `--apply` path
changes the live registry.

## OpenCode Go pilot: native Responses endpoint

The fixed `--provider opencode-go` registrar supports the reviewed Responses
model allowlist. The [Go documentation](https://opencode.ai/v2/docs/console/go/)
publishes the native route at
`https://opencode.ai/inference/go/openai/v1/responses`. The registrar stores
base `https://opencode.ai/inference/go/openai/v1`, a LiteLLM `openai/` model
prefix, and `mode=responses`. Other protocols and arbitrary upstream URLs are
not accepted by this registration path. Verify current account/model entitlement
in the operator's console before selecting a model.

The pilot alias is `aginctus-go-pilot`, with initial upstream candidate
`openai/gpt-6-luna`; `opencode-go/gpt-6-luna` is a different client-side selector.
Preview:

```sh
nix --quiet develop -c python3 scripts/gateway/register-openai-model.py \
  --endpoint "$GATEWAY_ENDPOINT" --provider opencode-go \
  --alias aginctus-go-pilot --source openai/gpt-6-luna
```

Do not re-register an existing alias just to test forwarding. Registration success
establishes only the database route, not entitlement, upstream inference or
session fidelity.

## Selective Go forwarding and offline acceptance

Use [selective Go forwarding operations](selective-go-forwarding.md) for the
default-off setting, embedded callback packaging, immutable image requirements,
complete offline commands, delivery audit, migration and rollback.

**Stock model-group/global forwarding is rejected for the Go pilot.** Actual
pinned native Responses auditing observed unrelated metadata forwarding and loss
of the original identifying UA. A model-group list is not a header allowlist.
The selective policy preserves only native session and identifying UA, rejects
malformed/missing identity before upstream execution and disallows Chat Completions
for the approved alias. Both renderers declare exact embedded public artifacts;
the OCI runtime provides pinned dependencies with a strict version guard.

The synthetic acceptance exercises actual OpenCode, LiteLLM, local DB registration
and restricted-key behavior, but does not deploy or verify the live pilot.
Neither a unit-test pass nor a healthy proxy proves active callback loading or
successful Go inference.

### Stock forwarding negative-audit fixture

The retained audit can reproduce the stock-policy failure without a provider.
With the stock `ghcr.io/berriai/litellm:v1.103.0` image already cached, run:

```sh
nix --quiet shell nixpkgs#podman -c podman run \
  --signature-policy "$PWD/deploy/litellm-go/podman-probe-policy.json" \
  --pull=never --rm --network none --read-only --tmpfs /tmp:rw,nosuid,nodev \
  -e AGINCTUS_ISOLATED_LITELLM_PROBE=1 -e PYTHONDONTWRITEBYTECODE=1 \
  -v "$PWD:/probe:ro" --entrypoint /app/.venv/bin/python \
  ghcr.io/berriai/litellm:v1.103.0 \
  /probe/scripts/gateway/probe_pinned_litellm_responses.py \
  /probe/deploy/litellm-go/model-group-forwarding.yaml --require-safe
```

Its expected nonzero strict verdict rejects unsafe forwarding, not transport
functionality. The command-local Podman policy rejects other image references
and does not verify publisher signatures; it is a test prerequisite, not a
production trust policy. Use the digest-qualified packaged probes in the
selective runbook for positive acceptance.

## Required live compatibility and cleanup gates

After separate deployment and spending authorization, use the
[restricted workload-key procedure](gateway-workload-key-pilot.md), then the
genuine Herdr/OpenCode path under [#123](https://github.com/blevinn/aginctus/issues/123).
Require a terminal successful Responses result, native session/UA continuity and
trusted gateway/provider attribution without logging raw headers, keys, prompts
or spend rows. Keep provider account usage controls in place: the proxy's dollar
budget does not independently attest subscription accounting.

The generic [inference smoke](gateway-inference-smoke.md) sends synthetic traffic
and does not provide a genuine OpenCode conversation identity. It cannot replace
the Go compatibility gate; do not manufacture a session header to make it pass.
`aginctus gateway responses verify` validates a captured stream's contract, not
its upstream provenance. Always revoke the exact temporary workload key and
verify denial, preserving protected evidence if cleanup is uncertain.

[#122](https://github.com/blevinn/aginctus/issues/122) owns actual upstream inference,
[#123](https://github.com/blevinn/aginctus/issues/123) the managed workload path,
[#121](https://github.com/blevinn/aginctus/issues/121) model-driven tools, and
[#124](https://github.com/blevinn/aginctus/issues/124) repeatable all-up proof.
Offline synthetic acceptance does not close these live requirements.

## Validation

```sh
nix --quiet develop -c python3 -m unittest discover \
  -s scripts/gateway -p 'test_register_openai_model.py' -v
nix --quiet develop -c just check-all
git diff --check
```

These hermetic checks do not contact the pilot or a real provider. The separate
[offline forwarding probes](selective-go-forwarding.md#reproduce-without-contacting-providers)
require the documented cached OCI/runtime prerequisites.
