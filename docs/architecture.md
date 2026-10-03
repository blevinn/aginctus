# Architecture

Aginctus is an orchestration layer for building and operating isolated agentic environments on Incus.

The architecture should be driven by a small usable vertical slice before expanding into the full control-plane vision.

## First target

The first target is intentionally narrow:

> Run a Herdr client on the Incus host and connect it to a Herdr server running inside an Aginctus-managed Incus container through a shared local socket.

Herdr exposes a local socket API. On Unix, the server listens on a Unix-domain socket, and the socket path can be overridden with `HERDR_SOCKET_PATH`.

For the first milestone, Aginctus can place that socket in an Aginctus-managed directory that is shared between the Incus host and the container. The Herdr server runs inside the container; the client or host-side tooling connects to the socket from the host.

This target forces the project to solve useful platform concerns early:

- instance creation and lifecycle;
- guest bootstrap;
- host/container filesystem sharing;
- socket ownership and permissions;
- configuration generation;
- discovery of workload control endpoints;
- ownership and cleanup of Incus resources;
- separation between portable workload intent and Incus-specific implementation.

It deliberately avoids making network reachability or SSH a prerequisite for the first usable environment.

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
- resolving host-accessible control endpoints;
- reporting status and errors;
- destroying resources owned by Aginctus.

The orchestration layer should not expose Incus implementation details in the public workload model unless they are intentionally part of the Aginctus contract.

### Incus adapter

Direct interaction with Incus belongs behind a narrow adapter.

The adapter is responsible for operations such as:

- creating, starting, stopping, and deleting instances;
- selecting container or VM execution;
- attaching storage and shared directories;
- attaching networks when a workload needs them;
- reading instance state;
- applying Aginctus ownership metadata;
- performing Incus-specific discovery.

This boundary exists so higher-level configuration remains portable and testable.

### Guest bootstrap

Guest bootstrap configures the software required inside an instance.

Shared orchestration must not assume a particular package manager, init system, or filesystem layout.

NixOS is the first-class initial guest path, but the bootstrap contract should allow other Linux distributions to implement the same requirements.

For the first Herdr milestone, a guest must at minimum provide:

- Herdr installed;
- a writable runtime directory for the Herdr socket;
- the expected runtime dependencies for the chosen agent workloads;
- a service or launch mechanism that starts Herdr with the Aginctus-provided socket path.

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

### Control endpoint model

A control endpoint describes a supported way for host-side Aginctus tooling to interact with a workload.

For the first milestone, the endpoint is a Unix-domain socket shared between the Incus host and a container.

An endpoint should describe intent such as:

- transport;
- target workload;
- host visibility;
- ownership and permissions;
- lifecycle relationship to the workload.

It should not require callers to know the underlying Incus mount implementation.

Future endpoint transports may include SSH, HTTP, vsock, forwarded TCP, or other mechanisms suitable for VMs and remote hosts.

## Local socket model

The first milestone should use the simplest transport that preserves isolation while avoiding unnecessary networking:

1. Aginctus creates a host-side runtime directory owned by the workload.
2. That directory is mounted into the container at a deterministic guest path.
3. Herdr is started with `HERDR_SOCKET_PATH` pointing inside that shared directory.
4. The Unix-domain socket is therefore visible from both the container and the host.
5. Host-side Aginctus tooling connects to that socket directly.
6. The shared directory is scoped to the workload and receives restrictive permissions.
7. No public port or host-to-guest network path is required for Herdr control.

Because containers and the host share the same kernel, a Unix-domain socket located on a shared filesystem can serve as a local IPC boundary.

This mechanism does not naturally extend to virtual machines, which have a separate kernel. VM support should use the same higher-level endpoint abstraction with a different transport.

## Networking model

Networking remains an explicit workload concern, but it is not required for the initial Herdr control path.

The platform should preserve these principles:

- workloads do not gain broad host access merely because they are managed by Aginctus;
- internet egress, workload-to-workload connectivity, and host-reachable services are explicit policy choices;
- no service should be publicly exposed merely to support host-side orchestration;
- network details should remain below the workload abstraction where possible.

SSH remains a useful future endpoint transport, especially for VMs, remote Incus hosts, and workflows where filesystem-backed local IPC is unavailable.

## Socket security

The shared socket directory is a security boundary.

The implementation should be designed around:

- one Aginctus-managed runtime directory per workload;
- restrictive host and guest permissions;
- no socket sharing between unrelated workloads;
- predictable ownership and cleanup;
- avoiding broad writable host mounts;
- treating possession of socket access as authority to control the Herdr session.

The exact UID/GID mapping and Incus mount options can be finalized during implementation.

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

Aginctus should not require its own persistent database for the first milestone unless implementation experience demonstrates a need for one. Configuration plus discoverable Incus metadata and runtime directories should be sufficient to bootstrap the initial CLI.

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
    control:
      transport: local-socket
      from: host
```

A future VM variant should preserve the same conceptual control endpoint while selecting a VM-capable transport:

```yaml
workloads:
  herdr:
    isolation: vm
    guest:
      distribution: nixos
    runtime:
      type: herdr
    control:
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
- VM control transport;
- multi-host scheduling;
- cluster management;
- a stable public configuration schema.

Those decisions should be made when a concrete milestone requires them.
