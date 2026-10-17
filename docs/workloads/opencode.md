# OpenCode workload

Status: OpenCode image, lifecycle and fail-closed gateway launcher implemented; credential provisioning, service discovery and on-host inference remain integration work.

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
just opencode-workload-image-update
```

The image is imported as:

```text
aginctus-opencode-workload
```

## Guest behavior

The image enables `sshd` but disables password and keyboard-interactive authentication. Root SSH login is disabled.

No SSH public key is baked into the image. The image exposes a fixed `aginctus-ssh-authorize` helper and a dedicated root-owned authorization path for Aginctus-managed client keys; host-side orchestration still needs to deliver and reconcile those keys. See [SSH bootstrap workflow](../ssh-bootstrap-workflow.md) for the exact client-helper → host → workload-helper handoff.

The Herdr headless server starts at boot as the `agent` user with persistent state under `/var/lib/herdr` and runtime state under `/run/herdr`.

OpenCode is installed for interactive use by Herdr panes or direct workload sessions. It is not started as a system service.

## Model access

The image contains no model-provider credentials.

The intended path is for OpenCode to use a workload credential issued by the Aginctus AI gateway. Provider credentials remain in the gateway and never enter the workload image.

The image invokes an Aginctus OpenCode launcher from Herdr and interactive shells. It reads **runtime** files (not Nix store values):

- `/var/lib/aginctus/opencode/gateway.json`: `{"endpoint":"http://<private-litellm-dns>:4000/v1","model":"<approved-alias>","protocol":"responses"}`.
- `/var/lib/aginctus/opencode/workload-key`: shared v0.1 LiteLLM **workload** key, readable by `agent` with mode `0600`. This must not be the LiteLLM master key or any upstream/OAuth token.

**Do not copy the placeholder names literally.** Obtain the DNS name/port from the actual Compose service discovery in the configured management network, and the alias from the approved LiteLLM model registration. Project-scoped Incus DNS names may not resolve from the default-project workload; prove resolution rather than assuming the Compose service name is reachable. Until a safe credential-delivery interface is implemented, provision these files only through a verified operator-controlled private channel, never via `incus exec ... -- sh -c 'echo SECRET ...'` or environment overrides containing secrets.

`gateway.json` may be world-readable (no secrets); `workload-key` must remain a private regular file. The runtime exports the workload key only in the child OpenCode process environment and uses OpenCode's `{env:...}` API-key interpolation. No secret is serialized into `OPENCODE_CONFIG_CONTENT`, a rendered Nix derivation, a command-line argument or OpenCode's configured provider JSON. The launcher defaults to the OpenAI Responses adapter (`@ai-sdk/openai`). Specify `"protocol":"chat-completions"` only for an explicitly validated LiteLLM model which requires `@ai-sdk/openai-compatible`.

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

The separate integration test runs the actual pinned CLI via the launcher in an isolated temporary HOME, with a disposable loopback mock. It asserts an authenticated `POST /v1/responses`, the configured model, and terminal streamed text consumption; no external model or gateway is needed. This is an **operator validation step**, not a claim that it passed. The test does not yet prove real LiteLLM interoperability or model-driven filesystem tools (see #172).

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

**Remaining dependencies:** #122 supplies operational LiteLLM and tested inference; #172 must prove LiteLLM's Responses streaming and tool event contract for OAuth models; #220 ensures host-managed DNS/DHCP reachability. Workload-key delivery and approval of its model scope must be implemented/verified before #123 closes.

## Lifecycle

Run `aginctus workload dev ensure` to reconcile the configured management network and workload instance through the Apply driver, or `aginctus workload dev teardown` to delete only the workload instance. Use `--dry-run` to inspect actions, and see [workload lifecycle](lifecycle.md) for ownership and isolation rules.

This lifecycle does not yet provision gateway workload credentials or complete runtime-specific readiness. The image remains independent of host-specific values.

## VM compatibility

The image built here is for Incus **containers**, not virtual machines.
Changing `workloads.dev.isolation` to `vm` without replacing
`workloads.dev.image.alias` with a VM-compatible Incus image will fail.
For alternate images and supported isolation combinations, see
[Workload lifecycle](lifecycle.md#isolation-and-image-compatibility).
