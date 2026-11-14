# aginctus-testenv

Separate operator-only Cobra CLI for dedicated, isolated test-environment worker operations. Its worker actions are not part of the public `aginctus` CLI or MCP interface.

## Usage

```sh
go run ./cmd/aginctus-testenv --help
go run ./cmd/aginctus-testenv worker --help
go run ./cmd/aginctus-testenv worker bootstrap --help
go run ./cmd/aginctus-testenv worker preflight --help
go run ./cmd/aginctus-testenv worker acceptance --help
go run ./cmd/aginctus-testenv worker sweep --help
```

High-level syntax is `aginctus-testenv worker <bootstrap|preflight|acceptance|sweep> [flags]`; use each leaf's help for required arguments and supported bootstrap subcommands. Bootstrap planning is offline; worker preflight, acceptance and sweep have distinct authority and safety requirements. **Do not run mutating worker operations on a multipurpose or pilot host.** Consult the [dedicated worker operations guide](../../docs/testing/testenv/testenv-worker-operations.md) and [worker trust contract](../../docs/testing/testenv/testenv-worker.md) before use.
