# Herdr SSH bootstrap workflow

This document explains the two SSH guest helpers introduced by the SSH bootstrap work, where they run, and how their outputs are connected.

For the full credential lifecycle and security model, see [Herdr SSH credential lifecycle](ssh-credentials.md).

## What this PR does

This PR installs two fixed helper programs into two different Incus images:

| Helper | Installed in | Purpose |
| --- | --- | --- |
| `aginctus-ssh-client-identity` | Herdr client image | Own the Herdr client's SSH keypair and expose only its public identity |
| `aginctus-ssh-authorize` | Managed workload image | Own Aginctus's dedicated `authorized_keys` file for a workload login |

The helpers do **not** invoke each other.

The Aginctus host process is the coordinator. It runs each helper independently through authenticated Incus guest exec and carries only the public key from the Herdr client guest to the workload guest.

This PR provides the guest-side contract and image configuration. It does **not** yet wire the host-side invocation into an Aginctus CLI command. The follow-up remote lifecycle invokes these helpers through the Incus Go API.

## Components

The flow has three trust domains:

```text
                         authenticated Incus API
                    ┌─────────────────────────────┐
                    │                             │
                    │                             ▼
┌──────────────────────────┐          ┌──────────────────────────┐
│ Aginctus host process    │          │ Herdr client guest       │
│                          │          │                          │
│ 1. exec identity helper  │─────────▶│ private key lives here   │
│ 2. read public output    │◀─────────│ public key is returned   │
│ 3. send public key only  │          └──────────────────────────┘
│                          │
│                          │          ┌──────────────────────────┐
│                          │─────────▶│ Managed workload guest   │
│                          │  stdin   │                          │
│                          │          │ authorization helper     │
│                          │          │ writes managed key file  │
└──────────────────────────┘          └──────────────────────────┘
```

The private key never crosses the Incus API. Only the public key line produced by the client helper is delivered to a workload.

## Step 1: ensure the Herdr client identity

Aginctus executes this inside the Herdr client guest:

```sh
aginctus-ssh-client-identity ensure
```

On first use, the helper creates:

```text
/var/lib/aginctus/ssh/
├── .lock
├── manifest
└── identities/
    └── 1/
        ├── id_ed25519
        └── id_ed25519.pub
```

The private key is root-owned mode `0600`. The manifest records only the active generation.

The command prints a tab-delimited response:

```text
generation	1
fingerprint	SHA256:...
public_key	ssh-ed25519 AAAA... aginctus:1
```

Aginctus parses that output and retains the `public_key` value. It never reads or copies `id_ed25519`.

Repeated `ensure` calls validate and return the existing identity rather than generating another key.

For read-only discovery, Aginctus calls:

```sh
aginctus-ssh-client-identity inspect
```

`inspect` exits with status 3 when no identity exists. That lets dry-run distinguish "not created yet" from corrupt state without generating a key.

## Step 2: deliver the public key to a workload

For the OpenCode workload, the target SSH account is `agent`.

Aginctus takes the public key returned in step 1 and sends it on stdin to this command inside the workload guest:

```sh
aginctus-ssh-authorize reconcile agent
```

Conceptually:

```text
Herdr client helper stdout
        │
        │ public_key only
        ▼
Aginctus host process
        │
        │ Incus exec stdin
        ▼
workload: aginctus-ssh-authorize reconcile agent
```

The workload helper validates that each input line is an Ed25519 key carrying the managed `aginctus:<generation>` comment and atomically writes:

```text
/var/lib/aginctus/ssh/authorized_keys/agent
```

It does not modify `/home/agent/.ssh/authorized_keys`.

The workload image configures sshd with:

```text
AuthorizedKeysFile .ssh/authorized_keys /var/lib/aginctus/ssh/authorized_keys/%u
```

Therefore normal administrator keys and Aginctus-managed authorization remain separate.

For read-only discovery:

```sh
aginctus-ssh-authorize inspect agent
```

This prints the currently managed key and exits with status 3 when no managed authorization exists.

To revoke only Aginctus access:

```sh
aginctus-ssh-authorize remove agent
```

Removal is idempotent and leaves ordinary administrator authorization untouched.

## Manual end-to-end example

Until host-side orchestration is wired, the contract can be exercised manually through Incus.

First ensure the identity and capture only the public key:

```sh
identity="$(
  incus exec aginctus-herdr -- aginctus-ssh-client-identity ensure
)"

public_key="$(
  printf '%s\n' "$identity" |
    awk -F '\t' '$1 == "public_key" { sub(/^public_key\t/, ""); print }'
)"
```

Then deliver that public key to the workload helper:

```sh
printf '%s\n' "$public_key" |
  incus exec aginctus-dev -- aginctus-ssh-authorize reconcile agent
```

Verify the installed managed authorization:

```sh
incus exec aginctus-dev -- aginctus-ssh-authorize inspect agent
```

And remove it again:

```sh
incus exec aginctus-dev -- aginctus-ssh-authorize remove agent
```

These commands demonstrate the contract. Production Aginctus code should use the Incus Go API directly rather than shelling out to the `incus` CLI.

## Why there are two helpers

The split is intentional.

The client helper owns secret state and never accepts key material from outside the Herdr client. Its only outbound interface is public metadata.

The workload helper owns authorization state and never needs access to the private key. Its input is public key material only.

Keeping those responsibilities separate means the host can coordinate trust without placing the private key in host files, command arguments, Jsonnet data, image contents, or workload storage.

## Failure behavior

Both helpers fail closed on unsafe filesystem state such as symlinks or unexpected ownership.

Common exit statuses are:

| Status | Meaning |
| --- | --- |
| 0 | Operation succeeded |
| 1 | Unsafe, corrupt, or invalid guest state |
| 2 | Invalid command or arguments |
| 3 | Read-only `inspect` found no managed state |

The host-side caller should treat status 3 as "not provisioned" only for `inspect`. Other non-zero statuses are errors and should stop reconciliation.

## What happens after authorization

Installing the public key is only the first half of making a workload a usable Herdr remote.

The host-side remote lifecycle must additionally:

1. read the workload SSH host public key through Incus;
2. discover the realized management address;
3. pin host trust in the Herdr client;
4. publish the managed SSH target configuration;
5. probe SSH with strict host-key checking;
6. register the verified target with Herdr.

Those later steps deliberately build on these two helpers rather than adding another way to create or copy credentials.
