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
used by `tests/nix/vm-harness/guest.nix`, including the real system integration,
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

## Scenario-specific native-instance image

The opt-in `testenv-native-instance-vm-image` output packages the existing
native-instance fixture and its pinned Go test binary and container image
inside an Incus VM. It shares the batch scenario's actual assertion script,
not a second implementation. A fixed guest-local systemd oneshot named
`aginctus-testenv-native-instance.service` runs only when requested by
trusted worker code. It is not enabled at boot. The base image is unchanged.
Direct guest execution needs no extra nested QEMU test node.

The image has no outer NIC or host-share requirement; the outer policy,
not guest settings, must enforce that. The fixed fixture is for fresh
disposable VMs only, not installed pilot hosts or arbitrary source execution.
The existing dedicated-worker acceptance command accepts
`--scenario native-instance` with the imported fingerprint of this specific
image. It starts the fixed service through the signed Incus VM agent,
observes systemd completion with short bounded queries, then destroys the
ephemeral VM and continues the retained TTL lifecycle trial. The scenario
has a 30-minute execution budget and a 35-minute signed VM lifetime.
Allow at least 43 minutes for the transient worker unit and its cleanup.
The private receipt distinguishes `native_instance_scenario` from outer
VM/root-volume cleanup and unverified isolation. Do not combine this mode
with the third-layer KVM probe.

The image build and real execution must be validated before treating this
as a supported managed test path.

## Nested KVM guest execution probe

The image contains a fixed no-NIC KVM probe at
`/run/current-system/sw/bin/aginctus-testenv-kvm-probe`. It requires actual
execution of a small x86 boot sector under hardware acceleration; no software
emulation fallback is permitted. Its success marker is not evidence of a
nested NixOS boot, inner Incus, or outer isolation.

On an independently authorized disposable worker, the existing worker
acceptance command accepts `--require-nested-kvm` to require this probe on
both ephemeral and retained guests. Older images may run the default
lifecycle trial without the optional gate, leaving the nested KVM result
unverified. A failed probe fails the requested trial and still triggers
owned cleanup. Never run this procedure on the pilot.

## Experimental automatic worker fixture

A fixed, opt-in NixOS test-driver fixture provisions a fresh worker VM and
its separate 72 GiB test disk, initializes only its guest-local Incus
daemon, imports the store-pinned image, and runs the existing signed worker
lifecycle acceptance. It is a candidate for eliminating the manually
prepared worker from that acceptance gate, not the general developer
launcher. Trusted agents may run the self-contained fixture as a development
test on an authorized KVM builder with effective Nix sandboxing, without
independent worker admission. Use a reviewed immutable source revision for
formal acceptance; never run a mutable test-driver console under a pilot
identity.

```sh
nix build --no-link --no-update-lock-file .#testenv-disposable-worker-fixture
```

The fixture uses nested Incus VMs, but does not require a third VM layer
for the optional nested guest-instruction probe. NixOS test-driver
transport and store access are part of this trusted fixture; neither
fixture construction nor a successful run proves agent-facing isolation,
physical quota enforcement or independent worker-reboot recovery.
A real KVM run is required to verify this fixture.

The same fixed worker fixture can run the direct guest native-instance
scenario end to end with its pinned scenario image:

```sh
nix build --no-link --no-update-lock-file .#testenv-managed-native-instance
```

This builds and stages the scenario-specific closure as part of the
immutable image, starts the scenario via the signed worker lifecycle,
checks scenario and outer lifecycle evidence, and independently checks the
worker's instance and volume inventory. The local development fixture requires
an authorized KVM builder with effective Nix sandboxing, not a separately
admitted persistent worker. Record the tested source; formal acceptance still
requires a reviewed immutable revision. This does not admit arbitrary callers
or grant host-level execution.
The earlier lifecycle-only target remains available. Both targets are
experimental until actually run.

A successful fixture result includes `testenv-fixture-receipt.json` in the
Nix output. It records the scenario, flake revision (or `unavailable`),
lock digest, imported image fingerprint and scoped lifecycle checks. Private
signed handles and guest logs are excluded. The receipt does not establish
agent admission or independent host teardown, and a cached Nix output is
not a fresh execution. Record the exact derivation and independently
observed builder execution when collecting acceptance evidence.

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
