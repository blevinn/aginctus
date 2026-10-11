# v0.1 Herdr client connectivity validation (#120)

This is the **first, read-only validation pass** for the v0.1 Herdr client. It verifies the deployed client instance, management NIC, Herdr binary, and the client-side SSH identity without modifying workloads or printing private credentials.

Aginctus uses a dedicated Herdr client container; it is not an HTTP client for a central Herdr API. Per [ADR 0001](../adr/0001-herdr-client-topology.md), the client connects over SSH to Herdr servers **inside managed workloads**. Successful container startup alone does not establish end-to-end connectivity. Workload SSH authorization, pinned host trust, address discovery, remote registration, and an interactive Herdr session are separate requirements.

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
incus config show "$HERDR_INSTANCE" --expanded
incus network show "$MGMT_NETWORK"
incus exec "$HERDR_INSTANCE" --project default -- sh -c '
  set -eu
  command -v herdr
  ip -4 addr show dev eth0
  ip route
'
```

Check that the instance is running, carries the Aginctus ownership metadata and has a NIC on the configured management network. Verify that the realized IPv4 address and route match the intended network. `ip route` output does not by itself prove reachability.

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
incus config show "$WORKLOAD" --expanded
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


## Deployed-host findings (2026-10-11)

The first on-host validation confirmed an Aginctus-owned management bridge, a running Herdr client on its management subnet, a working Herdr 0.9.3 systemd service, and SSH identity generation 1. The bridge address did not respond to ICMP; successful neighbor discovery makes this an insufficient test of workload-to-workload SSH, and no NAT/routing or firewall change is justified by the ping result alone. The dev workload was absent but its reconciliation dry run succeeded.

**CLI/server environment:** the Herdr systemd unit explicitly sets `XDG_CONFIG_HOME=/var/lib/herdr/config`, `XDG_RUNTIME_DIR=/run/herdr`, and `XDG_STATE_HOME=/var/lib/herdr`. Running `herdr status` directly under `incus exec` instead consults `/root/.config/herdr/herdr.sock` and reports the server absent, even though the service is active. With `XDG_CONFIG_HOME=/var/lib/herdr/config`, status correctly reports the active server with a compatible protocol. The `aginctus herdr` command also opened a window before the launcher fix, but that alone did not establish which socket/session it used. The updated launcher passes the managed environment explicitly.

Validate the managed service and console context on the deployed host:

```sh
set -eu
nix develop -c go test ./internal/cli ./internal/herdrclient
nix develop -c go test ./...
nix flake check
incus exec aginctus-herdr --project default -- \
  env XDG_CONFIG_HOME=/var/lib/herdr/config XDG_RUNTIME_DIR=/run/herdr \
  XDG_STATE_HOME=/var/lib/herdr herdr status
incus exec aginctus-herdr --project default -- \
  env XDG_CONFIG_HOME=/var/lib/herdr/config XDG_RUNTIME_DIR=/run/herdr \
  XDG_STATE_HOME=/var/lib/herdr herdr machine list
# Interactive: after updating the local CLI to this PR, verify it opens
# the managed server session and exits normally.
nix develop -c go run ./cmd/aginctus herdr
```

The machine list currently has no saved SSH machines. A successful interactive window is **not** remote workload acceptance; deploy the workload and establish authorized SSH, pinned host keys, and remote machine registration before considering #120 complete.

## Reporting results

Record the instance name, management network, workload name (if applicable), observed state and address, command exit statuses, and the precise failing stage. Redact secrets, tokens, private keys, and sensitive output. Distinguish:

1. Container/bridge provisioning
2. Guest network addressing
3. Client identity availability
4. Workload SSH authorization
5. Pinned host trust and SSH authentication
6. Herdr remote registration and interactive control

**Issue #120 remains incomplete until stages 5–6 pass on a real host.** This runbook is a validation baseline, not a claim that connectivity has been established.
