# Trusted Incus testenv recovery worker (v0.2, experimental)

This page documents **opt-in worker-only** installation of the one-shot
expiration reconciler. It is **not** an agent-usable VM controller and
does **not** automatically initialize Incus or configure a project.

## Dedicated host boundary

Only deploy on an isolated, disposable NixOS/KVM worker with **no** pilot
credentials, pilot routes, sensitive host paths or host Incus socket.
The worker runs its own Incus daemon. The `aginctus-testenv-recovery`
service uses a static non-login identity in the **incus-admin** group:
that group grants *daemon-administrator* authority on this host.
Systemd restrictions mitigate accidents but **do not sandbox a compromised
incus-admin process from that host**. Do not deploy on the pilot.

The NixOS module deliberately fails evaluation if `virtualisation.incus`
is not enabled separately, or if the Aginctus pilot service is enabled.
It uses exactly the `testenv` storage pool and
`/var/lib/incus/unix.socket` (the dedicated worker's local socket).
Enabling this module does not create an Incus project, image, pool,
network, admin key, or account allowed to call MCP mutating operations.

## Declarative unit, runtime-only signing credential

On the **separate worker's NixOS configuration**, after provisioning
a dedicated restricted Incus project and storage pool matching the
read-only `aginctus testenv vm policy` contract:

```nix
{
  inputs.aginctus.url = "github:blevinn/aginctus/<reviewed-revision>";

  outputs = { self, nixpkgs, aginctus, ... }: {
    nixosConfigurations.testenv-worker = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        aginctus.nixosModules.aginctusTestenvRecovery
        ({ ... }: {
          system.stateVersion = "26.05";
          virtualisation.incus.enable = true;
          networking.nftables.enable = true;
          services.aginctusTestenvRecovery.enable = true;
        })
      ];
    };
  };
}
```

The `<reviewed-revision>` placeholder must be replaced by an approved
pinned revision. This example **does not supply** the Incus daemon's
initialization, testenv storage pool, restricted project policy or disk
quotas; these must be established on the isolated worker before the
timer is activated. Avoid using this as an import on a multipurpose
host, even when the option is disabled.

Provision the policy key **out of band** as a root-owned 32-byte
regular file in a root-owned 0700 directory. One example, executed
only on that dedicated worker by its operator:

```sh
sudo install -d -m 0700 -o root -g root /var/lib/aginctus-testenv
sudo sh -c 'umask 077; head -c 32 /dev/urandom > /var/lib/aginctus-testenv/worker-policy.key'
sudo stat -c '%U %a %s %n' /var/lib/aginctus-testenv/worker-policy.key
# Expected owner root, permissions 600, size 32.
```

Never place this key in a Nix expression, Nix store derivation,
git commit, guest filesystem, MCP client or Incus instance metadata.
**Preserve it across worker reboots.** Replacing it invalidates previously
signed runs; do not regenerate it as a workaround for failure.

Systemd copies the key using `LoadCredential` into a private
credential directory. The service checks its open file descriptor for
type, ownership, permissions and exact length before contacting the
dedicated Incus daemon. The executable accepts no command-line
arguments, uses no default Incus remote, rejects root, and prints only
bounded aggregate success/failure counts.

## Runtime instance policy

Signed run metadata does not authorize arbitrary additional Incus options.
Before initial start, status, guest execution, ownership discovery or cleanup,
the backend checks the expanded instance configuration. Only the launch plan's
CPU, memory, nesting and signed run metadata keys are accepted, plus the
dedicated daemon's `image.*` and `volatile.*` metadata. Other keys, including
raw QEMU overrides, cloud-init (also legacy `user.*` seed keys), environment,
boot and snapshot controls, fail closed even when their values are empty.
The single root disk must contain exactly `type`, `path`, `pool` and `size`,
with the admitted values; additional disk options are rejected.

These are application preflight checks, not atomic daemon authorization or a
defense against a compromised worker daemon/administrator. The daemon remains
trusted for image/volatile metadata and enforcing project restrictions. A
concurrent administrator can change resources between checks and operations.
Policy drift blocks the entire discovery/sweep before deletion; an operator
must investigate it on the dedicated worker. Do not rotate the signing key or
weaken policy to bypass a failed sweep. A recoverable signed handle is returned
if post-create verification fails, but it does not authorize cleanup of a
policy-drifted instance.

Hostless regression tests exercise extra configuration and disk options before
start, exec and destruction, including expiry reconciliation, and permit
daemon metadata on an otherwise valid run. They do not prove actual Incus
project enforcement, real-host isolation or complete storage reclamation.

## Opt-in real-worker lifecycle helper

`testenv-worker-acceptance` packages the fixed
`aginctus-testenv-acceptance` binary in the Nix store. It uses the same
`IncusVMBackend`, signed handles, readiness probes, typed guest exec and
destruction path as the worker library. It is not installed or started by
the recovery module, normal checks, public CLI or MCP. Its commands and
resource sizes cannot be overridden by a checkout manifest or guest input.

### Prerequisites and invocation

**Run only after separate operator approval on the dedicated disposable
worker described above. Never run this on a multipurpose host or the pilot.**
An acknowledgement and marker are accident-prevention gates, not proof of
host isolation or project-scoped daemon credentials. The process still has
daemon-administrator authority through `incus-admin`.

Before running, the operator must independently:

- Establish the worker's isolation, restricted `aginctus-testenv` project,
  `testenv` storage pool with effective quotas, and the no-NIC/no-host-device
  project policy printed by `aginctus testenv vm policy`. The helper never
  provisions any of these.
- Build/import the [reviewed VM image](testenv-incus-vm-image.md), record its
  actual full Incus image fingerprint, and ensure the image fits a 32 GiB
  root disk. Only lowercase 64-hex fingerprints are accepted, not aliases.
- Provision the stable policy key and non-login `aginctus-testenv` identity
  described above. Never copy the key into the checkout, store or guest.
- Provision `/etc/aginctus-testenv` as a root-owned real directory, without
  group/world write access, and a root-owned **regular, single-link** file
  `/etc/aginctus-testenv/disposable-worker`, also without group/world write
  access. Its exact content is `aginctus-dedicated-disposable-worker-v1`
  followed by one newline. A symlink, including a Nix `environment.etc`
  store symlink, is deliberately rejected.
- Supply an existing, preferably stopped, unsigned foreign VM sentinel in
  that project, with a name outside the `testenv-` namespace. Do not attach
  secrets to the sentinel. Keep it unchanged throughout the test. It occupies
  one of the project's two instance slots; the helper creates its own two
  VMs **sequentially**. Size the sentinel so the helper also fits the aggregate
  CPU, memory and disk project limits. No other signed runs may exist at admission.
- Quiesce other lifecycle writers and the recovery timer for this bounded
  trial so they cannot race the helper's retained expiry observation.
  Arrange to restore the independent timer after the trial, including after
  failure. A quiesced timer is not independent recovery evidence.

The following commands are **instructions for that approved worker only**,
not part of hermetic validation. Use a clean checkout of the reviewed pinned
revision, not an agent-writable checkout, and substitute the actual image
fingerprint and sentinel name. The transient unit supplies the runtime key
and an owned private report root without installing a persistent service:

```sh
helper=$(nix --quiet build --no-update-lock-file --no-link --print-out-paths .#testenv-worker-acceptance)
"$helper/bin/aginctus-testenv-acceptance" --help

# On the independently approved dedicated worker ONLY:
sudo systemd-run --unit=aginctus-testenv-acceptance --wait --pipe --collect \
  --service-type=exec --uid=aginctus-testenv --gid=aginctus-testenv \
  --property=SupplementaryGroups=incus-admin \
  --property=LoadCredential=policy-key:/var/lib/aginctus-testenv/worker-policy.key \
  --property=StateDirectory=aginctus-testenv-acceptance \
  --property=StateDirectoryMode=0700 \
  --property=UMask=0077 --property=NoNewPrivileges=yes \
  --property=ProtectSystem=strict --property=ProtectHome=yes \
  --property=PrivateTmp=yes --property=PrivateDevices=yes \
  --property=RestrictAddressFamilies=AF_UNIX --property=IPAddressDeny=any \
  --property=TimeoutStartSec=13min --property=TimeoutStopSec=150s \
  "$helper/bin/aginctus-testenv-acceptance" \
  --disposable-worker DEDICATED_DISPOSABLE_WORKER \
  --socket /var/lib/incus/unix.socket --project aginctus-testenv \
  --image '<actual-64-hex-imported-image-fingerprint>' \
  --policy-key-file /run/credentials/aginctus-testenv-acceptance.service/policy-key \
  --report-root /var/lib/aginctus-testenv-acceptance \
  --foreign-instance '<existing-unsigned-foreign-vm>'
```

The worker socket and project must be supplied explicitly and match those
fixed values. No Incus default remote or inherited socket environment is used.
The helper rejects root; `systemd-run` starts the dedicated non-root identity.
Trusted report/key ancestors and same-UID writers remain operator concerns.

### What the helper proves when actually run successfully

Each VM uses two CPUs, 4096 MiB RAM, a 32 GiB root disk and a three-minute
signed TTL. The helper checks create, agent readiness (up to two minutes),
running status, and the fixed guest-agent command
`/run/current-system/sw/bin/incus --version` with a ten-second budget.
It requires successful exit, nonempty stdout and empty stderr, but does not
claim that the reported version string authenticates the inner daemon.

The ephemeral VM follows explicit backend destruction. The retained VM is
left until its **actual** expiry, re-observed as present, then passed through
the backend's same scoped expiry/ownership destruction path used by the
reconciler. The helper never advances its production clock or runs a broad
sweep over other runs. A retargeted signed handle must be refused before
foreign mutation; the sentinel's adapter-visible configuration/devices,
profiles, retention and status must compare unchanged at the end.

The independent volume API must positively observe each live VM root volume,
then report both absent after instance cleanup. This is separate evidence
from instance absence. It is **not** proof of physical block reclamation,
snapshot/backups/custom-volume removal, pool quotas or complete disk cleanup.
Sentinel comparison is not a full foreign-resource inventory or proof against
concurrent administrators. Expanded runtime preflight is not an adversarial
project-enforcement test.

### Evidence, failure and remaining acceptance

The helper emits a bounded JSON summary containing a run ID and pass/fail.
Inspect `/var/lib/aginctus-testenv-acceptance/<run-id>/result.json` privately
as the worker operator. It uses the existing `schema_version`, `status`,
`results`, run-ID and `disposable_guest_no_live_provider` evidence conventions,
with profile `dedicated_worker`; it is not input to the batch `vm-report`
reader and must not be represented as a hermetic report or live-provider proof.
The store package embeds the reviewed source identity (`source-<narHash>` for
dirty/non-Git builds). The private result also records the image fingerprint,
project, signed per-VM handles, fixed check outcomes and failure stage.
It never records guest output, daemon diagnostics, foreign configuration or
key bytes. Keep signed handles private even though they are not credentials.

Evidence is written atomically as 0600 in a new 0700 run directory, fsynced
before daemon admission and after handle updates, and limited to 32 KiB per
record. An advisory lock serializes this helper within a report root, not
other daemon clients. Interrupted create/start can precede local handle
persistence; signed daemon metadata and the separately installed recovery
timer remain necessary for that window.

The foreground budget is ten minutes plus up to two minutes of scoped
best-effort cleanup on error/cancellation. Only returned signed handles are
eligible for cleanup; policy drift still blocks destruction. Preserve the
private failed/incomplete result and independently inspect remaining instance
and volume inventory. Do not rotate the policy key. Restore the independent
timer through the dedicated operator procedure.

A helper `pass` covers only its implemented lifecycle subset. `crash_reboot`,
`nested_isolation`, `storage_reclamation` and `report_retention` remain
`not_verified`: no SIGKILL/reboot, nested Incus/Btrfs workload, packet isolation,
physical storage reclamation or report retention/GC trial is implemented.
Reports are deliberately retained, never automatically pruned; provision an
operator-managed bounded filesystem and review retention separately. No real
worker execution is claimed by building the package or running fake tests.
The authorization/service API and agent-facing mutations remain gated.

## Operational acceptance — not yet performed

After operator approval and after validating real project restrictions,
image boot and nested guest isolation on the **disposable worker**:

```sh
systemctl cat aginctus-testenv-recovery.service
systemctl cat aginctus-testenv-recovery.timer
systemctl list-timers aginctus-testenv-recovery.timer
# Only on that worker, explicitly start the one-shot service:
sudo systemctl start aginctus-testenv-recovery.service
sudo systemctl status aginctus-testenv-recovery.service --no-pager
```

Observe no inappropriate resources after a clean sweep. Seed
known, signed **run-owned** expired ephemeral and retained test VMs,
leave a separate foreign VM present, and repeat the sweep. Then
SIGKILL the control process, reboot the worker and verify the timer
removes only expired signed instances without altering foreign
objects. Inspect the storage pool and root-volume inventory separately:
a successful instance deletion is **not** evidence of complete disk
or report cleanup. Incomplete/unverified recovery must remain visible.

The run-admission authorization layer and mutating CLI/MCP operations
remain **disabled** pending #475/#479 acceptance. The batch NixOS
test-driver remains independently available to operators.
