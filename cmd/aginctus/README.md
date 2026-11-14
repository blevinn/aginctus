# aginctus

The primary Aginctus operator CLI for inspecting and managing Incus-backed agent workloads, networking, gateway services, and configuration. It uses Cobra; the installed help is the authoritative, version-specific syntax reference.

## Usage

```sh
go run ./cmd/aginctus help
go run ./cmd/aginctus help workload
go run ./cmd/aginctus <command> --help
```

High-level command families include `apply`, `remove`, `status`, `doctor`, `config`, `network`, `workload`, `herdr`, `gateway`, and `images`. Subcommands and flags vary by family; consult help before using a mutating action.

```sh
go run ./cmd/aginctus doctor
go run ./cmd/aginctus network ensure --dry-run
go run ./cmd/aginctus workload status --project aginctus dev
go run ./cmd/aginctus apply --render
```

Read-only previews do not authorize writes. Mutating commands may require explicit confirmation flags and ownership checks. Review [CLI reference](../../docs/reference/cli.md), [configuration](../../docs/reference/configuration.md), and [security boundaries](../../docs/reference/security.md) before operating on a host.
