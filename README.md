# Aginctus

Aginctus is a portable, configurable agentic lab built on [Incus](https://linuxcontainers.org/incus/). It is intended to provide a secure, observable control plane for running sandboxed agentic systems in containers or virtual machines, with explicit controls for model access, networking, MCP access, and workload lifecycle.

## Project status

Aginctus is in the early project-definition stage. Interfaces, architecture, configuration formats, and implementation choices are expected to evolve as the first end-to-end workloads are built.

The initial direction includes:

- Incus as the execution substrate;
- container and VM workloads behind a common workload model;
- Linux distribution neutrality with first-class NixOS support;
- OpenCode and Hermes as initial agent runtimes;
- an AI gateway, likely LiteLLM;
- explicit network and MCP policy;
- an API-first control plane with a dashboard and future TUI support;
- a frontend for interacting with deployed agents, potentially using Herdr.

See the [project overview](docs/project-overview.md) for more detail.

## Quickstart

> Placeholder: installation and first-run instructions will be added once the initial control-plane skeleton exists.

## Documentation

Project documentation lives in [docs/](docs/README.md).

## Development

> Placeholder: local development, testing, contribution, and repository workflow instructions will be added as the implementation takes shape.

Automated coding agents must also follow [AGENTS.md](AGENTS.md).
