# Aginctus

Aginctus is a portable, configurable agentic lab built on [Incus](https://linuxcontainers.org/incus/).

The project aims to provide a secure, observable control plane for running agentic systems in isolated containers or virtual machines, with first-class controls for model access, networking, MCP access, and workload lifecycle.

## Vision

Aginctus should make it practical to stand up and operate a reproducible agentic environment without tying the deployment to one guest Linux distribution, one agent runtime, or one model provider.

The initial direction is:

- use Incus as the execution substrate;
- support agentic workloads in either system containers or virtual machines;
- keep guest environments distribution-agnostic;
- support NixOS as a first-class guest from day one;
- support OpenCode and Hermes as initial agent runtimes;
- provide an AI gateway, likely LiteLLM, behind an internal abstraction;
- make network and MCP access explicit, configurable policy surfaces;
- expose workload and infrastructure state through an API;
- provide a dashboard UI over that API;
- support a frontend for interacting with deployed agentic systems, potentially using Herdr;
- keep the API suitable for future clients, including a TUI.

## Design principles

### API-first control plane

The Aginctus API is intended to be the canonical interface to the environment. The dashboard, future TUI, automation, and external integrations should consume the same API rather than relying on privileged shell access or directly coupling to Incus internals.

### Workload portability

A workload definition should describe what an agent needs rather than encode assumptions about one Linux distribution or one Incus instance type.

Container-versus-VM execution, guest operating system, model access, MCP access, networking, resources, mounts, and runtime selection should be configurable concerns.

### Strong isolation and explicit access

Agentic workloads should operate with least privilege. Network reachability, model credentials, MCP servers and tools, host access, and inter-workload communication should be explicit policy decisions rather than ambient capabilities.

### Replaceable integrations

Incus is the platform substrate, but higher-level integrations should sit behind clear boundaries. LiteLLM, Herdr, OpenCode, and Hermes are initial choices or candidates, not permanent constraints on the internal model.

### Observable by default

The system should expose enough desired state, runtime state, health, events, and policy information to make deployments understandable and debuggable through the API and dashboard.

## Initial architecture

Aginctus is expected to develop around several cooperating layers:

1. **Control plane** — desired state, lifecycle management, status, events, and the public Aginctus API.
2. **Incus adapter** — provisioning and management of containers, VMs, networks, storage, profiles, and related Incus resources.
3. **Workload model** — a portable definition for agent runtime, isolation mode, guest image, resources, workspace, and lifecycle.
4. **Policy plane** — model gateway policy, MCP policy, network policy, secrets, and capability grants.
5. **Agent adapters** — runtime-specific integration for systems such as OpenCode and Hermes.
6. **User interfaces** — the native dashboard, external frontends such as Herdr, and future clients such as a TUI.

This structure is intentionally provisional. Early design work should preserve these boundaries without prematurely fixing implementation language, persistence technology, or frontend framework.

## Initial scope

The first milestones should focus on proving a small end-to-end path:

- connect the control plane to Incus;
- describe an agent workload declaratively;
- provision and destroy an isolated workload;
- run one supported agent runtime through the configured model gateway;
- report lifecycle and status through the API;
- exercise both container and VM execution without creating separate product models for each;
- establish a clean NixOS path while retaining support for other Linux guests.

Network policy, MCP policy, additional runtimes, richer observability, dashboard functionality, and Herdr integration can then grow as focused increments.

## Non-goals for the initial phase

Aginctus does not initially need to:

- become a general-purpose replacement for Incus management tools;
- define a new agent protocol;
- require NixOS for every guest;
- hard-code LiteLLM, Herdr, OpenCode, or Hermes into its public workload model;
- support every agent runtime, Linux distribution, or model provider immediately;
- expose raw host privileges to agent workloads for convenience.

## Repository workflow

All substantive changes to this repository are made on topic branches and proposed through pull requests.

Only humans merge pull requests.

Agents may create and update topic branches, commits, and pull requests, but must follow the rules in [AGENTS.md](AGENTS.md), including the prohibition on force-pushing unless a human explicitly instructs them to do so.

## Status

Aginctus is at the project-definition stage. Interfaces and architecture are expected to evolve as the first end-to-end implementation is built.
