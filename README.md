# Aginctus

Aginctus is a portable, configurable agentic lab built on [Incus](https://linuxcontainers.org/incus/). It provides a control plane for agent workloads with explicit networking, credential, and lifecycle controls.

## Project status

Aginctus is in **v0.1 hardening, with live pilot acceptance incomplete**. Available implementation includes:

- a multi-command Go CLI (`aginctus apply` / `remove` / `status` / `network ...` / `workload ...` / `gateway ...` / `herdr ...` / `images ...` / `doctor` / `config ...`);
- a NixOS module (`services.aginctus`) with `nix flake check` evaluation and three opt-in firewall VM tests;
- three guest images: Herdr client, OpenCode workload, and an opt-in dedicated OAuth daemon image (deferred for v0.1 activation; see [architecture decision](docs/architecture/native-chatgpt-oauth.md));
- acceptance tooling for firewall reconciliation, OpenCode provisioning, and Herdr lifecycle phases, including an opt-in live dispatcher that requires operator-supplied hooks.

Hermetic tests, disposable firewall VM tests, and recorded offline real-Incus provisioning checks establish component behavior, not a successful live deployment. [Release acceptance #118](https://github.com/blevinn/aginctus/issues/118) still requires pilot evidence: [#257](https://github.com/blevinn/aginctus/issues/257) owns LiteLLM egress migration and isolation proof; [#122](https://github.com/blevinn/aginctus/issues/122) owns authenticated upstream inference and is on operator hold pending egress verification; [#123](https://github.com/blevinn/aginctus/issues/123) owns managed OpenCode-to-gateway inference; [#124](https://github.com/blevinn/aginctus/issues/124) owns the repeatable end-to-end coding task. Code merges, health checks, and synthetic responses do not complete these gates or establish production readiness.

## Build and inspect

```sh
nix develop                  # reproducible Go + Python + Nix tooling
go test ./...                # Go unit tests
nix flake check              # NixOS module + flake checks
nix build .#aginctus         # build the CLI

# Or on a NixOS host, import the module:
#   services.aginctus = { enable = true; package = aginctus; settings = {...}; };
# See docs/configuration.md for the full host example and current threat model.

```

These are development checks; they do **not** deploy an Incus environment or prove gateway inference. For the supported host prerequisites, NixOS activation side effects, read-only preflight and optional operator-approved deployment, start with [getting started](docs/getting-started.md). See [development](docs/development.md) for local tooling and [NixOS configuration](docs/configuration.md#nixos-module) for the full host example.

## Documentation

Read the [documentation index](docs/README.md) or jump to the [CLI operations guide](docs/cli.md), [architecture](docs/architecture.md), [configuration](docs/configuration.md) and [security controls and limitations](docs/security.md). Current implementation is not equivalent to live acceptance, a completed threat model or a security audit.

## Development

Enter the reproducible development environment with:

```sh
nix develop
```

Automated coding agents must also follow [AGENTS.md](AGENTS.md).
