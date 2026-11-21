# aginctus

The primary Aginctus operator CLI for inspecting and managing Incus-backed agent workloads, networking, gateway services, and configuration. It uses Cobra; the installed help is the authoritative, version-specific syntax reference.

## Usage

```sh
go run ./cmd/aginctus help
go run ./cmd/aginctus help workload
go run ./cmd/aginctus help --examples environment
go run ./cmd/aginctus help --full gateway
go run ./cmd/aginctus <command> --help
```

High-level command families include `apply`, `remove`, `status`, `doctor`, `config`, `network`, `workload`, `herdr`, `gateway`, and `images`. Basic help is intentionally compact, listing immediate subcommands within a hard 35-line / 80-column layout. This layout is validated across registered commands in Go tests; overflow is a command-design failure, never a reason to hide subcommands or silently cut essential options. Use `help --full` for exhaustive inherited and local flags; `help --examples` shows reviewed examples when available. Subcommands and flags vary by family; consult full help before using a mutating action.

```sh
go run ./cmd/aginctus doctor
go run ./cmd/aginctus network ensure --dry-run
go run ./cmd/aginctus workload status --project aginctus dev
go run ./cmd/aginctus apply --render
```

Read-only previews do not authorize writes. Mutating commands may require explicit confirmation flags and ownership checks. Review [CLI reference](../../docs/reference/cli.md), [configuration](../../docs/reference/configuration.md), and [security boundaries](../../docs/reference/security.md) before operating on a host.

## Help contract

- `aginctus help [path]` and `<path> --help` provide navigational help limited to 35 lines of 80 terminal columns (including blank lines and hints).
- `aginctus help --full [path]` provides the complete Cobra-derived command and flag reference, including aliases and inherited flags. It has no page-length limit.
- `aginctus help --examples [path]` prints only explicitly authored examples. No potentially mutating example is inferred.
- Help is generated offline from the registered command tree; it must not load configuration, talk to Incus, or run command handlers.
- When adding commands or high-priority flags, update grouping or descriptions to satisfy the budget. Do not add truncation or pagination.
