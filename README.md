# Aginctus

Aginctus is a portable, configurable agentic lab built on [Incus](https://linuxcontainers.org/incus/). It provides a control plane for agent workloads with explicit networking, credential, and lifecycle controls.

## Project status

Aginctus **v0.1** is the initial single-host, operator-controlled NixOS/Incus agent lab. Genuine Herdr/OpenCode coding and private LiteLLM upstream inference were observed in the live pilot. A full fresh-host NixOS deploy/smoke/cleanup/repeat was **not performed** and was explicitly deferred to [v0.2 #580](https://github.com/blevinn/aginctus/issues/580). Available functionality includes:

- a multi-command Go CLI (`aginctus apply` / `remove` / `status` / `network ...` / `workload ...` / `gateway ...` / `herdr ...` / `images ...` / `doctor` / `config ...`);
- a NixOS module (`services.aginctus`) with `nix flake check` evaluation and three opt-in firewall VM tests;
- two guest images: Herdr client and OpenCode workload; optional LiteLLM-native ChatGPT OAuth is gated v0.2 work (see [architecture decision and blockers](docs/architecture/native-chatgpt-oauth.md));
- acceptance tooling for firewall reconciliation, OpenCode provisioning, and Herdr lifecycle phases, including an opt-in live dispatcher that requires operator-supplied hooks.

Five functional criteria in [release acceptance #118](https://github.com/blevinn/aginctus/issues/118) have direct live pilot evidence: Herdr-managed OpenCode read/edited a broken fixture, ran real tests, completed, and used a private scoped LiteLLM gateway route to a real upstream provider. Independent key revocation and owned-resource cleanup were observed. The sixth, reproducible **fresh-host** test remains unchecked and is deferred to v0.2, not claimed as passing. The existing selective gateway configuration is under an explicit host reconciler hold; manual review is required before any convergence or upgrade. This is not an audited or production-hardened multi-tenant platform.

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

Read the [documentation index](docs/README.md) or jump to the [CLI operations guide](docs/cli.md), [architecture](docs/architecture.md), [configuration](docs/configuration.md) and [security controls and limitations](docs/security.md). Individual implemented capabilities, pilot acceptance and deployment reproducibility are distinct: see the [v0.1 release notes](docs/releases/v0.1.0.md) for verified evidence, explicit deferrals and security limitations.

## Development

Enter the reproducible development environment with:

```sh
nix develop
```

Automated coding agents must also follow [AGENTS.md](AGENTS.md).
