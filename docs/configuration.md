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
locally with `just herdr-client-image-update`. The image is NixOS 26.05 and
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
  inputs.aginctus.url = "github:blevinn/aginctus";

  outputs = { self, nixpkgs, aginctus, ... }: {
    nixosConfigurations.lab = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        aginctus.nixosModules.default
        ({ pkgs, ... }: {
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
          };
        })
      ];
    };
  };
}
```

The module writes the declared settings to `/etc/aginctus/config.json`, preserving the same JSON shape and precedence semantics consumed by the CLI. Known settings are typed Nix options, while a freeform JSON layer allows newly introduced configuration keys to be represented without waiting for a module update. Runtime gateway secrets remain intentionally out of band and are not exposed as Nix configuration options.

When enabled, the module defines oneshot image lifecycle units for the Herdr client and OpenCode workload images. Each unit imports the corresponding image derivation through `aginctus images update` and uses a restart trigger on that derivation, so a NixOS generation containing a new image causes the unit to run again.

Optional boot reconciliation can be enabled with:

```nix
services.aginctus.reconcile = {
  enable = true;
  workloads = [ "dev" ];
};
```

This reconciles the management network, Herdr client, and selected workloads only after Incus and any enabled image update services have succeeded. The services include the host `incus` executable in their systemd `PATH`. An image import failure prevents dependent reconciliation instead of allowing workloads to start against an outdated alias. Reconciliation is disabled by default because it mutates Incus state.

The flake provides `packages.<system>.aginctus` (also `packages.<system>.default`) and the module selects that package by default. A consuming host can still override `services.aginctus.package` to pin or replace the CLI independently.

**Before merging the CLI packaging PR:** replace the temporary `vendorHash = pkgs.lib.fakeHash` in `flake.nix` with the real fixed-output hash produced by a Nix build on a trusted Nix-capable host:

```sh
nix build .#aginctus --print-build-logs
# Read the "got: sha256-..." value from the expected fixed-output hash mismatch.
# Replace vendorHash with that exact hash, then re-run:
nix build .#aginctus --print-build-logs
nix run .#aginctus -- version
nix flake check
```

Do not merge with the placeholder hash: the package will fail to build until it is replaced and validated.


### Module evaluation checks

The flake exposes a `checks.<system>.nixos-module` check, exercised by `nix flake check`. It evaluates the NixOS module with a harmless CLI stub and verifies typed configuration defaults and overrides, image service configuration, and reconciliation dependencies both with and without image imports enabled. It does not start Incus or modify a host. Real-world service activation and CLI package builds still require separate validation.
