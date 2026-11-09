# Incus-backed disposable VM image (v0.2, experimental)

This is an **explicitly built and operator-imported** image of an *outer*
NixOS virtual machine. It is different from `just test-vm-*`, whose
NixOS test driver starts QEMU and runs Incus *inside* its fixture.

## Security boundary

Only use this image with a **dedicated disposable worker's** own Incus
daemon and the restricted `aginctus-testenv` project. Never import it into
the v0.1 pilot's Incus daemon, nor give the worker's daemon socket, signing
key or admin privileges to a coding agent.

The image now imports the **same shared Aginctus NixOS host module**
used by `nix/vm-harness/guest.nix`, including the real system integration,
Herdr image definition and Incus configuration. It includes Incus guest-agent
loading, guest-local Incus, the store-pinned Aginctus CLI, Btrfs tooling
and no configured networking or SSH service. It does **not** reconcile
workloads or the gateway automatically; fixtures trigger those steps after
Incus initialization. **An image setting alone does not prevent guest root from adding
network configuration.** The outer Incus project must prohibit NICs and
host-device passthrough; this isolation requires separate real-host proof.

No OAuth, LiteLLM upstream keys, pilot data, host directories or
operator-specific configuration is embedded in the image. The image still
needs a trusted worker to provision the inner isolated fixture and synthetic
provider. No host control service or agent-facing mutations are activated.

## Build and inspect (offline, x86_64-linux)

```sh
nix flake check
nix build -L .#testenv-incus-vm-image
find -L result -maxdepth 1 -type f -printf '%f\n'
# Expected: disk.qcow2 and metadata.tar.xz
tar -xOf result/metadata.tar.xz metadata.yaml
```

This is a heavyweight disk-image build and intentionally is **not** part
of `just check-all`. The pure `testenv-incus-vm-image-config` Nix
check evaluates the image's guest-agent/Incus/SSH/network defaults; it
does not test KVM, perform imports or provide isolation evidence.

## Future disposable-worker import gate

**Operator-only; do not execute against the pilot daemon.** Provision a
restricted dedicated worker/project/storage pool and validate its security
settings *before* importing. On that separate worker:

```sh
# Copy the built result files into an operator-owned temporary directory
# on the disposable worker first; never grant Incus daemon access to agents.
incus --project aginctus-testenv image import \
  /operator/temporary/metadata.tar.xz \
  /operator/temporary/disk.qcow2
incus --project aginctus-testenv image list
```

Record the **actual full Incus image fingerprint** from the imported image;
that is the value the backend must pin. A filename, flake revision, mutable
Incus alias or store path is *not* an Incus image fingerprint.

In a future dedicated-worker integration test, create an ephemeral
instance through the authenticated backend, wait for the VM agent, run
`/run/current-system/sw/bin/incus --version` in the guest, and verify
that the instance has no outer NIC, no inherited profile, no host disk
source and no host/pilot secrets. Run inner Incus/Btrfs and recovery
acceptance tests, collecting evidence **before** ephemeral shutdown.

The worker provisioning, agent-readiness gate, fixed worker identity and
storage/TTL cleanup are tracked by [#479](https://github.com/blevinn/aginctus/issues/479).
A successful image build is **not** evidence that those are complete.
