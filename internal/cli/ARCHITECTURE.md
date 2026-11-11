# CLI package architecture

This document tracks the second-phase extraction in [#725](https://github.com/blevinn/aginctus/issues/725). The Cobra migration in #642 intentionally moved command **registration**, not all command **execution**; a large root package remains.

## Package ownership

| Package | Owns | Must not own |
| --- | --- | --- |
| `internal/cli` | Fresh root construction, entry-point `Run`, config-loader wiring, typed injection of handlers, exit/output compatibility | Incus resource algorithms, privileged firewall reconciliation, gateway/worker domain logic |
| `internal/cli/commands/<family>` | Cobra trees, leaf flags, help, syntax/confirmation validation | Privileged I/O, import of `internal/cli`, reusable orchestration |
| `internal/cli/operations/<family>` | CLI-specific application actions, typed requests/results, injected side-effect interfaces | Cobra parsing, global command trees, authorization bypasses, duplicate domain models |
| `internal/{incus,firewall,gateway,workload,...}` | Ownership policies, native reconciliation, domain algorithms | Cobra flags, CLI exit-code rendering |

The existing `internal/cli/commands/README.md` remains the reference for command registration. Small shared presentation helpers can live in `internal/cli/help`, without dragging runtime dependencies into the Cobra tree.

## Dependency and safety rules

1. Root assembly may import command packages and operation packages. Command packages must not import `internal/cli` or operation implementations; operation packages must not import `internal/cli` or `github.com/spf13/cobra`.
2. Pass side-effect capabilities explicitly to operations (typed functions or interfaces). Construct command trees afresh for each invocation, not in `init()`. Preserve test injection points until callers/tests move.
3. Syntactically invalid commands and `--help`/`version` must be side-effect free. Do not load configuration or inspect Incus before Cobra validation. Maintain command names, aliases, output/exit semantics and legacy spelling translations.
4. Project verification, read-only previews, explicit `--confirm-*` guards, leases, exact fingerprint checks, error redaction and fail-closed behavior are part of the API contract. Extract them without widening authority.
5. The operator-only `aginctus-testenv worker` surface must remain distinct from `aginctus testenv` and MCP. No dedicated-worker operations can be registered on the public tree.
6. Prefer domain ownership over `operations/` when code is reusable by other runtimes; do not duplicate accepted-state/native orchestration policy.

## Migration plan and completion gate

1. Record dependencies and preserve behavior through existing CLI help/syntax/exit tests and guardrails.
2. Move root/help/config/doctor/provenance helpers behind explicit interfaces.
3. Extract aggregate, workload and Herdr application handlers.
4. Extract gateway and image coordination while preserving leases and post-write checks.
5. Extract network/operator privileged operations with fail-closed verification.
6. Remove dead compatibility parsing and consolidate the root; keep integration tests at the root, move unit tests with their implementation.

Baseline (2026-11-09): 124 direct Go files (57 implementation, 67 tests), plus `VERSION`. Desired end-state: approximately 10–15 Go files **only if** each file has a justified composition/integration role. No issue closure until the final root inventory and full Go/Nix/CI gates are verified. Stacked PRs must remain independently reviewable and must not be merged automatically.
