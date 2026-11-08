# Sign in with ChatGPT through the managed gateway

The compiled `openai-siwc` plugin implements OpenAI's dynamic-client SIWC
contract and public `https://api.openai.com/v1/responses`. It does not use
LiteLLM's native `chatgpt/` provider, a Codex identity/device flow or ChatGPT's
`backend-api`. LiteLLM remains the only workload-facing gateway and refresh owner.

## Configuration and enrollment

Provider/catalog declarations describe the desired configuration. `gateway apply`
delivers runtime files and model routes, provisions the owned credential volume,
verifies the worker identity, initializes private storage and reconciles bindings.
It can restart LiteLLM, so use the normal approved gateway maintenance window.
`gateway provider login` performs browser enrollment and commits credentials
directly into that managed store. There is no separate SIWC initialization,
binding, rendering, bundling or capsule-transfer command.

Use an immutable, reviewed LiteLLM image digest in `gateway.images.litellm`.
The runtime verifies its supported LiteLLM APIs at import. Keep one worker;
the lifetime lease rejects a second owner. Broad client-header forwarding is
incompatible with SIWC. Selective OpenCode-Go forwarding is composed explicitly
into the generated configuration and retains its existing model restrictions.
Existing ordinary provider routes remain separate.

Start by declaring a named provider, either through the normal provider apply
command or the shared NixOS/Home Manager settings:

```sh
aginctus gateway provider apply openai-siwc --provider personal
aginctus gateway apply
aginctus gateway provider login --provider personal \
  --consent-connected-app --consent-background
aginctus gateway model list --provider personal
```

Normal configuration selection applies to all these commands, including
`--configuration-file`. Login starts a bounded loopback callback and opens the
browser on the operator's host. Open the printed authorization link on that
same host if a graphical opener is unavailable. Returning sign-in reuses the
gateway's registration and stable host identity. Canceled sign-in preserves
the previous session. `--enable-plan` explicitly requests plan consent again
when the account has not granted `chatgpt.tokens.use.direct`.

The consent flags authorize only the authenticated user's **connected Aginctus
application**, on that user's controlled runtime, including express background
use. They do not authorize unrelated applications, other users' requests,
account pooling, resale or a general-purpose subscription relay. Review the
[SIWC terms](https://openai.com/policies/sign-in-with-chatgpt-terms/) and
[plan contract](https://developers.openai.com/siwc/token-sharing-open-source).
Workspace policy, eligible models and usage limits remain provider decisions.

## Declare models and workload access

Choose an exact slug from authenticated discovery. The following Nix example
uses `ACCOUNT_CATALOG_SLUG` as a placeholder, not an assumed model name:

```nix
services.aginctus.settings.gateway = {
  providers.personal = {
    plugin = "openai-siwc";
    config = {
      models.ACCOUNT_CATALOG_SLUG.source = "ACCOUNT_CATALOG_SLUG";
      routes.siwc-coding = {
        catalog = "coding";
        workload_key_file = "/private/aginctus-workload-key";
      };
    };
  };
  catalogs.coding.models.siwc-coding = {
    provider = "personal";
    name = "ACCOUNT_CATALOG_SLUG";
  };
};
```

The same provider configuration object is accepted by `gateway provider apply
openai-siwc --provider personal --config-file FILE`. Apply the catalog through
the normal catalog command when using local declarations. Do not register a
configuration-owned name again locally. Home Manager uses
`programs.aginctus.settings` with the same schema.

The referenced file contains an existing, dedicated **LiteLLM workload key**,
never an OpenAI token. Provision its alias scope through the existing
[workload key lifecycle](gateway-workload-key-pilot.md). It must be owned,
mode 0600, regular and single-link. Only its SHA-256 identity reaches the binding.
Catalog inheritance preserves the original provider identity and source slug.
MCP aliases do not create or rename tool grants.

After changing configuration:

```sh
aginctus gateway validate
aginctus gateway render
aginctus gateway apply
aginctus gateway provider status --provider personal
```

Apply rechecks configured models against the consented account's displayed
catalog, updates workload-key bindings and removes deleted routes. Accounts
without recorded connected-application/background consent remain unbound until
login. Applying declarations never starts OAuth or silently grants consent.
Configuration and Nix evaluation contain no provider credentials.

Select the `siwc-coding` alias through the normal OpenCode gateway configuration
with `protocol: responses` and its dedicated workload key. The packaged SIWC
profile sets `store:false`, omits unsupported sampling/token-limit fields and
preserves tool-call continuation. Restart OpenCode after changing the launcher.

## Credential ownership and recovery

The authoritative store is inside the owned LiteLLM-only credential volume.
Incus project, instance and volume ownership/exclusivity are audited before
credential exchange. A one-shot authenticated exec verifies the actual LiteLLM
process UID/GID and mount, drops privileges, and holds an administrative lease.
Active SIWC streams retain shared admission leases until close, so enrollment
cannot race inference or rotating refresh tokens. Other providers retain their
normal request path. This adds no daemon, public token endpoint or network grant.

The host CLI holds an in-memory copy for enrollment/discovery and returns it
over authenticated exec stdin. Tokens never enter argv, environment, host files,
Nix derivations or workload directories. The guest atomically publishes private
updates with fsync and acknowledges the commit. Pending updates are generation
checked; incomplete delivery and stale replay fail closed. The store is owned,
mode 0700, with 0600 single-link regular files and persistent no-follow locks.

Refresh runs only inside LiteLLM during inference, or during an explicit managed
operation under its administrative lease. Ambiguous refresh persists a recovery
marker instead of replaying a potentially consumed rotating token. Reauthorize
with the normal provider login command; do not copy, restore or edit token files.
An account/registration mismatch is rejected rather than silently rebound.

```sh
aginctus gateway provider logout --provider personal
```

Logout attempts remote revocation, clears local tokens and route consent, and
retains registration identity. Unconfirmed revocation returns nonzero and asks
the user to disconnect Aginctus in ChatGPT Settings. Inference remains closed
until login. Status reports local state, not remote entitlement verification.

Trusted host/Incus administrators and reviewed images remain inside the
credential boundary. Do not back up live tokens or enable header/debug logging.
Provider errors retain recognized codes/request IDs but redact arbitrary text.
SIWC requests reject caller credential/destination/account overrides, hosted
tools, automatic retries and provider/account/billing fallback.

## Validation boundary

```sh
nix --quiet develop --no-update-lock-file -c just check all
nix --quiet develop --no-update-lock-file -c go test -race ./internal/siwc ./internal/gateway ./internal/cli ./internal/gatewaycontrol
nix --quiet build --no-link .#aginctus .#opencode-workload
```

Signed synthetic enrollment tests, real Go/Python exchange tests and pinned
LiteLLM/OpenCode integration exercise the protocol without a live account.
The opt-in proxy suite requires an isolated environment with
`litellm[proxy]==1.103.0`, Prisma and its runtime dependencies:

```sh
AGINCTUS_TEST_PINNED_LITELLM=1 LITELLM_LOCAL_MODEL_COST_MAP=True \
  python -m unittest discover -s scripts/gateway -p test_siwc_litellm.py -v
```

Set `AGINCTUS_TEST_OPENCODE_BINARY` to the reviewed client to include actual
OpenCode execution against the synthetic proxy. Synthetic completion and tool
continuation are not live model-driven filesystem execution.

Actual Incus rollout with the reviewed image, runtime UID/mount confirmation,
real account/workspace consent, paid inference, gateway restart/logout and
workload egress/tool isolation require separately authorized live acceptance.
Keep [#187](https://github.com/blevinn/aginctus/issues/187),
[#171](https://github.com/blevinn/aginctus/issues/171) and
[#172](https://github.com/blevinn/aginctus/issues/172) open until those observations
are recorded. Follow the [gateway operator path](gateway-operator-path.md) for
the existing firewall and deployment controls.
