# Cobra command packages

The public `aginctus` command tree is assembled afresh by
`internal/cli/commands_basic.go`. Registration is owned by
`internal/cli/commands/<family>`; each package owns flags, help,
syntax/confirmation validation and presentation. `internal/cli`
provides the typed execution capabilities and dependencies.

## Dependency direction

```text
cmd/aginctus -> internal/cli
                  |
                  +-> internal/cli/commands/* (Cobra parsing, typed callbacks)
                  +-> internal/cli/operations/* (application behavior)
                                      |
                                      +-> internal/<domain>/*
```

Registration packages must never import `internal/cli` or its operation
packages, and must not open files, run host commands, or register global Cobra
command trees. The runtime composes capabilities at individual command
leaves, after argument and confirmation validation. This dependency rule is
exercised by the CLI architecture tests.

The gateway asset family still composes some provider/model/MCP/catalog
actions using an injected `gatewaycontrol.Store`. That coupling is
documented as follow-up architecture debt under #755; do not use it as a
precedent for adding new store or OS access in registration packages.
Configuration-file reads are delegated to an injected gateway operation.

Do not place dedicated-worker operations under public `aginctus testenv`:
they belong to the operator-only `aginctus-testenv` binary.

See [`../ARCHITECTURE.md`](../ARCHITECTURE.md) for the root inventory,
privilege boundaries, completion criteria and justified exceptions.
