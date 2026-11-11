# Cobra command packages

The public `aginctus` command tree is composed by
`internal/cli/commands_basic.go`. Command registration lives in directories
named for the corresponding top-level CLI command and, where useful, nested
families. Each registration package owns flags, help text, argument validation
and Cobra command construction, while the `internal/cli` layer owns its
existing domain execution functions and dependency injection.

## Dependency direction

```
cmd/aginctus -> internal/cli (root + execution adapters)
                   |
                   +-> internal/cli/commands/{testenv,network,operator,...}
                   |          (Cobra syntax, handler interfaces)
                   +-> internal/{config,workload,gateway,testenv,...}
                              (domain behavior)
```

Subpackages must **not** import `internal/cli`: doing so would create an
import cycle and mix parsing with privileged execution. Avoid global Cobra
commands, `init()` registration and generic name-to-action switches.
Construct a new tree for each invocation; isolate stateful flag variables to
that invocation. Typed `Handlers` structs bind to the existing implementation
and keep side effects behind leaf-specific parsing and confirmation checks.

Do not place dedicated-worker operations under `aginctus testenv`: those
belong exclusively to the distinct operator-only `aginctus-testenv` binary.

See [`../ARCHITECTURE.md`](../ARCHITECTURE.md) for the broader root/operation/domain ownership and safety contract, tracked in issue #725.
