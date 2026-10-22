# Herdr SSH identity and trust contract

This page is the **implemented v0.1 guest-helper contract** behind the [Herdr operator workflow](herdr-machine-registration.md). It is not a proposed SSH CA, a general credential rotation service or a claim that an arbitrary Linux guest is supported. The [Herdr client topology ADR](adr/0001-herdr-client-topology.md) explains the durable design decision.

## Trust and material flow

The trusted Aginctus host coordinates fixed commands through authenticated Incus exec. The current host CLI invokes the `incus` executable for Herdr helper operations against the `default` project; it does **not** implement a generic `ssh-access` orchestration driver.

```text
Herdr client guest                   Trusted host                       Workload guest
  private Ed25519 identity --[public key/fingerprint]--> aginctus --[public key via stdin]--> managed authorized_keys
  dedicated pinned known_hosts <--[host public key over Incus]-- aginctus <--[authenticated Incus exec]-- host key
  strict SSH + Herdr machine ------ private management bridge ----------------> agent + Herdr server
```

The client's private key never crosses into host files, rendered plans, Nix stores, workload state or command arguments. An Incus administrator is nonetheless privileged to read guest state; this is **not** protection from the trusted host administrator. Each workload generates its own SSH host private key inside the guest image instance, not in the distributed image.

## Fixed guest interfaces

| Guest helper | Location | Supported operation | Boundary |
| --- | --- | --- | --- |
| `aginctus-ssh-client-identity` | Herdr client | `ensure`, `inspect` | Creates/validates a guest-local Ed25519 identity, exposes only generation, fingerprint and public key |
| `aginctus-ssh-authorize` | OpenCode workload | `reconcile agent`, `inspect agent`, `remove agent` | Manages a dedicated public-key file; rejects root and unsafe ownership |
| `aginctus-ssh-host-trust` | Herdr client | `reconcile ALIAS`, `inspect ALIAS` | Pins a validated Ed25519 host key supplied through Incus, not SSH network discovery |
| `aginctus-ssh-machine-config` | Herdr client | `reconcile ALIAS IPv4 GENERATION`, `inspect ALIAS IPv4 GENERATION` | Maintains a strict OpenSSH alias/config, refusing unexpected replacement |

`inspect` exits **3** when the managed item is absent; other errors indicate invalid commands, ownership/permissions or inconsistent state. Avoid piping raw error streams to shared logs. Do not call mutating helpers manually on unowned instances; `aginctus herdr authorize/pin-host/register` validate host-side ownership and are the supported operator path.

The client stores `/var/lib/aginctus/ssh/identities/<generation>/id_ed25519` (root-owned `0600`) and public metadata in a protected manifest. The persistent system-state volume binds its `ssh/` directory onto `/var/lib/aginctus/ssh` and `herdr/` onto `/var/lib/herdr`. The [volume contract](herdr-persistent-state-volumes.md) describes the mount requirements and reset hazards.

The workload's `/var/lib/aginctus/ssh/authorized_keys/agent` is an Aginctus-owned public-key file, **separate from** `~agent/.ssh/authorized_keys`. Its sshd configuration consults both, preserving unrelated administrator access. The client pins host trust in `/var/lib/aginctus/ssh/trust/known_hosts` and stores a verified machine alias at `/var/lib/aginctus/ssh/machines/<instance>.conf`.

Managed SSH enforces the selected identity, `IdentitiesOnly`, no agent, `BatchMode`, pinned `HostKeyAlias`, `StrictHostKeyChecking=yes`, `UpdateHostKeys=no`, Ed25519 host key and a dedicated known_hosts rather than ambient user/host trust. The registration command verifies the alias authenticates as `agent` before publishing a Herdr machine. A newly discovered IP is **never** a new trust decision.

## Idempotence, explicit changes and limitations

- Identity `ensure` creates the first generation only if none exists; `inspect` is read-only. Unexpected missing, malformed, foreign-owned or symlinked state fails closed rather than silently replacing a key.
- Workload authorization of a matching public key is repeatable. A different managed public key is not adopted. Existing mismatched pinned host key, machine alias or Herdr registration is rejected rather than updated implicitly.
- Deleting the client's container retains the separate state volume but leaves any existing public-key authorization on workloads. Removing a workload's managed key prevents **new** logins but does not terminate established SSH sessions. A volume backup/snapshot also contains sensitive identity material.
- Automatic private-key rotation, two-generation rollout, per-workload client identities, self-service host-key replacement, incidence-aware rebootstrap, remote machine cleanup and reliable trust repair on workload recreation are **not implemented**. Do not follow hypothetical rotation state-machine designs from old PRs as operational steps.
- Workload authorization is scoped to the non-root `agent` user; once authenticated that user can run its own agent and Herdr processes. Shared management-L2 access is not workload-to-workload segmentation. Incus admin and Herdr client compromise are security-relevant.

Never copy the private key to the Incus host, weaken host verification, replace a pinned key after a mismatch, or treat `herdr client teardown` as successful credential revocation. For practical validation and approved operations use the [canonical runbook](herdr-machine-registration.md).
