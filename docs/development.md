# Development

Aginctus uses Nix to provide a small, reproducible development environment.

## Prerequisites

You need:

- Nix with flakes enabled;
- a Linux host capable of running Incus for integration work;
- an initialized Incus daemon when exercising Incus-backed functionality.

The development shell supplies the Incus client and repository tooling. It does not install or manage the host's Incus daemon.

## Enter the development shell

Run:

```sh
nix develop
```

The initial shell includes:

- `git`;
- `go`;
- `incus`;
- `jq`;
- `just`;
- `nixfmt`;
- `shellcheck`;
- `shfmt`.

The flake follows the stable `nixos-26.05` Nixpkgs branch. A committed `flake.lock` should record the exact revision used by the repository.

## CLI development

The initial `aginctus` CLI is written in Go. It uses the official Incus Go client to communicate directly with the local daemon API.

Run it directly with:

```sh
go run ./cmd/aginctus help
go run ./cmd/aginctus doctor
```

The `doctor` command connects directly to the local Incus daemon through the Incus Go client and reports the server version. It does not create or modify Incus resources.

The first mutating command is:

```sh
go run ./cmd/aginctus network ensure
```

It creates or reconciles the configured management bridge. Network name, addressing, NAT, routing, and IPv6 behavior come from the effective configuration; Aginctus ownership metadata remains an internal safety invariant.

Use `--dry-run` to inspect the action without mutating Incus:

```sh
go run ./cmd/aginctus network ensure --dry-run
go run ./cmd/aginctus network teardown --dry-run
```

Use `network teardown` to remove the configured management bridge:

```sh
go run ./cmd/aginctus network teardown
```

Both `ensure` and `teardown` accept `--force`. Without it, Aginctus refuses to mutate a same-named bridge that lacks Aginctus ownership metadata. With `--force`, Aginctus may adopt or delete that same-named Incus-managed bridge. `--force` does not allow Aginctus to reinterpret an unmanaged network or a different network type.

The next infrastructure slice can be exercised with:

```sh
go run ./cmd/aginctus herdr-client ensure --dry-run
go run ./cmd/aginctus herdr-client ensure
go run ./cmd/aginctus herdr-client teardown --dry-run
go run ./cmd/aginctus herdr-client teardown
```

The Herdr client instance is an Aginctus-owned container attached to the
configured management network. It does not install Herdr, SSH credentials, or
machine configuration yet; this slice establishes only the container lifecycle
and resource ownership boundary.

Both lifecycle operations accept `--dry-run` and `--force`. Teardown stops a
running owned container before deleting it. With `--force`, the stop request is
also forced and a same-named container may be adopted or removed despite missing
Aginctus ownership metadata, but a same-named VM is still rejected.

Common development commands are available through `just`:

```sh
just build
just test
just vet
just check
just fmt
```

`just fmt` formats both Go and Nix sources.

## direnv

If you use direnv with nix-direnv support, the repository includes an `.envrc`:

```sh
direnv allow
```

The generated `.direnv/` directory is ignored by Git.

## Incus development

The `incus` command in the development shell is only the client. It expects to connect to a usable Incus daemon.

Development code should not assume the daemon's storage pool, network names, or other host-specific defaults. Tests and future bootstrap commands should create or discover the resources they need explicitly.

Do not point destructive development commands at Incus resources that are not clearly owned by Aginctus.
