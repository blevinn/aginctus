# Architecture

Aginctus is an orchestration layer for building and operating isolated agentic environments on Incus.

The architecture should be driven by a small usable vertical slice before expanding into the full control-plane vision.

## First target

The first target is intentionally narrow:

> Run a Herdr client on the Incus host and attach it to a Herdr server running inside an Aginctus-managed Incus instance.

Herdr remote attach uses SSH as its transport. The remote machine owns the Herdr server, panes, and agent processes; the local client renders the UI. Aginctus therefore does not need to expose a Herdr-specific TCP service for this milestone.

This target forces the project to solve useful platform concerns early:

- instance creation and lifecycle;
- guest bootstrap;
- host-to-instance networking;
- SSH authentication and host verification;
- endpoint discovery;
- configuration generation;
- ownership and cleanup of Incus resources;
- separation between portable workload intent and Incus-specific implementation.

## Initial component boundaries

### Aginctus CLI

The first user-facing control surface will be an `aginctus` CLI running on the Incus host.

Initially, the CLI may be a thin orchestration layer over Incus. It should not merely proxy arbitrary `incus` commands, though: Aginctus commands should operate on Aginctus concepts and preserve Aginctus invariants.

The CLI is expected to provide the first executable interface to orchestration logic that can later be shared with an API service.

### Orchestration core

The orchestration layer owns the translation from Aginctus configuration into concrete operations.

Its responsibilities include:

- validating configuration;
- creating or reconciling Incus resources;
- bootstrapping guests;
- discovering runtime state;
- resolving host-accessible endpoints;
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
- performing Incus-specific discovery.

This boundary exists so higher-level configuration remains portable and testable.

### Guest bootstrap

Guest bootstrap configures the software required inside an instance.

Shared orchestration must not assume a particular package manager, init system, or filesystem layout.

NixOS is the first-class initial guest path, but the bootstrap contract should allow other Linux distributions to implement the same requirements.

For the first Herdr milestone, a guest must at minimum provide:

- a usable SSH server;
- an Aginctus-managed login path;
- Herdr installed and available to the remote session;
- enough runtime dependencies for the chosen agent workloads.

### Workload model

A workload describes desired behavior rather than Incus machinery.

The initial model should be able to express:

- workload name;
- isolation mode: `container` or `vm`;
- guest image or guest profile;
- resource requirements;
- bootstrap/runtime type;
- host-access requirements;
- workspace or mounted data;
- lifecycle intent.

Herdr is the first workload we will exercise, but Herdr-specific concepts should not become generic platform concepts without a clear need.

### Endpoint model

An endpoint describes a supported way to reach a workload from an authorized client.

For the first milestone, the important endpoint type is SSH from the Incus host to the workload.

An endpoint should describe intent such as:

- transport;
- target workload;
- target port or service;
- authorized source scope;
- identity or authentication reference.

It should not require callers to know how Incus assigned an address or implemented the network.

Future endpoint types may include HTTP APIs, model gateways, MCP gateways, or explicitly forwarded services.

## Networking model

The first milestone should use the simplest topology that preserves the intended trust boundary:

1. the Incus host can reach the managed instance;
2. the managed instance does not gain broad access to host services merely because the host can reach it;
3. no Herdr-specific port is exposed publicly;
4. SSH access is explicitly provisioned for the host-to-instance path;
5. external or inter-workload connectivity is not implicitly granted by the workload model.

A managed Incus bridge is a natural initial implementation because the host can communicate with instances on that network. Aginctus should discover the realized instance address rather than requiring it to be hard-coded.

Where later deployments need forwarding rather than direct host-to-instance reachability, the endpoint abstraction should allow an Incus proxy device or another implementation without changing the workload's higher-level intent.

## SSH and trust

Herdr remote attach depends on normal SSH connectivity, so SSH is part of the first platform contract rather than an incidental setup step.

Aginctus should not weaken SSH host verification for convenience.

The implementation should be designed around:

- a dedicated Aginctus-managed guest identity or explicit user-provided identity;
- deterministic authorized-key provisioning;
- host-key verification;
- Aginctus-owned SSH client state rather than silent mutation of the user's global SSH configuration where practical;
- clear rotation and cleanup behavior.

The exact key-management mechanism can be finalized during implementation, but insecure defaults such as disabling `StrictHostKeyChecking` are outside the intended design.

## Resource ownership

Aginctus must be able to distinguish resources it owns from unrelated Incus resources.

Created resources should carry stable ownership metadata sufficient to:

- map an Incus object back to an Aginctus workload;
- discover existing resources during reconciliation;
- avoid deleting unrelated resources;
- make cleanup predictable.

The exact naming and metadata schema can be defined alongside the CLI implementation.

## Desired state and runtime state

Configuration describes desired state. Incus and the guest provide runtime state.

Aginctus should not require its own persistent database for the first milestone unless implementation experience demonstrates a need for one. Configuration plus discoverable Incus metadata should be sufficient to bootstrap the initial CLI.

This keeps the first implementation small while preserving a path toward a daemon or API-backed control plane later.

## Initial configuration shape

The configuration format is not yet stable, but the first milestone should be expressible without Incus-specific plumbing leaking into the manifest.

For example:

```yaml
workloads:
  herdr:
    isolation: container
    guest:
      distribution: nixos
    runtime:
      type: herdr
    access:
      - name: ssh
        transport: ssh
        from: host
```

A VM variant should require only a change to the execution choice:

```yaml
workloads:
  herdr:
    isolation: vm
    guest:
      distribution: nixos
    runtime:
      type: herdr
    access:
      - name: ssh
        transport: ssh
        from: host
```

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
- multi-host scheduling;
- cluster management;
- a stable public configuration schema.

Those decisions should be made when a concrete milestone requires them.
