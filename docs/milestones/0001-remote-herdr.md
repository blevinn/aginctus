# Milestone 1: remote Herdr on Incus

The first Aginctus milestone is a deliberately small, usable environment that exercises orchestration and networking before the project grows a larger control plane.

## Goal

From the Incus host, a user can start a Herdr client and attach it to a Herdr server running inside an Aginctus-managed Incus instance.

The remote Herdr server owns the panes and agent processes. The local Herdr client runs on the Incus host.

A successful milestone should make the connection path reproducible through Aginctus rather than requiring the user to manually inspect Incus networking or hand-edit guest configuration.

## Why this milestone

This path is useful on its own and forces Aginctus to solve foundational concerns:

- create and identify an owned Incus instance;
- configure a Linux guest;
- support NixOS as the first polished guest;
- install and run Herdr remotely;
- provision SSH access;
- resolve a reachable instance endpoint;
- preserve SSH host verification;
- report useful runtime status;
- cleanly stop or remove the environment.

It avoids prematurely requiring an Aginctus daemon, dashboard, database, model gateway, or MCP gateway.

## Expected user flow

The exact CLI is intentionally deferred, but the eventual flow should resemble:

```text
aginctus up
aginctus status
aginctus herdr
```

The final command may invoke Herdr directly, emit an SSH target, or provide equivalent connection information. The important contract is that users should not need to discover an Incus-assigned address or reconstruct SSH arguments manually.

At the Herdr layer, the connection should remain an ordinary supported remote attach over SSH.

## Success criteria

The milestone is complete when:

1. an Aginctus configuration can describe a Herdr workload;
2. Aginctus can create that workload as an Incus container;
3. the same higher-level model is capable of selecting a VM without becoming a separate product path;
4. the initial NixOS guest can boot with SSH and Herdr available;
5. the Incus host can reach the guest through an explicitly provisioned SSH path;
6. host-key verification remains enabled;
7. a Herdr client on the host can attach to the guest's Herdr session;
8. Aginctus can report enough endpoint and lifecycle state to diagnose common failures;
9. Aginctus can remove resources it owns without deleting unrelated Incus resources.

Container execution is the first implementation target. VM execution is an architectural requirement and should follow once the container path is working.

## Security boundary

For this milestone:

- the Herdr client runs on the Incus host, not inside another workload;
- the Herdr server and agent processes run inside the managed instance;
- host-to-guest SSH is explicitly allowed;
- guest-to-host access is not implicitly granted;
- no Herdr application port needs to be exposed publicly;
- SSH authentication material must not be embedded in the workload manifest;
- SSH host verification must not be globally disabled.

Broader egress, MCP access, model credentials, and workload-to-workload communication remain separate policy problems.

## Implementation sequence

This milestone is expected to span multiple pull requests.

### Architecture

Define the boundaries, network model, ownership model, and first workload contract.

### Development environment

Add the Nix flake and reproducible developer tooling needed to work on Aginctus.

### CLI bootstrap

Create the initial `aginctus` CLI and Incus integration boundary.

The CLI should begin with only the operations needed by this milestone rather than attempting to wrap the entire Incus command surface.

### Guest and Herdr bootstrap

Implement the NixOS guest path, SSH provisioning, Herdr installation, endpoint discovery, and the host-side attach flow.

## Out of scope

The first milestone does not require:

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
