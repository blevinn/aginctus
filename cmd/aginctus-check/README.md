# aginctus-check

Developer/CI-only hermetic validation runner and bounded evidence reporter. Run from the repository root (where `justfile` exists); this is **not** the product CLI.

## Usage

```sh
go run ./cmd/aginctus-check [--format text|json] [--verbose]
go run ./cmd/aginctus-check --vm-run-id RUN_ID [--format text|json]
go run ./cmd/aginctus-check --acceptance-run-id RUN_ID [--format text|json]
go run ./cmd/aginctus-check --coverage [EVIDENCE_SOURCE_FLAGS] [--format text|json]
```

With no evidence selector, runs the hermetic checks. `--verbose` streams unfiltered diagnostics for trusted local use and cannot be combined with JSON or evidence reporting. VM and acceptance modes inspect **existing** private receipts; they do not start tests. Coverage composes selected existing evidence with flags such as `--hermetic-json`, `--coverage-vm-run-id`, `--pilot-topology-json`, `--opencode-runtime-json`, and `--local-acceptance-run-id`.

Evidence modes are mutually exclusive; coverage sources require `--coverage`. Partial acceptance and coverage reports intentionally return nonzero and must not be treated as live sign-off. See [testing documentation](../../docs/testing/README.md).
