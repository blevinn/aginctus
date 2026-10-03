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
