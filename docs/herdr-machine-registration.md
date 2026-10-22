# Herdr: operator workflow and validation

This is the canonical **current v0.1 Herdr/SSH procedure** for an Aginctus-owned Herdr client and OpenCode workload. The commands use the configured `dev` workload; replace it with another explicitly configured OpenCode container ID. See the [SSH helper and trust contract](ssh-credentials.md) for what these steps change and the [state-volume guide](herdr-persistent-state-volumes.md) before any image replacement or legacy reset.

The managed Herdr client is a **separate infrastructure container** entered by the host CLI; the workload runs its own Herdr server and agent as `agent`. Completing a Herdr SSH session does not prove model-backed OpenCode inference or gateway egress.

## Prerequisites

- Initialized Incus, the configured management bridge, imported NixOS guest images, an owned **running** Herdr client and a distinct owned **running OpenCode container**, in the current CLI's `default` Incus project.
- Herdr client has its expected persistent system-state volume, identity helper, trust and machine helpers. An existing identity must already be provisioned by its guest initialization; `aginctus herdr authorize` inspects it and **does not create or rotate a private key**.
- Workload image provides `aginctus-ssh-authorize`, an SSH server with Ed25519 host key and a compatible Herdr server. The CLI currently targets management interface `eth0`, user `agent`, an SSH alias equal to the managed instance name, and Herdr remote session `default`.
- Confirm the current host's firewall permits the intended **internal** SSH path. Do not enable management NAT/routing, disable bridge filtering, change host firewall rules, or use `--force` to make a connectivity test pass.

## Read-only preflight

From a configured host, these commands do not provision identities, authorize keys or register machines:

```sh
aginctus doctor
aginctus status
aginctus herdr client status
aginctus workload status dev
aginctus herdr client apply --dry-run
aginctus workload apply dev --dry-run
```

Inspect actual instance ownership and NIC attachment using only selected non-secret fields. The example assumes default instance names; check them with `aginctus config get infrastructure.herdr.name` and `aginctus config get workloads.dev.instance.name` instead of copying these defaults to another host:

```sh
incus query '/1.0/instances/aginctus-herdr?project=default&recursion=1' |
  jq '{name, status, managed: .config["user.aginctus.managed"],
       nics: [(.expanded_devices // {}) | to_entries[] |
       select(.value.type == "nic") | {device: .key, network: .value.network}]}'
incus query '/1.0/instances/aginctus-dev?project=default&recursion=1' |
  jq '{name, status, managed: .config["user.aginctus.managed"],
       nics: [(.expanded_devices // {}) | to_entries[] |
       select(.value.type == "nic") | {device: .key, network: .value.network}]}'
incus exec aginctus-herdr --project default -- aginctus-ssh-client-identity inspect
incus exec aginctus-dev --project default -- aginctus-ssh-authorize inspect agent
```

The two `inspect` helpers return exit status **3** for absent managed state; other nonzero codes require investigation. Public-key output and ownership markers are not private secrets, but avoid sharing unfiltered `incus config show --expanded`: gateway instances can include credentials and database URLs.

If `inspect` reports a missing identity, verify the client state-volume mounts and guest image/service readiness first. Do not silently regenerate the key or replace the client. A rootfs-only legacy client requires a separately approved migration or reinitialization.

## Explicit SSH bootstrap (mutates guest/client state)

After operator approval and successful preflight:

```sh
aginctus herdr authorize dev
aginctus herdr pin-host dev
aginctus herdr register dev
```

1. **Authorize:** verify both owned running instances; read the client guest's managed Ed25519 **public** key with `aginctus-ssh-client-identity inspect`; send that key through fixed Incus exec stdin to the workload's `aginctus-ssh-authorize reconcile agent`. A different existing managed authorization fails closed; ordinary administrator keys remain untouched.
2. **Pin and probe:** obtain the workload's SSH host **public** key through authenticated Incus exec, never `ssh-keyscan`; discover its management IPv4; pin the key using `aginctus-ssh-host-trust` in the client. Run an SSH probe with explicit guest-only identity, `StrictHostKeyChecking=yes`, dedicated known_hosts, no agent and a bounded timeout. A changed host key refuses automatic trust replacement.
3. **Register:** repeat strict SSH preflight, install/inspect the immutable machine alias config, verify it authenticates as `agent`, then inspect `herdr machine list --json` and add only a missing compatible `default` session entry. Existing mismatched machine profiles fail closed; Herdr may request human approval to update an incompatible remote server, which the CLI does not auto-approve.

`authorize` alone is **not connectivity**; `pin-host` alone is **not Herdr registration**. These are distinct, explicitly mutating commands, not steps automatically performed by ordinary `aginctus apply`.

## Verify without changing trust

After the bootstrap, inspect the client and workload state. Adjust default names/project to the deployment:

```sh
incus exec aginctus-herdr --project default -- aginctus-ssh-host-trust inspect aginctus-dev
incus exec aginctus-herdr --project default -- herdr machine list --json
incus exec aginctus-herdr --project default -- herdr machine status aginctus-dev --json
incus exec aginctus-dev --project default -- aginctus-ssh-authorize inspect agent
```

Then, with an interactive operator at the host terminal, launch `aginctus herdr`. Verify that the selected remote machine opens and runs a harmless command under the workload account, then closes cleanly. Do not use `StrictHostKeyChecking=no`, `accept-new`, ambient user credentials, or a public SSH port as a fallback.

The [closed Herdr connectivity issue #120](https://github.com/blevinn/aginctus/issues/120) records successful validation on a prior host configuration, not a guarantee on a new image/host. Separately, the [v0.1 release tracker](https://github.com/blevinn/aginctus/issues/118) tracks private gateway and real model-backed acceptance. Offline tests, Herdr connectivity and a healthy LiteLLM process are not interchangeable evidence.

## Failures, revocation and upgrades

- **Missing identity or broken volume:** inspect canonical system-state mounts and guest services. Client deletion retains the custom volume; it does **not** prove safe replacement or remove authorizations in surviving workloads.
- **Unexpected authorization/host key/SSH alias drift:** stop; do not overwrite the managed key or delete the host pin. Changed incarnation, IP or SSH identity requires explicit recovery and verification. Automatic rotation and incarnation-aware trust reset are not available.
- **Revocation:** the workload guest's `aginctus-ssh-authorize remove agent` changes **only** its Aginctus-managed key file and does not remove ordinary administrator keys or terminate preexisting SSH sessions. Invoke it only after verifying target ownership and obtaining approval. `herdr client remove` is **not** automatic revocation on surviving workloads.
- **Replacement:** importing a new image alias does not upgrade an existing running instance. Follow [persistent state and legacy-reset warnings](herdr-persistent-state-volumes.md); preserve the client's SSH identity, pinned trust and Herdr state before any approved deletion/recreation.
- **Security:** do not share private keys, snapshots containing them, token-bearing instance config or raw helper failure streams. A compromised client identity grants access to the non-root account on every workload that still authorizes it.

## Host-independent validation for changes to this workflow

```sh
nix develop -c go test ./internal/cli ./internal/herdrclient ./internal/statevolume
nix develop -c python3 -m unittest discover -s guest/ssh -p 'test_*.py' -v
nix develop -c just check-all
```

These tests do not exercise the live pilot or authorize guest mutation. Actual on-host operations above require a separately approved, owned test deployment.
