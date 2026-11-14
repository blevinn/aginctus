# Public plugin API: first foundation

**Status: experimental discovery and configuration-validation contracts only.**
This implements slice A1 of the [stacked plan](https://github.com/blevinn/aginctus/issues/797#issuecomment-6871452026)
for [#798](https://github.com/blevinn/aginctus/issues/798). The broader
[architecture proposal](../design/plugin-oriented-core.md) remains the target,
not a claim that deployment plugins are already supported by the CLI.

## Available contract

`github.com/blevinn/aginctus/pkg/plugin` is importable from external Go modules
without importing Aginctus implementation packages. It uses only the Go standard
library. Its API is experimental and may change incompatibly before stabilization.

- `Plugin.Descriptor` supplies a versioned implementation identity and named
  capabilities without configuration or I/O. Identity is case-sensitive; discovery
  does not enable anything. One plugin can advertise several capabilities,
  including several named capabilities of the same kind.
- Capability kinds currently cover agents, gateways, UIs/control clients, MCP
  and model providers. Kinds describe integrations, not container counts or grants.
  A provider/MCP implementation need not have any local container.
- `Descriptor.Validate` rejects incompatible API versions, malformed names,
  invalid implementation versions, missing capabilities, duplicate capability
  names and unknown kinds. Implementation versions are opaque printable ASCII
  identifiers without spaces, not semantic-version constraints or verified provenance.
- `Plugin.ValidateConfiguration` checks the implementation's nonsecret JSON
  configuration schema. It must reject unknown fields/invalid combinations,
  cooperate with context cancellation and emit safe errors. It must not perform
  I/O, inspect guests, resolve images, read ambient configuration, load secrets
  or mutate resources.
- `plugin.Validate` checks cancellation, descriptor compatibility and a single
  non-null JSON object envelope before calling the implementation. It checks
  cancellation again afterward, retains plugin error causes, and does not report
  success for a callback that returns nil after cancellation. Interrupting a
  blocking callback still requires implementation cooperation.

The host must use the validation boundary before consuming configuration.
Neither the JSON envelope check nor the descriptor check validates a plugin's
complete schema: that responsibility remains with the implementation. These
checks do not register instances, authenticate callers or authorize execution.
Compiled plugins are trusted in-process Go code, not sandboxed extensions.

## External-author example and validation

The independently named module under `pkg/plugin/testdata/external/` contains a
provider/MCP discovery and configuration example plus an external consumer. Its
local `replace` points to the checked-out Aginctus module. Go rejects direct
imports of Aginctus `internal/` packages from that module. Root tests also inspect
the public package's transitive imports so an indirect internal dependency cannot
silently undermine the boundary.

From the repository root, with its pinned development environment:

```sh
nix develop -c go test ./pkg/plugin/...
nix develop -c go vet ./pkg/plugin/...
```

The root package test explicitly runs the nested module with workspace mode and
module/toolchain downloads disabled and module files read-only. It is included
in ordinary `go test ./...`; no separate manual fixture command is required.
This check uses the local Go toolchain and writable Go build cache, not Incus,
host privileges, credentials, network services or live models.

## Next slices

A2 introduces public target, image and check contributions and extends the fixture
to exercise them. A3 adds instance registration and validated contribution
assembly. Context-aware operation preconditions/postconditions, reporting,
selection/generation, Nix integration and product extraction follow in their own
reviewed PRs. The existing provider plugin machinery is not switched to this API
by A1, and #798 stays open.
