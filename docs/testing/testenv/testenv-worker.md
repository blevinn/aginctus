# Trusted Incus testenv recovery worker (v0.2, experimental)

For test tier selection and authority limits, start with the [testing overview](../README.md). This page documents **opt-in worker-only** installation of the one-shot
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
dedicated Incus daemon. The recovery subcommand accepts no command-line
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

## Operating procedures (separate authority)

The dedicated worker is a **daemon-administrator** environment, not an agent sandbox.
Use the [operator-only worker runbook](testenv-worker-operations.md) for the
actual opt-in lifecycle helper, pool/image gate, recovery and outstanding
acceptance. Never run those procedures on the pilot host. The former section
headings remain below solely to preserve existing links.

### Opt-in real-worker lifecycle helper

Follow the [dedicated worker runbook](testenv-worker-operations.md#opt-in-real-worker-lifecycle-helper) only on a separately approved worker.

### Installed read-only image and pool gate

Follow the [dedicated worker runbook](testenv-worker-operations.md#installed-read-only-image-and-pool-gate) only on a separately approved worker.

### Operational acceptance — not yet performed

Follow the [dedicated worker runbook](testenv-worker-operations.md#operational-acceptance--not-yet-performed) only on a separately approved worker.
