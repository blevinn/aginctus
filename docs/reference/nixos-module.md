# NixOS host module integration and activation boundaries

This is the **NixOS-specific** companion to [configuration source precedence](configuration.md). The host's incumbent NixOS and Incus administrator retains authority. A Nix evaluation, unit activation or dry-run is not permission to alter a live pilot's firewall, networks, images or guest state. Review all user-specific settings, privilege boundaries and rollback before enabling any mutating service. Home Manager operator setup remains a separate boundary described in [host operations](../operations/host/home-manager.md).

## NixOS module

The flake exports `nixosModules.default` (also named `nixosModules.aginctus`) for host integration.

A consuming NixOS flake can import it with:

```nix
{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
  inputs.aginctus.url = "github:blevinn/aginctus";

  outputs = { self, nixpkgs, aginctus, ... }: {
    nixosConfigurations.lab = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        aginctus.nixosModules.default
        ({ pkgs, ... }: {
          system.stateVersion = "26.05"; # Use your host's existing stateVersion.
          networking.nftables.enable = true;
          virtualisation.incus.enable = true;

          services.aginctus = {
            enable = true;

            settings = {
              incus.management.network.name = "aginctus-mgmt";
              infrastructure.herdr.storage.pool = "default";

              workloads.dev = {
                instance.name = "aginctus-dev";
                runtime.type = "opencode";
                isolation = "container";
                image.alias = "aginctus-opencode-workload";
                storage.pool = "default";
              };
            };

            # Explicit opt-in: these commands change host Incus state.
            reconcile = {
              enable = true;
              workloads = [ "dev" ];
            };
          };
        })
      ];
    };
  };
}
```

The module writes the declared settings to `/etc/aginctus/config.json`, preserving the same JSON shape and precedence semantics consumed by the CLI. Known settings are typed Nix options, while a freeform JSON layer allows newly introduced configuration keys to be represented without waiting for a module update. Runtime gateway secrets remain intentionally out of band. The module rejects `settings.gateway.postgres.password`, `settings.gateway.master.key`, and `settings.gateway.salt.key` (including keys introduced via the freeform JSON layer), since generated Nix store configuration is not an appropriate place for credentials. Leave these secrets unset for runtime generation; do not include them in Nix options or inline systemd environment values.

When enabled, the module defines oneshot image lifecycle units for the Herdr client and OpenCode workload images. Each unit imports the corresponding image derivation through `aginctus images update` and tracks both the image derivation and Aginctus CLI package, so changes to either input can cause the unit to run again.

Optional boot reconciliation can be enabled with:

```nix
services.aginctus.reconcile = {
  enable = true;
  workloads = [ "dev" ];
};
```

This reconciles the management network, Herdr client, and selected workloads only after Incus and any enabled image update services have succeeded. The services include the host `incus` executable in their systemd `PATH`. An image import failure prevents dependent reconciliation instead of allowing workloads to start against an outdated alias. Reconciliation is disabled by default because it mutates Incus state.

Set `services.aginctus.reconcile.gateway = true` to additionally invoke `aginctus gateway apply` during the same boot reconciliation, between the firewall reconcile and the Herdr client ensure. The apply runs the gateway orchestration plan against the configured `services.aginctus.settings.gateway` values, including the gateway egress bridge, Compose project, LiteLLM and PostgreSQL instances, and protected runtime secrets under `$XDG_STATE_HOME`. When the option is on, the reconciler pins `XDG_STATE_HOME=/var/lib/aginctus/state` in its oneshot environment so the gateway CLI's state-directory resolution does not depend on systemd exporting `$HOME`. Re-runs are idempotent against the declared settings. The option requires `services.aginctus.reconcile.enable = true` and is rejected at evaluation when set without it. Operators that prefer to drive `aginctus gateway apply` themselves should leave it disabled.

The sequence is `network ensure`, host firewall reconciliation (when enabled), `gateway apply` (when opted in), `herdr client ensure`, then selected workload ensures. A failed step stops later commands. Successful apply means resource application completed, **not** that the gateway is healthy, reachable, or ready for inference. There is no application-readiness gate before Herdr or workloads. Live routing, TLS, authentication, and inference acceptance remain separate operator-controlled gates (#257/#122/#123/#124).

The root-run reconciler generates missing secrets in `/var/lib/aginctus/state/aginctus/gateway/<gateway.id>/secrets.json` (default ID `local`). The CLI protects the per-gateway directory with mode 0700 and the secret file with mode 0600; the file is runtime state, not a Nix derivation. Retain this persistent state across boots and generation changes: subsequent initialization reuses stored values, even if runtime seed variables change. Removing state is not a rotation procedure and can leave deployed resources with mismatched credentials. `TestInitializeRuntimeEnvironmentGeneratesAndPersistsMissingSecrets` and `TestInitializeRuntimeEnvironmentUsesProvidedSeedOnlyWhenMissing` in `internal/gateway/gateway_test.go` cover reuse, file permissions, and seed precedence without Incus.

### Host firewall reconciliation (default-on)

Independent of the Incus-state reconcile above, the module defaults
`services.aginctus.firewall.enable = true`. When enabled, the module
adds a boot-time oneshot and a 30-second timer that call
`aginctus network firewall reconcile` and (on the firewalld backend)
materialize a generated drop-zone file under `/etc/firewalld/zones/`.
The reconcile mutates **host** INPUT rules — it is not the same
operation as `services.aginctus.reconcile`, which targets Incus
state. Disable the firewall reconciler explicitly to opt out:

```nix
services.aginctus.firewall.enable = false;
```

The full host example at the top of this document enables the
firewall reconciler silently. See
[`docs/firewall-host-backends.md`](../operations/host/firewall-host-backends.md) for the
ownership contract and backend-specific caveats.

The flake provides `packages.<system>.aginctus` (also `packages.<system>.default`) and the module selects that package by default. A consuming host can still override `services.aginctus.package` to pin or replace the CLI independently.

The CLI package uses a pinned `vendorHash` in the Aginctus flake. If a future Go dependency update changes the vendored module tree, regenerate the hash as part of that update and verify `nix build .#aginctus`. The initial fixed-output hash placeholder has already been resolved.

### Module evaluation checks

The flake exposes a `checks.<system>.nixos-module` check, exercised by `nix flake check`. It evaluates the NixOS module with a harmless CLI stub and verifies typed configuration defaults and overrides, image service configuration, and reconciliation dependencies both with and without image imports enabled. It also runs generated reconciliation scripts with recording CLI/systemctl substitutes to verify exact command order, default-off/no-auto-deletion behavior, repeat invocation, and failure stopping later commands. It does not start Incus or modify a host. Real-world systemd activation, resource idempotence, application readiness, and CLI package builds still require separate validation.


### Image lifecycle validation

NixOS evaluates image alias names before attempting a systemd import: each enabled alias must satisfy Aginctus's Incus alias naming constraints (1–100 ASCII letters, digits, underscores, periods, or hyphens, beginning with an alphanumeric character), and enabled image imports must use distinct aliases. When reconciliation is enabled, every ID in `services.aginctus.reconcile.workloads` must exist under `services.aginctus.settings.workloads`. These checks reject configuration errors before mutating Incus state. They do not recreate running containers or imply that changing an image alias automatically upgrades an existing instance.


### Reconciliation on NixOS generation changes

When `services.aginctus.reconcile.enable = true`, the reconciliation oneshot is also restarted when the generated configuration, Aginctus CLI package, or any **enabled** image source derivation changes in a NixOS generation. It is ordered after and requires the enabled image-import units, so a failed import prevents the dependent reconciliation run. Disabled image sources do not trigger reconciliation. When reconciliation is disabled, no automatic ensure operations are added.

These triggers re-run the existing idempotent `network ensure`, `herdr client ensure`, and selected `workload ensure` commands. They do **not** promise replacement of already-created Incus instances when an image alias points to a newer image; instance recreation requires a separate, explicit upgrade policy.

When `services.aginctus.reconcile.gateway = true`, the same triggers also re-run `aginctus gateway apply` against the declared gateway settings. Like the other steps, the apply is idempotent: a re-run observes the current Compose project, instances, and egress bridge, and applies only the declared drift. Removing the option from a host that has previously applied the gateway does not by itself stop the gateway; explicit `aginctus gateway remove` is the destructive path.

### Image-import service activation on CLI updates

Each enabled image-import oneshot tracks both its image source derivation and the configured `services.aginctus.package` as restart triggers. Updating the CLI package in a NixOS generation therefore re-runs image import even if the source image derivation has not changed; the CLI's fingerprint check makes identical imports idempotent. These are image alias updates only: already-running Incus instances are not recreated.

### NixOS integration acceptance checklist (issue #67)

The flake exports `nixosModules.aginctus`/`nixosModules.default` and `packages.<system>.aginctus`/`packages.<system>.default`. The module exposes `services.aginctus.settings` as typed options for known configuration paths plus a freeform JSON-compatible namespace for other paths, and generates `/etc/aginctus/config.json`. Its known image services import image derivations through systemd, with restart triggers for image and CLI changes. Optional reconciliation waits for successful image services and is disabled by default. The module rejects configured runtime gateway secrets in Nix store settings.

On a disposable x86_64-linux host, validate the implementation from the repository root:

```sh
nix build .#aginctus
nix build .#checks.x86_64-linux.nixos-module
nix flake check
just check
just test-nix
```

`just test-nix` boots disposable QEMU guests: one runs a real Incus daemon and tests imports, idempotency, alias replacement, and failed import handling; another tests systemd reconciliation ordering and failure gating with a recording CLI. This is heavier than the evaluation-only flake checks. The runner does not connect to the host's Incus daemon. On a production host, review the generated systemd units and enable reconciliation only after reviewing the Incus resources it will mutate.

**Scope boundary:** Changing an image alias does not replace existing running instances. Instance rollout, data migration, health checks, and instance-level rollback require a separate explicit upgrade policy and are not part of issue #67.
