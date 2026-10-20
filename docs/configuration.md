# Configuration

Aginctus configuration is assembled from multiple sources with deterministic precedence.

From highest priority to lowest:

1. command-line overrides passed with `--config=path=value`;
2. an explicit command-line configuration file passed with `--configuration-file=path`;
3. environment variables prefixed with `AGINCTUS_`;
4. the user configuration file;
5. the system configuration file;
6. built-in defaults.

Higher-priority sources override lower-priority sources.

## Command-line overrides

Use dotted paths:

```sh
aginctus --config=z.b.c=xyz config get z.b.c
```

The `--config` option may be repeated:

```sh
aginctus \
  --config=incus.management.network.name=my-management \
  --config=incus.management.network.ipv4.nat=false \
  config show
```

Override values are parsed as JSON when possible. This means values such as `true`, `false`, `42`, arrays, and objects retain their JSON types. Values that are not valid JSON are treated as strings.

`--configure` remains accepted as an alias for `--config`.

## Command-line configuration file

Use:

```sh
aginctus --configuration-file=abc.json config show
```

The file must contain a JSON object. Its values override environment, user, system, and default configuration.

## Environment

Environment variables use the `AGINCTUS_` prefix. Underscores after the prefix map to dots in the configuration path, and names are normalized to lowercase.

For example:

```sh
export AGINCTUS_Z_B_C=xyz
```

sets:

```text
z.b.c = "xyz"
```

Similarly:

```sh
export AGINCTUS_INCUS_MANAGEMENT_NETWORK_NAME=lab-mgmt
```

sets `incus.management.network.name`.

As with `--config`, environment values are parsed as JSON when possible and otherwise treated as strings.

### Runtime gateway secrets

The gateway PostgreSQL password, LiteLLM master key, and LiteLLM salt key are **runtime-only** values, not Aginctus configuration settings. The paths `gateway.postgres.password`, `gateway.master.key`, and `gateway.salt.key` are rejected in all configuration files (system, user, and explicit CLI), `--config` overrides, and object-valued environment mappings, including nested JSON objects. This also applies when a prohibited key is set to `null`.

The standalone environment variables `AGINCTUS_GATEWAY_POSTGRES_PASSWORD`, `AGINCTUS_GATEWAY_MASTER_KEY`, and `AGINCTUS_GATEWAY_SALT_KEY` remain available to the gateway's existing runtime secret-initialization mechanism. The configuration loader deliberately excludes these direct variables from `config show` and `config get`. Do not place their values into configuration files, NixOS `settings`, object-valued overrides, CLI arguments, `systemd.services.*.environment`, inline unit `Environment=`, or generated Nix files: Nix store and generated unit content are not secret stores. For NixOS reconciliation, leave these variables unset and use generated persistent state as described below. External pre-seeding integration is deferred; the module provides no secret-input option.

## User configuration file

On Linux, the default user configuration file is:

```text
~/.config/aginctus/config.json
```

Aginctus uses the platform user configuration directory, so `XDG_CONFIG_HOME` is respected when set.

A missing user configuration file is not an error.

## System configuration file

The system configuration file is:

```text
/etc/aginctus/config.json
```

A missing system configuration file is not an error.

## JSON merging

Configuration files are merged recursively by object key. A higher-priority scalar, array, or object replaces the lower-priority value at the same path unless both values are JSON objects, in which case their keys are merged recursively.

For example, a system file may define:

```json
{
  "incus": {
    "management": {
      "network": {
        "name": "aginctus-mgmt",
        "ipv4": {
          "nat": false
        }
      }
    }
  }
}
```

and a user file may override only the name without repeating the rest of the object.

## Gateway egress DHCP input

`gateway.egress.network.ipv4.dhcp` is optional. Omitting it preserves the
default enabled DHCP behavior without an explicit pool restriction. When set,
it accepts either a legacy boolean (`true` enables DHCP, `false` disables it)
or a nested object with a required boolean `enabled` and optional string
`ranges`. No other object members are accepted. Missing or mistyped `enabled`,
non-string `ranges` (including `null`), and unknown members are errors before
an orchestration plan is returned.

For example, this JSON restricts the egress bridge's DHCP pool:

```json
{
  "gateway": {
    "egress": {
      "ipv4": { "address": "10.42.40.25/24" },
      "network": {
        "ipv4": {
          "address": "10.42.40.1/24",
          "dhcp": {
            "enabled": true,
            "ranges": "10.42.40.100-10.42.40.199"
          }
        }
      }
    }
  }
}
```

Omitted or empty `ranges` means no explicit pool restriction. Nonempty ranges
require enabled DHCP, explicit bridge and LiteLLM IPv4 CIDRs, and a valid pool
that excludes the bridge, subnet and LiteLLM static addresses.
Disabled DHCP requires both explicit CIDRs; it cannot be combined with a
nonempty range.

JSON keys along the DHCP input path must be nested: literal dotted keys such
as `"dhcp.ranges"` beside `"dhcp"`, or `"ipv4.dhcp"`, are rejected even without
a colliding nested value. Dotted CLI override paths and environment mappings
still construct nested objects; set `dhcp.enabled` explicitly when setting
`dhcp.ranges`. Nix attribute paths without quotes likewise produce nested JSON.
This restriction is local to Aginctus DHCP input, not a global config-key rule.
Rendered Incus resource keys remain literal `ipv4.dhcp` (set to `"false"` when
disabled, otherwise omitted to retain Incus's enabled default) and
`ipv4.dhcp.ranges` (the exact nonempty pool string).

## Inspecting effective configuration

Print the complete merged configuration:

```sh
aginctus config show
```

Print one value:

```sh
aginctus config get incus.management.network.name
```

These commands apply the same precedence rules used by operational Aginctus commands.

## Initial defaults

The initial defaults reserve configuration for the management network that will be used by the first Incus provisioning slice:

```json
{
  "incus": {
    "management": {
      "network": {
        "name": "aginctus-mgmt",
        "ipv4": {
          "address": "auto",
          "nat": false,
          "routing": false
        },
        "ipv6": {
          "address": "none"
        }
      }
    }
  }
}
```

These are defaults rather than hard-coded orchestration constants. `aginctus network ensure` consumes these effective values when creating or reconciling the management bridge, and `aginctus network teardown` uses the effective network name when removing it, so higher-precedence sources apply consistently to both lifecycle operations.

Both network lifecycle commands support `--dry-run` and `--force`. Dry-run reports the intended mutation without applying it. Force only relaxes the Aginctus ownership check for a same-named Incus-managed bridge; it does not bypass network type checks or Incus API errors.

### Where a default comes from

Two registries describe the same effective configuration: the CLI's `defaults` map in `internal/config/config.go` and the module's typed options in `nix/nixos-module.nix`. The module renders its defaults into `/etc/aginctus/config.json`, which overrides the CLI fallbacks, so a value that differs between the two registries changes behavior depending on which layer supplies it.

`TestDefaultsMatchNixOSModule` (`internal/config/nix_defaults_parity_test.go`) fails when the registries disagree in either direction. The `gateway-config-parity` flake check runs it against the module's actual rendered output; plain `go test ./internal/config` runs it against the committed snapshot in `internal/config/testdata`. Adding a default therefore requires adding it to both registries, or recording it as a CLI-only fallback.

Two keys are CLI-only fallbacks today, listed explicitly in that test:

| Key | Value | Why |
| --- | --- | --- |
| `gateway.credentials.storage.pool` | `default` | Incus pool for the gateway credential volume; the module has no option for it |
| `gateway.egress.network.name` | `aginctus-egress` | Operators set this through the module's freeform JSON layer; the CLI value is the fallback when it is absent |

`firewall.backend` is not a registry default. The module pins it per backend in the generated reconcile unit's command line (`firewalld`, `nixos-nft`, or `iptables-nft`), and the CLI reads it only to refuse a backend the host does not actually provide. Setting it by hand changes which reconciliation path is attempted and fails closed on any mismatch, so prefer the module-generated unit over a hand-written override.


## Herdr client container defaults

The Herdr client uses a locally imported Incus image built by the repository flake:

```json
{
  "infrastructure": {
    "herdr": {
      "name": "aginctus-herdr",
      "image": {
        "alias": "aginctus-herdr-client"
      },
      "storage": {
        "pool": "default"
      }
    }
  }
}
```

`aginctus herdr client ensure` combines these values with
`incus.management.network.name`. The container is created without inheriting
Incus profiles: it receives an explicit root disk on the configured storage pool
and an explicit NIC on the configured management network.

The default image alias is produced by `nix build .#herdr-client` and imported
locally with `just update image herdr-client`. The image is NixOS 26.05 and
consumes Herdr from the published Herdr flake, with the exact revision pinned by
Aginctus's `flake.lock`. A systemd service starts `herdr server` in headless mode
when the container boots.

The image alias remains configurable through the normal Aginctus precedence
rules, allowing a separately built or promoted image to be selected without
changing orchestration code.


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
[`docs/firewall-host-backends.md`](firewall-host-backends.md) for the
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
