# Architecture: current single-host deployment

Aginctus is a Go CLI and NixOS/Incus integration for managing a private agent lab. This page describes **what the v0.1 implementation can deploy**, not the later API, multi-host or multi-runtime vision. Its components can be provisioned without the whole pilot passing live model-backed acceptance.

## Runtime topology

```text
Operator on trusted Incus host
  aginctus CLI  <--- effective JSON configuration / NixOS module
      |
      +-- Incus resource lifecycle: network, state volumes, guests
      |    |
      |    +-- aginctus-mgmt (managed bridge; no IPv4 routing/NAT)
      |         |
      |         +-- Herdr client (NixOS container, persistent SSH state)
      |         |      |
      |         |      +-- strict, pinned SSH ---> OpenCode workload
      |         |                                     (NixOS container)
      |         |                                     Herdr server + agent
      |         |                                           |
      |         |                                  scoped runtime key
      |         |                                           |
      |         +-- LiteLLM (OCI gateway) <--- private HTTP /v1
      |         |      |
      |         +-- PostgreSQL (gateway persistence)
      |                |
      +-- aginctus-egress: separate LiteLLM-only NIC and guarded
           upstream HTTPS path, subject to host firewall policy
```

The drawing shows **intended connectivity**, not proof that the deployed pilot currently reaches an upstream model. The management bridge is shared at v0.1: network membership alone is not per-workload segmentation. PostgreSQL and normal workloads have no intended direct public egress; LiteLLM's dedicated egress path has separate routing and filtering prerequisites.

## Host control and orchestration

- **CLI/configuration:** `cmd/aginctus` and `internal/cli` implement status, lifecycle, gateway and Herdr commands. `internal/config` merges defaults, configuration files, environment and explicit overrides. The [configuration guide](configuration.md) is authoritative for precedence and values; [CLI operations](cli.md) documents effects and preview limits.
- **NixOS integration:** `nix/nixos-module.nix` packages the CLI, typed host settings and optional image imports. `services.aginctus.firewall.enable` defaults **on**, reconciling selected host DNS/DHCP firewall rules; `services.aginctus.reconcile.enable` defaults **off**, leaving Incus resource deployment explicit unless opted in. `reconcile.gateway` is a further opt-in. These are **independent mutation boundaries**.
- **Incus planning:** Jsonnet generates ordered resource plans, prevalidated by `internal/orchestration`. The current resource drivers use Incus Apply and Compose integration, plus an owned state-volume driver. A migration to native Incus reconciliation is [planned](design/native-incus-reconciliation.md), **not the current driver**. `aginctus apply --render` is an offline plan; `apply` and `apply --from FILE` execute and mutate resources.
- **Ownership and discovery:** network, instances, Compose resources and persistent state carry Aginctus ownership markers and are checked before ordinary mutations. `--force` may relax selected guards; it is not a general safe repair mechanism. The current CLI/Herdr guest paths use the default Incus project; independent named Incus projects are future work.

The Incus host and its administrators are in the trusted computing base. Ownership metadata helps reject accidents and foreign collisions, not privileged tampering.

## Herdr control path

`aginctus herdr` enters the dedicated **Herdr client container** using Incus exec and connects to Herdr servers inside managed workload guests. It is not a central Herdr HTTP API and the client container does not run OpenCode itself.

For the supported OpenCode NixOS guest, explicit host commands authorize the client's public SSH key, pin the guest's Ed25519 SSH host key from authenticated Incus inspection, probe strict SSH as non-root `agent`, and register the managed machine with Herdr. Client private keys remain guest-local on a persistent system-state volume. Host-key changes and incompatible profiles fail closed; there is no automatic rotation or safe cross-incarnation trust reset. See [Herdr operator workflow](herdr-machine-registration.md), [SSH credential contract](ssh-credentials.md) and [persistent state](herdr-persistent-state-volumes.md).

## Model gateway and credential path

`internal/gateway` renders and deploys an Aginctus-owned LiteLLM + PostgreSQL Compose service with protected runtime secrets. The gateway is reachable on the private management network. The OpenCode image contains no upstream provider credential: the operator obtains an approved, scoped LiteLLM workload key and **explicitly** runs `aginctus workload gateway provision`. The host discovers the owned LiteLLM service's management address, transfers the already issued key to a fixed guest helper over Incus stdin, and the OpenCode launcher fails closed if runtime configuration is missing or invalid.

Gateway provider/model registration is **local metadata**, not a completed OAuth authorization, LiteLLM route activation or successful inference request. LiteLLM's additional OCI egress NIC, default route and host firewall are handled by separate guarded procedures. Enabling NAT on the management bridge or allowing a workload direct provider access is **not** a fallback. See [gateway](ai-gateway.md), [key provisioning](workloads/opencode.md#provision-private-gateway-access-123), [host firewall](gateway-firewalld-egress.md) and [egress acceptance](litellm-oci-egress-pilot.md).

## State and trust boundaries

| Boundary | Implemented behavior | Important limitation |
| --- | --- | --- |
| Configuration vs secrets | Nix configuration excludes gateway secret paths; runtime gateway secrets reside in protected host state | Incus administrators and privileged host processes remain trusted |
| Herdr client vs workload | Private client SSH identity stays in client; only the public key is installed in the workload; host key is pinned | A compromised client identity can control authorized workload accounts; recovery/rotation is manual |
| Workload vs gateway | OpenCode receives a scoped LiteLLM key in guest runtime state; no baked provider key | Private-bridge HTTP carries bearer keys in plaintext; provisioning does not test effective remote scope |
| Management vs egress | Management bridge is unrouted; external provider path is limited to the gateway egress attachment | Same-L2 management peers are not mutually isolated; real firewall and OCI route acceptance still require observation |
| Root disk vs persisted state | Herdr client system-state volume outlives its rootfs | Removing a client does not automatically revoke its keys on surviving workloads or establish safe image migration |

[Security controls and limitations](security.md) has the full risk statement. Treat Incus guest state, container images, snapshots and backups containing credentials as sensitive.

## What is not yet the shipping architecture

A public control-plane API, dashboard, Bubble Tea TUI, multiple independent Incus projects, remote Kubernetes workloads, arbitrary aggregation shells, broadly portable guest bootstrap, general-purpose managed SSH rotation, agent-controlled host tools/MCP grants and native Incus reconciliation are **design/roadmap work**. See [project overview](project-overview.md) for direction and [ADR 0001](adr/0001-herdr-client-topology.md) / [ADR 0002](adr/0002-native-incus-reconciliation.md) for durable decisions. Do not copy future proposals as current configuration contracts.

## Verification boundary

`just check-all`, rendering and read-only preflight validate offline contracts, not the pilot's live key scope, route, provider TLS, inference response, or model-directed file/test operation. The [v0.1 release checklist](https://github.com/blevinn/aginctus/issues/118) and its linked acceptance issues remain authoritative; a running container or HTTP 200 health check is insufficient. Consult [getting started](getting-started.md) for non-mutating versus explicitly approved deployment commands.
