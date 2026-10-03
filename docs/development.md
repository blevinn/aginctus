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
- `incus`;
- `jq`;
- `just`;
- `nixfmt`;
- `shellcheck`;
- `shfmt`.

The Nixpkgs input is pinned to a NixOS 26.05 revision so developers resolve the same tool set.

## direnv

If you use direnv with nix-direnv support, the repository includes an `.envrc`:

```sh
direnv allow
```

The generated `.direnv/` directory is ignored by Git.

## Formatting Nix files

The flake exports `nixfmt` as its formatter:

```sh
nix fmt
```

As implementation code is introduced, language-specific formatters, linters, test commands, and developer dependencies should be added to this environment rather than relying on undocumented host tooling.

## Incus development

The `incus` command in the development shell is only the client. It expects to connect to a usable Incus daemon.

Development code should not assume the daemon's storage pool, network names, or other host-specific defaults. Tests and future bootstrap commands should create or discover the resources they need explicitly.

Do not point destructive development commands at Incus resources that are not clearly owned by Aginctus.
