# Gateway: API-key provider/model registration

**Stage 3** of the [canonical gateway workflow](README.md#3-register-an-authorized-upstream).
This is opt-in model registration, not inference or permission to rebuild an
existing gateway. Complete read-only network/isolation verification before
separately approving registry changes.

## Preconditions

1. Confirm existing PostgreSQL connectivity, gateway DNS/application readiness
   and gateway-only egress using [firewall pilot validation](../host/firewall-pilot-validation.md).
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

## OpenCode Go: separate registration and forwarding contract

The generic API-key registration workflow above does not authorize native
OpenCode Go session forwarding. For the approved Go alias, use the
[selective Go compatibility and negative-audit appendix](selective-go-forwarding.md#go-registrar-compatibility-and-stock-policy-negative-fixture), followed by
that guide's explicit configuration, offline acceptance, delivery audit and
rollback gates. Do not re-register a model just to test forwarding, and do not
treat a stock model-group header configuration as equivalent to a selective
allowlist.

## Operator-authorized live compatibility and cleanup gates

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
