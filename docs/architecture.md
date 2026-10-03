# Architecture

Aginctus is an orchestration layer for building and operating isolated agentic environments on Incus.

The architecture should be driven by a small usable vertical slice before expanding into the full control-plane vision.

## First target

The first target is intentionally narrow:

> Run a dedicated Herdr client inside an Aginctus-managed infrastructure container and use it to connect over SSH to Herdr servers running inside Aginctus-managed agent workloads.

The human starts the console from the Incus host with an Aginctus command such as `aginctus herdr`. Aginctus uses `incus exec` to enter the Herdr client container and launch the client there.

Each agent workload owns its own Herdr server, panes, and agent processes. The Herdr client container is presentation and control infrastructure; it does not host the agent workloads themselves.

This target forces the project to solve useful platform concerns early:

- instance creation and lifecycle;
- infrastructure workloads versus agent workloads;
- guest bootstrap;
- an internal management network;
- SSH identity and trust bootstrap;
- service discovery and generated Herdr machine configuration;
- container and VM targets behind the same connection model;
- ownership and cleanup of Incus resources;
- separation between portable workload intent and Incus-specific implementation.

## Initial component boundaries

### Aginctus CLI

The first user-facing control surface will be an `aginctus` CLI running on the Incus host.

Initially, the CLI may be a thin orchestration layer over Incus. It should not merely proxy arbitrary `incus` commands: Aginctus commands should operate on Aginctus concepts and preserve Aginctus invariants.

For the first milestone, `aginctus herdr` should enter the managed Herdr client container with `incus exec` and launch or attach to the Herdr client.

The CLI is expected to provide the first executable interface to orchestration logic that can later be shared with an API service.

### Orchestration core

The orchestration layer owns the translation from Aginctus configuration into concrete operations.

Its responsibilities include:

- validating configuration;
- creating or reconciling Incus resources;
- bootstrapping infrastructure and agent workloads;
- provisioning management-network connectivity;
- managing SSH client identity and target authorization;
- discovering runtime state and workload addresses;
- generating Herdr machine configuration;
- reporting status and errors;
- destroying resources owned by Aginctus.

The orchestration layer should not expose Incus implementation details in the public workload model unless they are intentionally part of the Aginctus contract.

### Incus adapter

Direct interaction with Incus belongs behind a narrow adapter.

The adapter is responsible for operations such as:

- creating, starting, stopping, and deleting instances;
- selecting container or VM execution;
- attaching networks and storage;
- reading instance state and addresses;
- applying Aginctus ownership metadata;
- performing `incus exec` operations needed by higher-level workflows.

This boundary exists so higher-level configuration remains portable and testable.

### Guest bootstrap

Guest bootstrap configures the software required inside an instance.

Shared orchestration must not assume a particular package manager, init system, or filesystem layout.

NixOS is the first-class initial guest path, but the bootstrap contract should allow other Linux distributions to implement the same requirements.

The first milestone has two guest roles.

#### Herdr client infrastructure

The Herdr client container needs:

- Herdr installed;
- an Aginctus-managed SSH client identity;
- generated SSH host-trust data;
- generated Herdr machine configuration for managed agent workloads.

#### Agent workload

Each agent workload needs:

- an SSH server reachable on the management network;
- the Aginctus Herdr-client public key authorized for the intended login account;
- Herdr installed and available to the SSH session;
- the agent runtime and dependencies required by that workload.

### Workload model

A workload describes desired behavior rather than Incus machinery.

The initial model should be able to express:

- workload name;
- role or runtime;
- isolation mode: `container` or `vm`;
- guest image or guest profile;
- resource requirements;
- workspace or mounted data;
- management access;
- lifecycle intent.

Infrastructure workloads such as the Herdr client are Aginctus-managed resources but should remain distinguishable from agent workloads.

Herdr is the first frontend we will exercise, but Herdr-specific concepts should not become generic platform concepts without a clear need.

## Management network

Aginctus should create or select an internal management network for control-plane communication between infrastructure workloads and agent workloads.

For the first milestone:

1. the Herdr client container joins the management network;
2. each managed agent workload joins the management network;
3. the Herdr client can reach the SSH service of each agent workload;
4. agent workloads do not require public SSH exposure;
5. management connectivity does not imply unrestricted workload-to-workload trust;
6. workload internet egress remains a separate policy concern.

The initial implementation may use an Incus-managed bridge. Address assignment is an implementation detail; Aginctus should discover realized addresses and generate client configuration from them.

As policy support matures, Incus ACLs or equivalent controls should restrict the management network so the Herdr client has the access it needs without turning it into a generally trusted flat network.

## SSH identity and trust

SSH is the first management transport between the Herdr client tier and agent workloads.

Aginctus should manage this path explicitly rather than relying on ambient user SSH state.

The initial design should include:

- an Aginctus-managed SSH keypair for the Herdr client environment;
- the private key present only in the Herdr client container and Aginctus-controlled secret material;
- the public key provisioned only to agent workloads intended to be reachable from that client;
- host-key verification enabled;
- generated `known_hosts` or equivalent trust data for managed targets;
- predictable key rotation and cleanup behavior.

A single deployment-scoped keypair is sufficient for the first milestone. Per-workload keys or an SSH CA may be introduced later if they materially improve isolation or operations.

The human host does not need direct SSH credentials for agent workloads in order to use Herdr.

## Herdr machine discovery

Aginctus owns the mapping between managed agent workloads and the machine entries consumed by the Herdr client.

When agent workloads are created, removed, or addressed differently, Aginctus should reconcile the Herdr client configuration accordingly.

A Herdr machine entry should be derived from Aginctus runtime state rather than requiring users to manually copy IP addresses or edit SSH targets.

## Resource ownership

Aginctus must be able to distinguish resources it owns from unrelated Incus resources.

Created resources should carry stable ownership metadata sufficient to:

- map an Incus object back to an Aginctus deployment and workload;
- distinguish infrastructure from agent workloads;
- discover existing resources during reconciliation;
- avoid deleting unrelated resources;
- make cleanup predictable.

The exact naming and metadata schema can be defined alongside the CLI implementation.

## Desired state and runtime state

Configuration describes desired state. Incus and the guests provide runtime state.

Aginctus should not require its own persistent database for the first milestone unless implementation experience demonstrates a need for one. Configuration plus discoverable Incus metadata should be sufficient to bootstrap the initial CLI.

This keeps the first implementation small while preserving a path toward a daemon or API-backed control plane later.

## Initial configuration shape

The configuration format is not yet stable, but the first milestone should be expressible without Incus-specific plumbing leaking into the manifest.

For example:

```yaml
infrastructure:
  herdr:
    isolation: container
    guest:
      distribution: nixos

workloads:
  dev:
    isolation: container
    guest:
      distribution: nixos
    runtime:
      type: opencode
    console:
      herdr: true

  research:
    isolation: vm
    guest:
      distribution: nixos
    runtime:
      type: hermes
    console:
      herdr: true
```

Aginctus would derive the management network, SSH authorization, Herdr server setup, and Herdr machine entries from this intent.

These examples are illustrative contracts, not a committed schema.

## Deferred decisions

The first architectural slice does not need to settle:

- implementation language;
- persistence database;
- daemon or API framework;
- dashboard technology;
- full model-gateway design;
- full MCP policy model;
- general internet-egress policy;
- final management-network policy implementation;
- SSH CA versus managed keypairs;
- multi-host scheduling;
- cluster management;
- a stable public configuration schema.

Those decisions should be made when a concrete milestone requires them.
