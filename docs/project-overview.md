# Project overview

Aginctus is a portable, configurable agentic lab built on [Incus](https://linuxcontainers.org/incus/).

The project aims to make it practical to stand up and operate a reproducible agentic environment without tying the deployment to one guest Linux distribution, one agent runtime, or one model provider.

## Vision

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
- support a frontend for interacting with deployed agentic systems, initially using Herdr;
- keep the API suitable for future clients, including a TUI.

## Design principles

### API-first control plane

The Aginctus API is intended to become the canonical interface to the environment. The dashboard, future TUI, automation, and external integrations should consume the same domain operations rather than relying on privileged shell access or directly coupling to Incus internals.

The first implementation will be CLI-first so the orchestration model can be proven before an HTTP API or daemon is selected.

### Workload portability

A workload definition should describe what an agent needs rather than encode assumptions about one Linux distribution or one Incus instance type.

Container-versus-VM execution, guest operating system, model access, MCP access, networking, resources, mounts, and runtime selection should be configurable concerns.

### Strong isolation and explicit access

Agentic workloads should operate with least privilege. Network reachability, model credentials, MCP servers and tools, host access, and inter-workload communication should be explicit policy decisions rather than ambient capabilities.

### Replaceable integrations

Incus is the platform substrate, but higher-level integrations should sit behind clear boundaries. LiteLLM, Herdr, OpenCode, and Hermes are initial choices or candidates, not permanent constraints on the internal model.

### Observable by default

The system should expose enough desired state, runtime state, health, events, and policy information to make deployments understandable and debuggable through its control surfaces.

## Initial architecture

Aginctus is expected to develop around several cooperating layers:

1. **Control surface** — initially the `aginctus` CLI, later joined by the public Aginctus API.
2. **Orchestration core** — desired state, lifecycle management, discovery, generated configuration, status, and errors.
3. **Incus adapter** — provisioning and management of containers, VMs, networks, storage, profiles, and related Incus resources.
4. **Infrastructure workloads** — Aginctus-managed services such as the Herdr client environment.
5. **Agent workloads** — isolated containers or VMs that host agent runtimes and their local Herdr servers.
6. **Policy plane** — model gateway policy, MCP policy, network policy, secrets, and capability grants as those features are introduced.
7. **User interfaces** — Herdr initially, plus a future native dashboard and TUI.

This structure is intentionally provisional. Early work should preserve these boundaries without prematurely fixing implementation language, persistence technology, or frontend framework.

## Initial scope

The first milestone is a small end-to-end environment: run a dedicated Herdr client in an Aginctus-managed infrastructure container and use it to connect over SSH to Herdr servers running inside one or more Aginctus-managed agent workloads.

The Incus host enters the Herdr client container through `incus exec`; it does not need direct SSH access to every agent workload.

That milestone should prove:

- instance creation and lifecycle;
- infrastructure and agent workload roles;
- a first-class NixOS guest bootstrap path;
- an internal management network;
- SSH identity, target authorization, and host verification;
- workload address discovery;
- generated Herdr machine configuration;
- Herdr servers colocated with agent workloads;
- a common connection model for container and VM targets;
- resource ownership and cleanup;
- enough status reporting to diagnose the environment.

See [Milestone 1: Herdr console over managed workloads](milestones/0001-herdr-console.md) for the detailed success criteria.

Model gateways, MCP policy, richer network policy, the HTTP API, dashboard functionality, and multi-host operation can grow from the boundaries established by that vertical slice.

## Non-goals for the initial phase

Aginctus does not initially need to:

- become a general-purpose replacement for Incus management tools;
- define a new agent protocol;
- require NixOS for every guest;
- hard-code LiteLLM, Herdr, OpenCode, or Hermes into its public workload model;
- support every agent runtime, Linux distribution, or model provider immediately;
- expose raw host privileges to agent workloads for convenience.
