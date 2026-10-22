# v0.1 Herdr client connectivity validation (#120)

This is a **read-only, repeatable host validation** for the v0.1 Herdr client. It verifies the deployed client instance, management NIC, Herdr binary, and the client-side SSH identity without modifying workloads or printing private credentials.

Aginctus uses a dedicated Herdr client container; it is not an HTTP client for a central Herdr API. Per [ADR 0001](adr/0001-herdr-client-topology.md), the client connects over SSH to Herdr servers **inside managed workloads**. Successful container startup alone does not establish end-to-end connectivity. Workload SSH authorization, pinned host trust, address discovery, remote registration, and an interactive Herdr session are separate requirements.

## Prerequisites

Run from the Aginctus repository on the Incus host with the development shell available (`nix develop`). You must have an initialized Incus daemon, a configured storage pool, the Herdr client image imported, and an Aginctus-owned Herdr client instance deployed. Use the configured instance/network names if they differ from the defaults.

Do not use `--force` to work around ownership failures. Do not run this against unrelated instances.

## Host-side checks (read-only)

```sh
set -eu
HERDR_INSTANCE="${HERDR_INSTANCE:-aginctus-herdr}"
MGMT_NETWORK="${MGMT_NETWORK:-aginctus-mgmt}"

go test ./internal/herdrclient ./internal/cli ./internal/managementnetwork
go run ./cmd/aginctus doctor
go run ./cmd/aginctus status
incus list "$HERDR_INSTANCE"
incus query "/1.0/instances/${HERDR_INSTANCE}?recursion=1" | jq '{name, status, ownership: {managed: .config["user.aginctus.managed"], resource: .config["user.aginctus.resource"]}, nic_networks: [(.expanded_devices // {}) | to_entries[] | select(.value.type == "nic") | {device: .key, network: .value.network}]}'
incus network show "$MGMT_NETWORK"
incus exec "$HERDR_INSTANCE" --project default -- sh -c '
  set -eu
  command -v herdr
  ip -4 addr show dev eth0
  ip route
'
```

The `incus query` output intentionally selects only ownership markers and NIC network names; never paste unfiltered expanded Incus configuration into logs, issues or chat, as it may contain secrets. Check that the instance is running, has the expected Aginctus ownership metadata and a NIC on the configured management network. Verify that the realized IPv4 address and route match the intended network. `ip route` output does not by itself prove reachability.

## Client identity (read-only)

```sh
incus exec "${HERDR_INSTANCE:-aginctus-herdr}" --project default -- \
  aginctus-ssh-client-identity inspect
```

An exit code of 3 means the identity has not been provisioned. Do not interpret that as a connectivity success. This command displays public metadata only; do not copy private key files out of the guest.

## Remote workload checks (when a managed workload exists)

Replace `WORKLOAD` with an Aginctus-owned workload name. These checks inspect state; they do not authorize SSH or create trust records.

```sh
set -eu
WORKLOAD="${WORKLOAD:?Set WORKLOAD to an Aginctus-owned workload name}"
incus list "$WORKLOAD"
incus query "/1.0/instances/${WORKLOAD}?recursion=1" | jq '{name, status, ownership: {managed: .config["user.aginctus.managed"], resource: .config["user.aginctus.resource"]}, nic_networks: [(.expanded_devices // {}) | to_entries[] | select(.value.type == "nic") | {device: .key, network: .value.network}]}'
incus exec "$WORKLOAD" --project default -- sh -c '
  set -eu
  ip -4 addr show dev eth0
  command -v sshd
  command -v herdr
'
incus exec "$WORKLOAD" --project default -- \
  aginctus-ssh-authorize inspect agent
```

`aginctus-ssh-authorize inspect agent` exits 3 when no managed authorization exists. Confirm that both instances are on the expected management network and that the workload runs an SSH service and a Herdr server. Do not treat these inspections as proof of authenticated SSH connectivity.

## Interactive acceptance (requires completed remote registration)

Once Aginctus has provisioned the client's SSH identity, workload authorization, pinned SSH host key, and Herdr machine configuration, launch:

```sh
go run ./cmd/aginctus herdr
```

Inside Herdr, verify the intended managed workload is available and that a remote session can execute a harmless command. Record whether the session connects and exits cleanly. Do not disable `StrictHostKeyChecking`, use `ssh-keyscan` as an unauthenticated trust source, or copy the private identity to the host.


## Interpreting current results

[#120](https://github.com/blevinn/aginctus/issues/120) closed after real-Incus strict SSH authorization, host trust, registration and connectivity validation. That result applies to the tested deployment, **not automatically to this host or a replacement workload**. Reconfirm the present guest identity, Herdr service environment and saved machine entries before interactive use. A Herdr window opening does not prove model-backed OpenCode inference: the separate gateway and acceptance gates remain [#122](https://github.com/blevinn/aginctus/issues/122), [#123](https://github.com/blevinn/aginctus/issues/123) and [#124](https://github.com/blevinn/aginctus/issues/124).

## Reporting results

Record the instance name, management network, workload name (if applicable), observed state and address, command exit statuses, and the precise failing stage. Redact secrets, tokens, private keys, and sensitive output. Distinguish:

1. Container/bridge provisioning
2. Guest network addressing
3. Client identity availability
4. Workload SSH authorization
5. Pinned host trust and SSH authentication
6. Herdr remote registration and interactive control

Record any newly discovered host-specific failure against the appropriate active issue. Passing these checks does not complete #122/#123/#124 or prove upstream inference.
