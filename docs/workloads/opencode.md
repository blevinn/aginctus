# OpenCode workload

Status: OpenCode image, lifecycle, fail-closed launcher and explicit private gateway provisioning/discovery command are implemented. Live managed-workload inference and end-to-end acceptance remain unverified (#122/#123/#124).

## Purpose

The OpenCode workload is the first concrete Aginctus agent workload.

It is a NixOS Incus container image containing:

- OpenCode from the pinned Nixpkgs revision;
- Herdr from the pinned Herdr flake;
- Git;
- OpenSSH server;
- a non-root `agent` account.

Herdr runs as the `agent` user so the Herdr session and agent runtime share the same workspace identity.

## Build

Build the image with:

```sh
just build opencode-workload
```

The `.#opencode-workload` flake target produces an Incus metadata tarball and squashfs root filesystem under `result-opencode-workload`.

Import or replace the local development image alias with:

```sh
just update image opencode-workload
```

The image is imported as:

```text
aginctus-opencode-workload
```

## Guest behavior

The image enables `sshd` but disables password and keyboard-interactive authentication. Root SSH login is disabled.

No SSH public key is baked into the image. The image provides the fixed `aginctus-ssh-authorize` helper and a dedicated root-owned authorization path for managed client keys. An operator explicitly runs `aginctus herdr authorize dev` to inspect the existing Herdr client public identity and install matching authorization; ordinary workload `apply` does not do this automatically. See [Herdr SSH identity contract](../ssh-credentials.md) for the public-key handoff and [Herdr operations](../herdr-machine-registration.md) for host trust and registration.

The Herdr headless server starts at boot as the `agent` user with persistent state under `/var/lib/herdr` and runtime state under `/run/herdr`.

OpenCode is installed for interactive use by Herdr panes or direct workload sessions. It is not started as a system service.

## Model access

The image contains no model-provider credentials.

OpenCode uses an operator-approved, LiteLLM-scoped workload key delivered by the explicit `aginctus workload gateway provision` command below. Aginctus does not itself issue or authorize the key; provider credentials remain inside the gateway and never enter the workload image.

The image invokes an Aginctus OpenCode launcher from Herdr and interactive shells. It reads **runtime** files (not Nix store values):

- `/var/lib/aginctus/opencode/gateway.json`: `{"endpoint":"http://<private-litellm-dns>:4000/v1","model":"<approved-alias>","protocol":"responses"}`.
- `/var/lib/aginctus/opencode/workload-key`: shared v0.1 LiteLLM **workload** key, readable by `agent` with mode `0600`. This must not be the LiteLLM master key or any upstream/OAuth token.

**Do not copy placeholder names literally.** The host CLI discovers the one owned, running LiteLLM instance's private management IPv4 and provisions that numeric endpoint, an approved alias and a protected workload key through fixed guest-helper stdin. Do not assume cross-project Incus DNS works, hand-edit a key with shell arguments, or print credential contents. The separate runtime key still must be issued and scoped by an authorized LiteLLM operator; provisioning does not prove reachability.

`gateway.json` may be world-readable (no secrets); `workload-key` must remain a private regular file. The runtime exports the workload key only in the child OpenCode process environment and uses OpenCode's `{env:...}` API-key interpolation. No secret is serialized into `OPENCODE_CONFIG_CONTENT`, a rendered Nix derivation, a command-line argument or OpenCode's configured provider JSON. The launcher defaults to the OpenAI Responses adapter (`@ai-sdk/openai`). Specify `"protocol":"chat-completions"` only for an explicitly validated LiteLLM model which requires `@ai-sdk/openai-compatible`.

For the opt-in Go pilot alias `aginctus-go-pilot` on Responses, the
launcher registers a *local, Nix-packaged* OpenCode plugin via a `file://` URL
in `OPENCODE_CONFIG_CONTENT`. The pinned OpenCode v1.15.11 custom provider
otherwise emits `x-session-affinity` but not the Go-required
`x-opencode-session`. Its supported `chat.headers` hook gives this plugin
the actual `sessionID` of each request. The plugin forwards that identifier
unchanged only when provider `aginctus` and model `aginctus-go-pilot`
match; absent/malformed session IDs fail closed. It never fabricates an ID,
overrides the user agent, or reads credentials. Other model aliases do not
load the plugin. This completes only the **client-to-gateway** session-header
leg; #456 still requires native pinned LiteLLM Responses forwarding, upstream
header exposure assessment, and operator-approved live provider validation.

If the endpoint, model or key is absent/invalid, OpenCode does **not** launch. An incorrect DNS name or an unreachable gateway fails requests without making direct upstream network connectivity available. The management network stays unrouted and without NAT.

## Isolated validation (does not require Incus or LiteLLM)

```sh
python3 -m unittest discover -s guest/opencode -p 'test_*.py' -v
nix develop -c go test ./...
nix flake check
nix build .#opencode-workload --no-link
```

The unit tests use a fake OpenCode executable and a synthetic tool-call response to exercise launcher mechanics. Additionally, exercise the actual **flake-pinned** OpenCode binary (not the pilot guest) against a synthetic streaming SSE Responses server:

```sh
OPENCODE_PATH="$(nix build .#opencode-runtime --no-link --print-out-paths)/bin/opencode"
nix shell nixpkgs#python3 --command python3 guest/opencode/test_real_runtime.py "$OPENCODE_PATH"
```

The separate integration test runs the actual pinned CLI through the Go-alias session plugin and launcher in an isolated temporary HOME, with a disposable loopback mock. It asserts an authenticated `POST /v1/responses`, the configured Go pilot alias, an identifying `User-Agent`, a real OpenCode session header (`x-opencode-session` or `x-opencode-session-id`), continuity across requests in that run, and terminal streamed text consumption; no external model or gateway is needed. It never prints session identifiers or bearer keys. This is an **operator validation step**, not a claim that it passed. If the pinned OpenCode binary lacks those headers, the offline test fails rather than having Aginctus silently fabricate a session. A run with one request verifies header presence only; repeatability across a multi-request conversation and upstream forwarding remain separate acceptance gates.

This test does **not** establish LiteLLM forwarding or model-driven filesystem tools (see #172). In pinned LiteLLM v1.103.0, client `x-*` forwarding is disabled by default and the model-scoped forwarding setting admits all eligible `x-*` headers, not solely the OpenCode session header. Do not enable global header forwarding or relay client credentials in order to satisfy the Go provider. Selective header forwarding and upstream `User-Agent` preservation require a separate, verified implementation before another paid OpenCode Go pilot request.

## Final pilot-host validation (after #122, #172 and #220)

```sh
aginctus config get incus.management.network.name
aginctus config get gateway.compose.project
aginctus gateway status
aginctus workload dev status
incus list --project default
incus list --project "$(aginctus config get gateway.compose.project | tr -d '"')"
# From inside the workload, verify the discovered LiteLLM DNS name resolves:
incus exec aginctus-dev --project default -- getent hosts '<private-litellm-dns>'
# Verify the runtime credential is provisioned with restrictive permissions
# without displaying its contents:
incus exec aginctus-dev --project default -- stat -c '%a %U:%G' /var/lib/aginctus/opencode/workload-key
# Check that the agent user can select the configured gateway model:
incus exec aginctus-dev --project default -- runuser -u agent -- opencode models aginctus
```

Then use the existing Herdr client to create a disposable remote workspace, start an OpenCode agent, prompt it to create a marker file under the **guest-local** workspace, verify the file content, terminate the agent and remove the temporary workspace. Inspect gateway access logs for an authenticated LiteLLM request without logging secrets. Test a deliberately invalid key and temporarily unreachable private endpoint: both must fail without fallback to a public provider. Confirm `incus network show "$(aginctus config get incus.management.network.name | tr -d '"')"` still reports `ipv4.nat=false` and `ipv4.routing=false` and that the agent cannot directly reach an external provider.

**Remaining acceptance:** #220's firewall integration is implemented, but the deployed host's actual network path must still be verified. #122 requires approved upstream inference, #123 a real managed OpenCode→LiteLLM request, #124 the repeatable Herdr-managed coding task, and #172 the native OAuth Responses/tool-event contract where applicable. Provisioning is implemented; actual key validity, model grants and live traffic must be proven separately.

## Provision private gateway access (#123)

Once LiteLLM is deployed and an operator has approved the shared v0.1 model
scope, create a **workload-facing LiteLLM key** in a local, mode-0600 file. This
must not be the LiteLLM master key or any upstream/OAuth token. Aginctus does
not generate or authorize that key: it must already be valid at LiteLLM.

```sh
# Substitute the actual operator-approved model alias and private key file:
aginctus workload gateway provision dev --model=APPROVED_ALIAS --key-file=/secure/path/workload-key
```

The CLI verifies ownership and running state of the target workload and gateway
Compose project. It inspects the single owned running LiteLLM service, requires
**one unambiguous private IPv4 address**, and derives the
`http://<discovered-ip>:4000/v1` endpoint. It does not assume a cross-project
Incus DNS name. The service port 4000 is the v0.1 LiteLLM default; deployments
using another port are intentionally unsupported by this initial contract.

The host reads the protected key file, then transmits a fixed JSON payload
through **Incus exec stdin** to the image's privileged
`aginctus-opencode-gateway-provision` helper. No credential enters command
arguments, Aginctus configuration overrides, Nix store paths, or the CLI's
stdout/stderr. The guest installs the key and settings atomically in private
agent-owned files beneath `/var/lib/aginctus/opencode`. Existing credentials
are replaced on explicit invocation; coordinate shared-key rotation with the
LiteLLM administrator. The key remains readable to the trusted workload
`agent` user by design, so per-workload isolation remains a v0.2 task (#177).

This only configures OpenCode. A successful command **does not** prove that
LiteLLM accepts the key, routes the chosen model, or is reachable from the
guest. On a deployment with multiple plausible service IPv4 addresses, the
command fails closed instead of guessing. Never enable management-network
NAT/routing to work around a connectivity failure.

### Real Incus test without LiteLLM

The acceptance harness launches an **offline, disposable** NixOS OpenCode
instance without profiles or NICs, streams a generated throwaway credential to
the real guest helper, checks installed file ownership/permissions and safe
settings, exercises invalid-input rejection, then deletes only its own test
instance. It does not use the production `aginctus-dev` instance or an
operational gateway. Run only on an Incus host after importing the rebuilt
OpenCode image containing the provisioning helper:

```sh
just build opencode-workload
just update image opencode-workload
nix shell nixpkgs#python3 --command python3 \
  scripts/acceptance/opencode-guest-provision.py --pool default
```

Replace `default` with your configured Incus storage pool. Inspect any
cleanup warning before manually removing the named disposable test instance.

### Independent validation

```sh
nix shell nixpkgs#python3 --command python3 -m unittest discover -s guest/opencode -p 'test_*.py' -v
nix develop -c go test ./internal/cli ./...
nix flake check
nix build .#opencode-workload --no-link
git diff --check origin/main...HEAD
```

For pilot-host validation, invoke the provisioning command with a valid LiteLLM
key and approved model, verify the key file's owner/mode using `stat` (without
printing contents), verify `opencode models aginctus`, and run an actual
Herdr model prompt against the private endpoint. Revoke the test key afterward.
This remains dependent on live gateway readiness (#122), Responses/tool
compatibility (#172), and management connectivity (#220).

## Lifecycle

Run `aginctus workload dev ensure` to reconcile the configured management network and workload instance through the Apply driver, or `aginctus workload dev teardown` to delete only the workload instance. Use `--dry-run` to inspect actions, and see [workload lifecycle](lifecycle.md) for ownership and isolation rules.

Workload instance `ensure` does **not automatically** provision credentials or establish runtime readiness. Use the separate, explicit `aginctus workload gateway provision` command after approving the scoped key; the image remains independent of host-specific values.

## VM compatibility

The image built here is for Incus **containers**, not virtual machines.
Changing `workloads.dev.isolation` to `vm` without replacing
`workloads.dev.image.alias` with a VM-compatible Incus image will fail.
For alternate images and supported isolation combinations, see
[Workload lifecycle](lifecycle.md#isolation-and-image-compatibility).
