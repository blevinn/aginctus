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
locally with `just herdr-client-image-update`. The image is NixOS 26.05 with
Herdr 0.9.3 included in the system closure. A systemd service starts
`herdr server` in headless mode when the container boots.

The image alias remains configurable through the normal Aginctus precedence
rules, allowing a separately built or promoted image to be selected without
changing orchestration code.
