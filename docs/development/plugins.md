# Public plugin and contribution contracts

**Status: experimental discovery, configuration and declarative contributions.**
This covers slices A1–A3 of the [stacked plan](https://github.com/blevinn/aginctus/issues/797#issuecomment-6871452026)
for [#798](https://github.com/blevinn/aginctus/issues/798). The broader
[architecture proposal](../design/plugin-oriented-core.md) remains the target,
not a claim that deployment plugins are already supported by the CLI.

## Available contract

`github.com/blevinn/aginctus/pkg/plugin` and `github.com/blevinn/aginctus/pkg/orchestration`
are importable from external Go modules without Aginctus implementation imports.
They depend only on public contracts and the standard library. The APIs are
experimental and may change incompatibly before stabilization. These are
in-memory contracts, not stable serialized plan/accepted-state/event schemas.

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
- `Descriptor.Dependencies` names required compiled plugin implementations.
  Descriptor validation rejects malformed, duplicate and self references;
  the host registry checks availability and cycles. These are not runtime
  instance dependencies and do not enable or authorize anything.
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

## Targets, images and criteria

`plugin.TargetContributor` is an optional interface extending `Plugin` with
`Targets(ctx, configuration)`. A1 discovery/configuration-only implementations
remain valid. Contribution construction is deterministic and I/O-free, using only
the supplied nonsecret configuration. It must not resolve images or load secrets.

`plugin.Contribute` validates configuration before calling the contributor and
checks cancellation, each declaration, duplicate target IDs and membership in
advertised capabilities. It returns no partial contributions on validation error.
An empty contribution is valid. Missing dependency references and graph cycles
are checked by host-side assembly, not by this public callback wrapper.

`orchestration.Target` describes a local ID, capability, target dependencies,
zero or more containers, and preconditions/postconditions/ongoing observations.
The host must qualify IDs and dependency references by deployment and instance;
these local names are not global resource identities or authority. Check IDs are
unique across all phases of one target. Definition validation does not schedule
or execute any operation or check, and no raw Incus/Apply/Compose payload is
exposed as a public resource model.

Each container has an independent `ImageReference` (Incus or OCI format and a
configurable pointer) and `GuestContract` (ID, version/range and descriptive
requirements for platform, helper protocols, services or layout). Pointer
validation checks only bounded nonempty shape, not exact backend syntax,
existence, provenance, compatibility or immutability. A custom image must satisfy
the same guest contract as the default. Resolution/pinning belongs to a later
authorized phase before accepting executable intent; these declarations never
build, pull or import images. Descriptive guest requirements are not compatibility
evidence: product integrations must add appropriate metadata/guest checks.

`orchestration.Check` declares its claim, evidence scope, required/optional policy,
effect, access classes, opt-in names, timeout, readiness polling interval and
maximum cache age. Offline checks cannot declare external access; inspection
declares its host/Incus/guest/network/credential needs. Active probes require
explicit opt-in names; live-model and tool execution cannot be disguised as
read-only inspection. These are requirements, not grants; the host must enforce
independent policy even if an implementation mislabels its behavior. Zero retry
interval means one attempt; zero maximum age disallows cached findings.

`orchestration.Finding` records local target/check correlation, passed/failed/unknown/
skipped outcome, observation time, diagnostic code, bounded safe text and optional
resource-incarnation/accepted-revision evidence. Unknown/zero outcomes cannot
silently become success. Valid findings are not necessarily fresh, correctly
correlated to current resources, authorized, or sufficient for convergence.
The host must carry deployment/instance scope alongside the local finding IDs.
Required failed/unknown/skipped results must block success in the later executor.
Text validation rejects oversized/control-character payloads, but does not scrub
secrets: producers must emit allowlisted nonsecret diagnostics.

## External-author example and validation

The independently named module under `pkg/plugin/testdata/external/` contains a
provider/MCP/gateway example plus an external consumer. It contributes a
container-free catalog target and a dependent two-container helper target, with
independently configurable image pointers and explicit criteria. The offline
consumer evaluates candidate schema validity and constructs a structured finding;
it does not perform the declared guest or live-inference probes. Its
local `replace` points to the checked-out Aginctus module. Go rejects direct
imports of Aginctus `internal/` packages from that module. Root tests also inspect
the public package's transitive imports so an indirect internal dependency cannot
silently undermine the boundary.

From the repository root, with its pinned development environment:

```sh
nix develop -c go test ./pkg/plugin/... ./pkg/orchestration/...
nix develop -c go vet ./pkg/plugin/... ./pkg/orchestration/...
```

The root package test explicitly runs the nested module with workspace mode and
module/toolchain downloads disabled and module files read-only. It is included
in ordinary `go test ./...`; no separate manual fixture command is required.
This check uses the local Go toolchain and writable Go build cache, not Incus,
host privileges, credentials, network services or live models.

## Host-side registration and graph assembly

`internal/plugins` is the application's product-neutral adapter. External plugins
continue using only public packages. The registry has no product imports, global
registration, ambient configuration, infrastructure clients or execution services.

- `New(ctx, implementations...)` validates and snapshots explicit descriptors,
  rejecting nil/duplicate implementations, incompatible APIs, missing compiled
  dependencies and cycles. `Inventory()` returns detached metadata in
  dependency-first order with lexical tie-breaking. Registration does not load
  code, establish verified provenance or instantiate dependencies.
- `Assemble(ctx, deployment, instances)` accepts an explicit logical deployment
  ID and selected instance names, plugin references and nonsecret configurations.
  It rejects unknown implementations and invalid/duplicate instance identities.
  Every selected configuration passes validation before any target contributor
  is invoked. Configuration validation may be repeated at the contribution
  boundary and must remain pure and deterministic.
- Discovery-only instances validate normally and contribute no targets. Omission
  is not deletion; an empty selection assembles an empty graph. Compiled-plugin
  dependencies do not imply runtime instance creation or execution ordering.
- Each target receives a separate `(deployment, instance, target)` key. Local
  definition/check IDs remain intact for public finding correlation. Dependency
  references resolve **only within the contributing instance**: there is no
  fallback to another instance's similarly named target. Cross-instance edges
  would require a future explicit scoped-reference contract.
- Assembly rejects incomplete/cyclic graphs and returns no partial graph on
  error or cancellation. Targets have deterministic dependency-first order with
  lexical scoped-key tie-breaking, independent of registration, instance input
  and target enumeration order. Definition order has no execution semantics.
- Registered metadata, inventory results and assembled nested declarations use
  detached slices. The registry copies caller configuration and does not retain
  candidate bytes in the graph. These ownership guarantees are not a sandbox;
  trusted implementations remain responsible for pure, cancellation-aware and
  appropriately concurrency-safe callbacks.

Logical deployment IDs are not Incus project bindings or principal credentials.
The assembled graph is a declaration snapshot, not accepted state, a scheduler,
resolved images, scoped runtime services or permission to perform a write.
Existing commands still use their current product paths. A4 must independently
bind services, enforce authorization/freshness/correlation and verify conditions.

Run the host-adapter tests with the public boundary checks:

```sh
nix develop -c go test ./internal/plugins/... ./pkg/plugin/... ./pkg/orchestration/...
nix develop -c go vet ./internal/plugins/... ./pkg/plugin/... ./pkg/orchestration/...
```

The host tests use public-contract fake implementations and a declaration-only
consumer. They verify scoped dependencies, independent image configuration,
deterministic ordering, metadata/data ownership and failure/cancellation behavior.
They do not deploy guests, evaluate runtime probes or import concrete products.

## Next slices

A4 introduces context-aware operation preconditions/postconditions; reporting,
selection/generation, Nix integration and product extraction follow in their own
reviewed PRs. The existing provider plugin machinery is not switched to this API
by these foundation slices, and #798 stays open until its remaining host-integration
acceptance criteria are delivered.
