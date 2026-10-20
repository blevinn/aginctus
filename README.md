# Aginctus

Aginctus is a portable, configurable agentic lab built on [Incus](https://linuxcontainers.org/incus/). It provides a control plane for agent workloads with explicit networking, credential, and lifecycle controls.

## Project status

Aginctus is in **v0.1 hardening, with live pilot acceptance incomplete**. Available implementation includes:

- a multi-command Go CLI (`aginctus apply` / `remove` / `status` / `network ...` / `workload ...` / `gateway ...` / `herdr ...` / `images ...` / `doctor` / `config ...`);
- a NixOS module (`services.aginctus`) with `nix flake check` evaluation and three opt-in firewall VM tests;
- three guest images: Herdr client, OpenCode workload, and an opt-in dedicated OAuth daemon image (deferred for v0.1 activation; see [architecture decision](docs/architecture/native-chatgpt-oauth.md));
- acceptance tooling for firewall reconciliation, OpenCode provisioning, and Herdr lifecycle phases, including an opt-in live dispatcher that requires operator-supplied hooks.

Hermetic tests, disposable firewall VM tests, and recorded offline real-Incus provisioning checks establish component behavior, not a successful live deployment. [Release acceptance #118](https://github.com/blevinn/aginctus/issues/118) still requires pilot evidence: [#257](https://github.com/blevinn/aginctus/issues/257) owns LiteLLM egress migration and isolation proof; [#122](https://github.com/blevinn/aginctus/issues/122) owns authenticated upstream inference and is on operator hold pending egress verification; [#123](https://github.com/blevinn/aginctus/issues/123) owns managed OpenCode-to-gateway inference; [#124](https://github.com/blevinn/aginctus/issues/124) owns the repeatable end-to-end coding task. Code merges, health checks, and synthetic responses do not complete these gates or establish production readiness.

## Quickstart

```sh
nix develop                  # reproducible Go + Python + Nix tooling
go test ./...                # Go unit tests
nix flake check              # NixOS module + flake checks
nix build .#aginctus         # build the CLI

# Or on a NixOS host, import the module:
#   services.aginctus = { enable = true; package = aginctus; settings = {...}; };
# See docs/configuration.md for the full host example and current threat model.

```

See the [development guide](docs/development.md) for prerequisites, direnv setup, formatting, and Incus development notes. For consuming the Aginctus flake as a NixOS module, see the [NixOS configuration and integration guide](docs/configuration.md#nixos-module), including a host example and validation commands.

## Documentation

Project documentation lives in [docs/](docs/README.md). See the [security controls and limitations](docs/security.md) for implemented boundaries, validation scope, and authoritative pilot acceptance links. This summary is not a completed threat model or security audit.

## Development

Enter the reproducible development environment with:

```sh
nix develop
```

Automated coding agents must also follow [AGENTS.md](AGENTS.md).
