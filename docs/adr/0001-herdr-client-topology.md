# ADR 0001: Use a dedicated Herdr client container with SSH-managed targets

- Status: Accepted
- Date: 2026-10-03

## Context

Herdr's multi-machine model places a Herdr server on each machine that owns panes and agent processes. A Herdr client can connect to multiple machines and present them through one interface.

Aginctus needs a first topology that supports multiple isolated agent workloads, works for both containers and VMs, and does not require the Incus host itself to hold direct SSH credentials for every workload.

## Decision

Aginctus will provision a dedicated Herdr client container as infrastructure.

The Incus host will enter that container with `incus exec` when the user invokes the Herdr console.

Each agent workload will:

- run its own Herdr server;
- expose SSH only on an Aginctus-managed management path unless explicitly configured otherwise;
- authorize an Aginctus-managed Herdr-client SSH identity.

The Herdr client container will:

- hold the corresponding SSH private key;
- maintain generated host-trust data;
- maintain generated Herdr machine configuration for managed agent workloads;
- connect to each target over SSH.

The initial SSH identity may be deployment-scoped. More granular keys or SSH certificates may be added later.

## Consequences

- Herdr's topology matches the isolation boundary: each agent workload owns its own server and processes.
- One Herdr client can aggregate multiple managed workloads.
- Containers and VMs use the same Herdr connection model.
- The Incus host does not need direct SSH access to agent workloads for normal console use.
- Aginctus must manage SSH identity, authorized keys, host-key verification, address discovery, and Herdr machine configuration.
- The Herdr client container becomes security-sensitive infrastructure because its SSH identity can control managed workloads.
- The management network must be designed so SSH reachability does not imply unrestricted workload-to-workload trust.
