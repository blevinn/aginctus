# Gateway egress and hosted-provider inference (issue #122)

This design introduces a **separate owned egress bridge** without changing the
default `aginctus-mgmt` bridge's `ipv4.nat=false` and
`ipv4.routing=false` isolation.

## Topology

- `aginctus-mgmt` retains Herdr, OpenCode, LiteLLM, and PostgreSQL on the
  existing private control network.
- `aginctus-egress` is a second Aginctus-owned Incus bridge. It uses an
  Incus-selected IPv4 subnet (`ipv4.address=auto`) with IPv4 NAT and
  forwarding enabled, and IPv6 disabled.
- **Only LiteLLM** is dual-homed. PostgreSQL remains on the management
  network. The Herdr client and agent workloads retain their existing NICs.
- The orchestrator requires `user.aginctus.managed=true` and
  `user.aginctus.resource=gateway-egress-network` on any existing bridge
  with the configured name. Foreign bridges are not adopted.

The egress bridge provides outbound NAT, **not** a provider-specific firewall
allowlist. Narrow L3/L4/L7 restrictions are deferred to the network-policy
workstream in v0.2. The host firewall must permit the bridge's NAT path;
successful bridge creation alone is not proof of upstream reachability.

## Provider model and credentials

The initial provider implementation supports an OpenAI-compatible model with
identifier `openai/<model>`. The default is `openai/gpt-4o-mini`. This
**public** configuration is set via
`gateway.provider.model`; the LiteLLM-facing name is
`aginctus-default`. Configuring other provider families is follow-up work.

Set `AGINCTUS_GATEWAY_UPSTREAM_API_KEY` in the trusted **gateway
deployment process environment**, *not* in Aginctus configuration, a Nix
module, a Git checkout, or command-line `--config` flags. The gateway
requires this value before applying orchestration. The key is injected only as
the gateway LiteLLM container's `OPENAI_API_KEY` runtime environment value,
so it is visible to privileged Incus/container administrators and should be
treated accordingly. It is **not persisted** in
`~/.local/state/aginctus/gateway/<id>/secrets.json` or added to Jsonnet;
the three internally generated secrets retain the existing lifecycle.

LiteLLM writes a minimal runtime config inside the guest with an
`os.environ/OPENAI_API_KEY` reference, never the literal credential. This
avoids embedding it in Compose files or images. The configured model is
syntactically validated before interpolation.

## Dry review and staged validation

On the PR branch run:

```sh
nix develop -c go test ./...
nix flake check
nix develop -c go run ./cmd/aginctus gateway validate
nix develop -c go run ./cmd/aginctus gateway render
```

Review that the rendered Compose file has a dual-network LiteLLM service and a
management-only PostgreSQL service; verify no literal upstream credentials.

Only on an approved test host with an upstream API key already available in a
protected environment should the operator run:

```sh
nix develop -c go run ./cmd/aginctus gateway apply
nix develop -c go run ./cmd/aginctus gateway status
incus network show aginctus-egress
```

**Do not paste provider credentials into terminal transcripts or chats.**
After verifying the provider connection and resolving LiteLLM's **private**
management address, use an authorized master key from the existing protected
gateway state to make a `/v1/chat/completions` request for
`model=aginctus-default`. Retrieve secrets locally with masking; never print
or log them. Test both valid and invalid credentials, check that the response
is an actual model reply, and examine LiteLLM/Incus status if the request
fails. At present the CLI's `gateway status` reports **instance state only**,
not application readiness; a real inference response is the acceptance test.

Note: whether `incus-compose` honors shell-style multiline commands and
the initial LiteLLM container's startup assumptions is a **required real-Incus
smoke test**, not a property established by plan-level tests.

## Scope boundaries

No public ports are published by Compose. No SSH keys are modified.
OpenCode model routing and scoped per-workload gateway credentials belong to
issue #123. General multi-provider configuration and destination-specific
network restrictions are outside this PR.
