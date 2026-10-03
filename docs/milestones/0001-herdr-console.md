# Milestone 1: Herdr console over managed workloads

The first Aginctus milestone is a deliberately small, usable environment that exercises orchestration, identity, and management networking before the project grows a larger control plane.

## Goal

From the Incus host, a user can run:

```text
aginctus herdr
```

Aginctus enters a dedicated Herdr client container with `incus exec` and launches the Herdr client there.

That client connects over SSH to one or more Aginctus-managed agent workloads. Each workload runs its own Herdr server alongside its agent runtime.

## Why this milestone

This path is useful on its own and forces Aginctus to solve foundational concerns:

- create and identify owned Incus instances;
- distinguish infrastructure workloads from agent workloads;
- configure Linux guests;
- support NixOS as the first polished guest;
- create an internal management network;
- provision SSH client identity and target authorization;
- verify SSH host keys;
- discover workload addresses;
- generate Herdr machine configuration;
- support both container and VM agent targets behind the same Herdr connection model;
- report useful runtime status;
- cleanly stop or remove the environment.

It avoids prematurely requiring an Aginctus daemon, dashboard, database, model gateway, or MCP gateway.

## Topology

The initial environment should look conceptually like:

```text
Incus host
│
├─ aginctus CLI
│    └─ incus exec
│
├─ herdr-client container
│    ├─ Herdr client
│    ├─ Aginctus-managed SSH private key
│    ├─ generated known_hosts
│    └─ generated Herdr machine configuration
│
└─ Aginctus management network
     ├─ agent-workload-a
     │    ├─ sshd
     │    ├─ Herdr server
     │    └─ agent runtime
     │
     └─ agent-workload-b
          ├─ sshd
          ├─ Herdr server
          └─ agent runtime
```

The second workload may eventually be a VM without changing the higher-level Herdr model.

## Expected user flow

The exact CLI is intentionally deferred, but the eventual flow should resemble:

```text
aginctus up
aginctus status
aginctus herdr
```

Users should not need to discover Incus-assigned addresses, manually copy SSH keys, edit `known_hosts`, or hand-maintain Herdr machine entries.

## Success criteria

The milestone is complete when:

1. Aginctus can create a dedicated Herdr client container;
2. Aginctus can create at least one agent workload;
3. both join an Aginctus-managed management network;
4. the agent workload runs SSH and its own Herdr server;
5. Aginctus provisions an SSH identity for the Herdr client and authorizes it on the agent workload;
6. SSH host-key verification remains enabled and Aginctus provisions the required trust data;
7. Aginctus generates the Herdr machine configuration for the managed workload;
8. `aginctus herdr` launches the Herdr client inside the client container with `incus exec`;
9. the Herdr client can connect to the workload and control its Herdr server;
10. adding another managed workload can add another Herdr machine without changing the overall topology;
11. Aginctus can report enough lifecycle, network, and SSH state to diagnose common failures;
12. Aginctus can remove resources it owns without deleting unrelated Incus resources.

Container execution is the first implementation target. A VM target should use the same SSH-based Herdr model once VM bootstrap is available.

## Security boundary

For this milestone:

- the human host reaches the Herdr UI through `incus exec`, not direct SSH to agent workloads;
- the Herdr client container holds the SSH private key used for managed targets;
- agent workloads receive only the corresponding public key;
- SSH is exposed only on the internal management path unless a workload explicitly requests otherwise;
- SSH host verification stays enabled;
- the Herdr client should have only the network access required to reach managed targets;
- agent workloads should not gain unrestricted trust of each other merely because they participate in the same lab;
- possession of the Herdr client SSH identity is treated as privileged control access.

Broader internet egress, MCP access, model credentials, and workload-to-workload communication remain separate policy problems.

## Implementation sequence

This milestone is expected to span multiple pull requests.

### Architecture

Define the workload roles, management network, SSH identity model, Herdr discovery flow, and ownership boundaries.

### Development environment

Add the Nix flake and reproducible developer tooling needed to work on Aginctus.

### CLI bootstrap

Create the initial `aginctus` CLI and Incus integration boundary.

The CLI should begin with only the operations needed by this milestone rather than attempting to wrap the entire Incus command surface.

### Infrastructure and workload bootstrap

Implement:

- the Herdr client container;
- the NixOS agent-workload path;
- the management network;
- SSH key provisioning;
- host-key trust;
- Herdr server installation;
- generated Herdr machine configuration;
- the `aginctus herdr` entry flow.

### Additional target types

Add a second workload and then a VM target to prove that the Herdr client topology scales without changing the user model.

## Extension points

The first SSH identity may be deployment-scoped for simplicity.

Future improvements may include:

- per-workload keys;
- SSH certificates and an Aginctus-managed CA;
- multiple Herdr client environments;
- remote Incus hosts;
- stronger network ACLs and identity-aware policy.

## Out of scope

The first milestone does not require:

- LiteLLM;
- MCP gateway policy;
- the Aginctus HTTP API;
- a dashboard;
- multi-host scheduling;
- public SSH ingress;
- automatic internet-egress policy;
- a stable configuration schema.

Those features should build on the boundaries proven here rather than block the first working environment.
