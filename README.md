# Aginctus

Aginctus is a portable, configurable agentic lab built on [Incus](https://linuxcontainers.org/incus/). It provides a secure, observable control plane for running sandboxed agentic systems in containers or virtual machines, with explicit controls for model access, networking, MCP access, and workload lifecycle.

## Project status

Aginctus is in **v0.1 hardening**. The CLI, the NixOS module, the three guest images, and the pilot validation harnesses are shipping; this release is the result of a full-repo code-review inventory tracked in [issue #249](https://github.com/blevinn/aginctus/issues/249). The shipping surface includes:

- a multi-command Go CLI (`aginctus apply` / `remove` / `status` / `network ...` / `workload ...` / `gateway ...` / `herdr ...` / `images ...` / `doctor` / `config ...`);
- a NixOS module (`services.aginctus`) with `nix flake check` evaluation and three opt-in firewall VM tests;
- three guest images: Herdr client, OpenCode workload, and an opt-in dedicated OAuth daemon image (deferred for v0.1 activation; see [architecture decision](docs/architecture/native-chatgpt-oauth.md));
- a full acceptance harness covering host firewall reconciliation, OpenCode guest provisioning, and Herdr lifecycle phases.

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

Project documentation lives in [docs/](docs/README.md). The current v0.1 threat model and pilot-validation records are in [docs/security/threat-model.md](docs/security/threat-model.md).

## Development

Enter the reproducible development environment with:

```sh
nix develop
```

Automated coding agents must also follow [AGENTS.md](AGENTS.md).
