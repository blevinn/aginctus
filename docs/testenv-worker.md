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
