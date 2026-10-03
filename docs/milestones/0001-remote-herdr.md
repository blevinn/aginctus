# Milestone 1: local Herdr on Incus

The first Aginctus milestone is a deliberately small, usable environment that exercises orchestration and isolation before the project grows a larger control plane.

## Goal

From the Incus host, a user can interact with a Herdr server running inside an Aginctus-managed Incus container through a shared local Unix-domain socket.

The Herdr server, panes, and agent processes run inside the container. The client or host-side control tooling runs on the Incus host.

A successful milestone should make this path reproducible through Aginctus without requiring the user to inspect Incus internals or hand-wire a network path.

## Why this milestone

This path is useful on its own and forces Aginctus to solve foundational concerns:

- create and identify an owned Incus instance;
- configure a Linux guest;
- support NixOS as the first polished guest;
- install and run Herdr;
- create a workload-scoped runtime directory;
- share that directory safely with the container;
- configure and discover the Herdr socket;
- report useful runtime status;
- cleanly stop or remove the environment.

It avoids prematurely requiring an Aginctus daemon, dashboard, database, model gateway, MCP gateway, or SSH transport.

## Expected user flow

The exact CLI is intentionally deferred, but the eventual flow should resemble:

```text
aginctus up
aginctus status
aginctus herdr
```

The final command may launch a Herdr client against the shared socket or otherwise provide the appropriate local connection behavior. The important contract is that users should not need to locate the container filesystem or reconstruct Incus mount details manually.

## Success criteria

The milestone is complete when:

1. an Aginctus configuration can describe a Herdr workload;
2. Aginctus can create that workload as an Incus container;
3. the initial NixOS guest can boot with Herdr available;
4. Aginctus creates a workload-scoped runtime directory on the host;
5. the runtime directory is shared with the container with restrictive permissions;
6. Herdr is configured to create its Unix-domain socket in that shared directory;
7. host-side tooling can connect to the Herdr socket without a network transport;
8. Aginctus can report enough control-endpoint and lifecycle state to diagnose common failures;
9. Aginctus can remove resources it owns without deleting unrelated Incus resources.

Container execution is the first implementation target. VM execution remains a platform goal, but its control transport is intentionally deferred because VMs do not share the host kernel.

## Security boundary

For this milestone:

- the Herdr server and agent processes run inside the managed container;
- the Herdr socket is the explicit host-to-workload control path;
- access to the socket is treated as authority to control the Herdr session;
- each workload receives its own runtime directory;
- the shared directory should be narrowly scoped and restrictively permissioned;
- no public Herdr port or SSH service is required for control;
- guest-to-host access is not implicitly granted beyond the explicitly shared path.

Broader egress, MCP access, model credentials, network services, and workload-to-workload communication remain separate policy problems.

## Implementation sequence

This milestone is expected to span multiple pull requests.

### Architecture

Define the boundaries, shared-socket model, ownership model, and first workload contract.

### Development environment

Add the Nix flake and reproducible developer tooling needed to work on Aginctus.

### CLI bootstrap

Create the initial `aginctus` CLI and Incus integration boundary.

The CLI should begin with only the operations needed by this milestone rather than attempting to wrap the entire Incus command surface.

### Guest and Herdr bootstrap

Implement the NixOS guest path, shared runtime directory, Herdr installation, socket configuration, discovery, and host-side attach/control flow.

## Extension points

SSH is a natural future transport for:

- VMs;
- remote Incus hosts;
- deployments where shared filesystem IPC is unavailable;
- users who want Herdr's normal remote-attach workflow.

The control-endpoint abstraction should allow SSH or other transports to be added without redefining the workload model.

## Out of scope

The first milestone does not require:

- VM control transport;
- OpenCode or Hermes orchestration beyond what may be run manually inside Herdr;
- LiteLLM;
- MCP gateway policy;
- the Aginctus HTTP API;
- a dashboard;
- multi-host Incus clusters;
- public ingress to workloads;
- automatic internet-egress policy;
- a stable configuration schema.

Those features should build on the boundaries proven here rather than block the first working environment.
