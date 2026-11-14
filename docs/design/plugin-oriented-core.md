# Plugin-oriented core, validation and observable orchestration

**Status: proposed architecture and implementation plan.** This page records the
agreed direction for [epic #797](https://github.com/blevinn/aginctus/issues/797).
It introduces no runtime functionality, public Go API, new command, configuration
key, image output or permission grant. Names and examples below describe the
target design; exact exported signatures and wire schemas require implementation
review. Release targeting is unassigned.

## Purpose and scope

Make the source tree reflect architectural responsibility: a reusable core owns
infrastructure, state convergence, validation and reporting; plugins own
product-specific integrations. Support externally implemented Go plugins from the
start, selected statically when building the executable. Treat preconditions,
postconditions, structured events and status as fundamental contracts rather than
CLI-specific conveniences.

The design must deliver usable increments without interrupting the existing
native reconciliation, accepted-state and CLI extraction work. It does not
require a bulk rename, a new orchestration backend, a runtime plugin loader, an
event-sourcing database, or a dashboard before the first plugin works.

### Established constraints

1. Concrete integrations live under root `plugins/`; public contracts cannot
   require imports from `internal/`.
2. One plugin may supply agent, gateway, UI/control-client, MCP and model-provider
   capabilities. Capabilities may use zero, one or several containers.
3. Every container-backed capability permits configured image pointers. A plugin
   specifies its image compatibility contract and may publish matching container
   specifications; its Go integration is not tied to that particular image build.
4. Nix makes default images, custom image derivations and existing image
   references natural deployment inputs, with the appropriate build dependencies.
5. Preconditions and postconditions define whether an operation can proceed and
   whether a target has converged. A successful mutation call is insufficient.
6. Structured events and independently inspectable status support CLI rendering,
   logs and future tooling without parsing prose.
7. Plugin inclusion, runtime enablement, a passing check and a resource name are
   never substitutes for independent execution authorization.

## Current implementation and coordination

The current [orchestration contract](../architecture/orchestration.md) uses strict
ordered plans, complete driver validation before execution, and Apply, Compose
and native integration. It is not yet the dependency/condition model below.
`Driver.Validate` currently lacks a context parameter; cancellation through
preflight remains tracked in [#348](https://github.com/blevinn/aginctus/issues/348).

Existing responsibilities are mixed:

- `internal/gateway` contains LiteLLM behavior alongside host firewall, DHCP,
  routing, credential attachment and topology logic.
- `internal/herdrclient` builds product-specific plans, while
  `internal/orchestration/resources` embeds both shared and product resources.
- `internal/gatewaycontrol` defines provider plugin hooks and concrete builtins;
  `internal/plugins/openai-siwc` contains a concrete implementation.
- Provider/model registries, MCP schemas, accepted state, private file helpers,
  Incus volumes and validation evidence each have distinct responsibilities.
  Similar names do not justify merging their authority or persistence models.

Coordinate, rather than replace:

| Existing work | Relationship |
| --- | --- |
| [CLI extraction #725](https://github.com/blevinn/aginctus/issues/725), [gateway/images #753](https://github.com/blevinn/aginctus/issues/753), [host operations #754](https://github.com/blevinn/aginctus/issues/754), [boundary audit #755](https://github.com/blevinn/aginctus/issues/755) | Preserve command/operation separation; update import rules together as domain owners move |
| [Native reconciliation #153](https://github.com/blevinn/aginctus/issues/153) and [native design](native-incus-reconciliation.md) | Reuse native primitives; plugin extraction does not claim global native cutover |
| [Three-layer state #693](https://github.com/blevinn/aginctus/issues/693) and [state design](three-layer-state.md) | Keep configured, accepted and current state separate; reconcile pinned accepted intent as those paths become available |
| [Authority #704](https://github.com/blevinn/aginctus/issues/704) and [controller ownership](controller-ownership.md) | Retain write-time scope/ownership/fencing requirements and restrictions on cross-principal execution |
| [Gateway declarations #496](https://github.com/blevinn/aginctus/issues/496) | Registry declarations remain distinct from deployment, route activation and grants |
| [Validation catalog ADR](../adr/0003-validation-catalog.md) and [testing index](../testing/README.md) | Reuse evidence classifications; runtime checks do not redefine acceptance tiers |

## Package ownership and dependency direction

Proposed layout:

```text
pkg/
  plugin/                  Public plugin descriptors and capability contracts
  orchestration/           Minimal public target/resource/condition vocabulary
  observability/           Public reporting envelopes and reporter interfaces

internal/
  cli/                     Commands, presentation and application assembly
  config/                  Loading, precedence and core configuration validation
  host/                    Host capabilities and host-side operations
    net/                   Host network policy, firewall and routing adapters
  incus/                   Incus access and reusable resource lifecycle primitives
  orchestration/           Planning, dependency execution and convergence engine
    state/                 Control-plane state coordination and persistence
  plugins/                 Registry and application-side plugin integration
  observability/           Delivery, status projections and logging adapters

plugins/
  herdr/                   UI/control-client capability and optional image specs
  opencode/                Agent capability and optional image specs
  litellm/                 Gateway capability and multi-container topology
  openai-siwc/              Provider/authentication integration
  opencode-go/              Provider integration
```

`pkg/orchestration` is the proposed home for the small public vocabulary, not a
commitment to publish the existing engine or every internal driver type. The
public API slice must prove the boundary with an external-module fixture before
settling exact package splits.

Dependency rules:

- Public packages depend only on public contracts and deliberately selected
  dependencies. Public function signatures and required extension points must
  not indirectly require internal types.
- Concrete plugins depend on public contracts, not internal packages. Enforce
  this for in-tree plugins even though Go permits some same-repository imports.
- Internal registries and executors consume public contracts and inject adapters
  to host/Incus/state implementations. Generic core packages do not import
  concrete plugins. Generated registration belongs at application composition.
- CLI operation packages coordinate typed application requests; renderers consume
  status/events. Commands do not become infrastructure implementations.
- Dependency cycles are rejected. Shared behavior moves to its actual owner,
  not a miscellaneous utility package introduced to bypass an import rule.

### Responsibility-level migration map

| Current area | Intended ownership / decision |
| --- | --- |
| `firewall`, host portions of `gateway` | `host/net` policy and backend subpackages |
| `managementnetwork` | Split host capability/policy from Incus network resources and orchestration contributions |
| `incus`, resource reconciliation primitives | `incus` owns reusable Incus operations; engine-specific adapters may remain under orchestration |
| `acceptedstate` | Public nonsecret intent types only where needed; implementation/state-service coordination under `orchestration/state`, retaining existing schema/storage semantics |
| `statevolume` | Incus volume lifecycle/attachment primitives, not accepted-state storage |
| `filestate` | Keep a cohesive private-file protocol owner; relocate only after auditing consumers, without changing secret owners |
| `workload` | Generic target/resource assembly in core; OpenCode-specific behavior under its plugin |
| `herdrclient` | `plugins/herdr`, reusing core infrastructure |
| `gateway`, `gatewaycontrol` | Product topology/authentication/rendering in plugins; generic capability/catalog/configuration orchestration in core |
| `providerregistry`, `modelregistry`, `mcpschema` | Classify public declaration contracts separately from persistence and concrete provider/MCP behavior |
| `oauthgateway`, `responsescontract` | Separate reusable protocol contract from provider-specific runtime; do not force a move based on directory count |
| `testenv`, `validation` | Retain cohesive execution/evidence tooling where justified; adapters may reuse public conditions/reporting without acquiring production authority |

These are ownership destinations, not an instruction to move every listed
package in one PR. The completion audit records justified specialist packages.

## Public plugin model

### Identity, configuration and capabilities

A plugin descriptor identifies its stable name, implementation version, supported
API contract, capability contributions and required plugin/capability dependencies.
Plugin identity differs from a configured instance identity: a compiled plugin can
have multiple independently configured instances, each with stable target IDs.
Resource naming must include explicit deployment scope rather than rely on a
global plugin name.

Prefer small optional capability interfaces to one interface containing every
possible operation. Initial capability families are:

| Capability | Typical contribution |
| --- | --- |
| Agent | Workload topology, launcher/provisioning contract and readiness criteria |
| Gateway | Routing/service topology, persistence and network requirements |
| UI/control client | Client/service topology and connection requirements |
| MCP | Server declaration, transport/schema, optional deployment and explicit access requirements |
| Model provider | Authentication lifecycle, model discovery and provider metadata; no local container required |

A LiteLLM gateway can contribute both a gateway container and PostgreSQL. An
OpenCode plugin need not require Herdr. A remote MCP server or provider can
contribute checks and metadata without creating an Incus instance.

Plugin configuration is namespaced per instance, validated strictly by the
owning plugin, and normalized before planning. Core owns precedence and scope;
plugins must not independently reread ambient configuration during execution.
Unknown plugins/capabilities, ambiguous identities, incompatible contract versions
and invalid configuration fail before mutation. Plugin-specific settings do not
become an unvalidated escape hatch for arbitrary Incus configuration or host code.

### Contributions and explicit dependencies

Plugins describe desired resources, target dependencies, conditions and product
operations. Core adapters implement shared infrastructure reconciliation.
Provider login, guest provisioning and discovery can require operational hooks;
these receive explicit scoped services and a context instead of constructing an
ambient administrator client.

The public vocabulary should cover stable resource identity, image selection,
storage/network requirements and operation/check descriptions. It should not
freeze Apply/Compose payloads as the universal plugin interface. The first slice
must demonstrate both a container-backed target and a container-free contribution
without publishing an oversized generic workflow framework.

Public Go API versions and serialized plan/event schema versions are separate.
Start with a documented experimental compatibility policy, explicit version
rejection and additive evolution where possible. API-breaking changes must be
intentional and reflected in external-plugin fixtures. Dependency registration
order must not silently become workflow execution order.

### Trust and authorization

Compiled plugins are trusted executable code in the Aginctus process. Scoped
interfaces reduce accidental coupling; they do not sandbox malicious Go code.
External plugin selection is therefore a build/supply-chain trust decision.

Core infrastructure adapters retain independent principal, project, selected
resource, ownership, accepted-revision and mutation-boundary checks. A plugin can
declare a required network or MCP grant; it cannot approve that requirement on
the operator's behalf. Existing [controller authority gates](controller-ownership.md)
remain in force. No root-capable user proxy or cross-principal merge is introduced.

## Build-time selection and runtime instances

Use a CoreDNS-inspired build manifest that names selected builtin or external
Go plugin packages. A generator produces deterministic imports and explicit
registration at the application composition point. Ordinary Go module/version
pinning supplies external dependencies; there is no runtime download or dynamic
shared-library loader.

Required properties:

- Duplicate identities, missing dependencies, incompatible APIs and dependency
  cycles fail clearly. Ordering is explicit and deterministic, not incidental
  `init()` order.
- Default builds preserve supported integrations. Minimal/custom builds include
  only the selected implementation wiring and required assets/dependencies.
- The executable exposes a structured compiled inventory with plugin identity,
  version and capabilities. Runtime references to absent plugins fail early.
- Generation, `go.mod`/`go.sum`, vendoring and Nix dependency hashes stay aligned;
  builds must not edit module dependencies opportunistically.
- Runtime configuration creates/enables instances of compiled capabilities.
  Compilation does not enable instances, deploy containers, grant model access,
  configure egress, or authorize MCP execution.

Exact manifest syntax and inventory command/API are implementation decisions,
not newly supported configuration or CLI syntax in this document.

## Image contracts and Nix integration

### Configurable pointers and compatibility

Every container in a capability has a configurable image pointer. Plugins may
provide default pointers and optional matching build specifications, but the
integration depends on a documented guest contract: supported platform/instance
type, executables/services, filesystem layout, helper protocol, version range and
readiness behavior. Custom compatible images are first-class inputs.

Resolve mutable aliases/tags to immutable Incus fingerprints or OCI digests, as
appropriate to the backend, before accepting executable deployment intent. Keep
the configured pointer as provenance; execute against the pinned identity.
Re-resolving a tag is a proposed configuration change, not permission to replace
an accepted image silently. Check metadata compatibility before mutation when
possible; guest-only checks run after boot before dependent operations proceed.
An unverifiable required contract is unknown, not compatible by assumption.

A plugin may ship `images/` with product-specific Nix definitions and helpers.
Reusable image construction/import machinery stays in shared Nix/core tooling.
Multi-container capabilities expose separate pointers and contracts for each
container, not one opaque image override for the entire plugin.

### Seamless Nix wiring

| Input | Nix responsibility | Execution responsibility |
| --- | --- | --- |
| Plugin-supplied container spec | Build the matching image when a requested artifact needs it; wire its output into deployment configuration | Explicit import/availability preparation, identity resolution and authorized convergence |
| Operator-supplied image derivation | Accept the derivation through the same image interface and include the correct dependency | Apply the same compatibility/ownership checks as a default image |
| Existing Incus image reference | Generate reference configuration without a redundant local image build | Resolve/inspect it in the explicitly selected project/remote context |
| Published OCI image reference | Preserve reference/digest and required acquisition metadata | Resolve/pull only in an appropriate authorized phase, with network requirements explicit |

Nix should avoid manual transcription of build output paths into configuration.
Requested deployment artifacts include the selected images they need; building
the CLI alone need not build every container. An optional deployment bundle can
aggregate the selected binary, image outputs and nonsecret candidate settings.
The manifest selects Go integrations; image choice remains instance/deployment
configuration, even when Nix wires both together.

Build, image import and resource convergence are distinct effects. Preserve
existing opt-in controls and make imports/dependencies explicit. NixOS may still
activate its own host policy under OS authority. A Home Manager configuration
does not thereby gain host permissions. For accepted-state paths, building or
switching a candidate must not implicitly promote it or reconcile resources;
existing effective-config reconcilers require explicit migration under
[#699](https://github.com/blevinn/aginctus/issues/699). This proposal does not claim
that those existing paths already have accepted-state behavior.

Retain current flake outputs and configuration keys through compatibility aliases
or an explicitly reviewed migration. Unsupported platforms, missing plugin
dependencies and invalid image settings should fail as early as their evidence
permits; evaluation must not require live Incus access or runtime secrets.

## First-class validation and convergence

### Four kinds of criteria

| Kind | Question | Example |
| --- | --- | --- |
| Configuration validation | Is the requested intent well formed and internally consistent? | Image pointer exists; capability options are compatible |
| Preconditions / preflight | May this specific operation proceed now? | Resource ownership and network prerequisites hold |
| Postconditions / postflight | Did the operation establish the required result? | Correct image is running; required guest helper is ready |
| Ongoing observation | Does the current target still satisfy its contract? | Configuration drift or lost readiness |

Conditions have stable IDs, scope, blocking policy, evidence claim, execution
requirements and freshness expectations. Checks produce structured findings:
passed, failed, unknown or skipped; an observation timestamp; bounded diagnostic
codes/details; and actionable remediation. Evaluation errors can be represented
as unknown with a diagnostic cause, rather than inventing a positive result.
Configuration errors remain errors, not optional runtime warnings.

Required failed/unknown/skipped conditions block success. A genuinely inapplicable
condition is excluded explicitly during plan construction with an explanation;
skipping an applicable required check is not a bypass. Optional diagnostics do
not block but remain visible. Cached observations must match the resource
incarnation, accepted revision where applicable, and freshness contract.

### Execution sequence

```text
Resolve and validate selected intent; pin accepted revision where supported
    → construct/validate complete target graph and checks
    → run feasible plan-wide preflight before mutation
    → for each eligible target:
        wait for dependency conditions
        recheck operation preconditions at mutation boundary
        execute authorized operation
        observe and evaluate required postconditions
    → evaluate deployment-wide postflight
    → publish final result and status, including partial/uncertain outcomes
```

Some prerequisites only become true after a dependency executes. Mark these as
deferred to their dependency boundary rather than failing global preflight or
claiming they already passed. Validate their definitions and dependencies before
any mutation. Preserve the current complete-plan validation guarantee.

The initial scheduler can stay sequential in a deterministic topological order.
Reject cycles and missing dependencies. Concurrency is not required for this arc;
introduce it only with explicit shared-resource/exclusion semantics. A dependent
target is eligible after its required dependency postconditions pass, not merely
after a successful create/start response.

Use context-aware validation/check/operation interfaces. Cancellation stops new
work and bounded waits, propagates through supporting APIs, and never reports
canceled work as successful. Readiness polling has explicit deadlines, intervals
and attempt identities. Do not automatically retry destructive/non-idempotent
operations because a readiness check timed out.

### Effects, failure and evidence

Check descriptors distinguish offline computation, read-only host/Incus/guest
inspection, active network probes, credentialed operations and live-model/tool
requests. Transport reads and active probes are not necessarily effect-free.
Declare required credentials, endpoints, cost/network implications and opt-ins.
Offline rendering cannot silently pull images or contact a provider. A read-only
status command cannot silently provision secrets or repair drift.

The executor retains independent write-time policy/ownership checks even when a
plugin omitted a criterion or an earlier preflight passed. Observe/plan/write
must retain appropriate exclusion, fencing and ETag checks. Preflight is evidence,
not enduring authorization.

Postflight failure means not converged; record what changed and block dependents.
Cancellation or connection loss after a write can leave completion uncertain.
Re-observe during recovery before retrying. Compensation/rollback is an explicit,
authorized workflow when supported, never an assumed cross-resource transaction.

A running instance, a healthy HTTP endpoint, scoped-key enforcement, working
upstream inference and a genuine model-directed coding task establish different
claims. Preserve those distinctions in findings and evidence references. Runtime
checks may adapt existing catalog/evidence tooling without reclassifying a green
hermetic test as live acceptance.

## Structured observability

### Events and snapshots

Events describe transitions within a run. Status snapshots describe current
knowledge independently of whether a consumer saw the transitions. Expose the
target graph, operation/check definitions and current conditions so tooling can
render both a planned workflow and its execution.

Proposed common metadata:

| Field group | Purpose |
| --- | --- |
| Schema version, event kind, diagnostic code | Stable machine interpretation without parsing prose |
| Run, deployment, target, operation, check and attempt IDs | Correlation and unambiguous retries |
| Parent/dependency links and accepted revision when present | Explain orchestration structure and bind evidence to intent |
| Per-run sequence and timestamps | Ordering within a run and elapsed/observation timing |
| Phase, outcome, reason, evidence scope and freshness | Honest lifecycle and condition reporting |
| Bounded safe message/details | Human presentation without leaking arbitrary runtime data |

Core emits run/target lifecycle transitions, dependency blocking, check outcomes,
operation results, retries, cancellation and convergence. Plugins contribute
domain progress/findings through the public reporter; they do not decide that
the overall target succeeded. Distinguish run completion from current health:
a previously converged target can later drift or become unobservable.

Snapshots include desired intent identity, latest observed resource incarnation,
phase, outstanding conditions and observation time. Failed inspection makes
status unknown/stale, rather than silently retaining healthy state or claiming
the resource is absent. A new consumer must be able to inspect a snapshot without
replaying the complete process lifetime.

### Delivery and consumers

Use a small public reporter interface and internal delivery/projection machinery.
Execution code does not print directly; CLI views and structured log sinks consume
the same vocabulary. Preserve existing stable CLI output unless a format change
is explicitly reviewed. No new TUI/server is necessary for the first slice.

The observability slice must implement and test these semantics:

- Establish a per-run ordered sequence at the core collector. No fictional total
  ordering across independent runs; wall clocks alone are not ordering keys.
- Provide a snapshot watermark/cursor. Subscriptions either continue after that
  watermark or explicitly report a gap and require a fresh snapshot.
- Bound queues and payload sizes. Slow optional sinks cannot block convergence
  indefinitely; signal dropped progress/gaps. Preserve final status for inspection
  through the authoritative run/state result rather than relying on one final
  best-effort event arriving.
- Make subscription cancellation, observer errors and terminal delivery behavior
  explicit. Do not promise durable or exactly-once event delivery. In-process
  projections must apply lifecycle updates consistently before publication.
- Define retention and access scope. Initial process-local progress can be
  ephemeral; restart-visible run records require the state/journal integration.
  A subscriber must not learn another principal's protected state from a shared
  stream.

### Reporting versus durable state

Best-effort telemetry is not the operation journal. Required durable records,
leases, accepted revisions and recovery outcomes belong to `orchestration/state`
and the existing state-service arc. If a required journal write fails, the
executor follows that storage contract; it must not swallow the failure as an
optional log error. Conversely, losing a terminal renderer or optional logging
sink must not change convergence success.

Snapshots combine observed state and relevant run metadata, but events do not
become authority to mutate resources. This arc does not require event sourcing
or treat replayed progress as proof of current health.

### Safe payloads

Use typed, allowlisted diagnostic data rather than arbitrary configuration maps,
HTTP requests, environment dumps or guest stdout/stderr. Keep credentials,
authorization headers, tokens and secret file contents out of generic events,
findings and snapshots. Secret references can also reveal sensitive paths and
must be filtered by audience. Scrub unexpected errors before public emission;
escape control characters in terminal/log rendering. Test redaction and bounded
payloads with representative failure paths, not only successful progress.

## Delivery sequence and tracking

Each issue may need several independently useful PRs. Prefer fewer than ten
changed files per PR, but preserve functional completeness. Move tests with code,
retain compatibility adapters until replacements work, and remove replaced
wiring only after parity is established. These dependencies are functional gates,
not a requirement to serialize all research or implementation.

| Slice | Dependencies and completion boundary |
| --- | --- |
| [#798 Public contracts](https://github.com/blevinn/aginctus/issues/798) | Design review; external-module fixture proves no internal dependency before API expansion |
| [#799 Core ownership](https://github.com/blevinn/aginctus/issues/799) | Public boundary agreed; behavior-preserving host/Incus/state moves coordinated with active CLI work |
| [#800 Conditions](https://github.com/blevinn/aginctus/issues/800) | Public target/check vocabulary; complete-plan validation, cancellation, dependency/postcondition semantics |
| [#801 Observability](https://github.com/blevinn/aginctus/issues/801) | Public reporting vocabulary; event delivery, status projection, CLI/log adapters and secret handling |
| [#802 Build selection](https://github.com/blevinn/aginctus/issues/802) | Descriptor contract; deterministic generated composition and external/minimal/default builds |
| [#803 Herdr vertical slice](https://github.com/blevinn/aginctus/issues/803) | Contracts, conditions, reporting and registration; usable container-backed plugin with image override |
| [#804 OpenCode](https://github.com/blevinn/aginctus/issues/804) | Same foundations; agent lifecycle/provisioning and guest contract parity |
| [#805 Gateway](https://github.com/blevinn/aginctus/issues/805) | Core networking split plus foundations; multi-container topology and independent egress/credential gates |
| [#806 Providers/MCP](https://github.com/blevinn/aginctus/issues/806) | Foundations; migrate concrete adapters and preserve registry/credential/grant semantics |
| [#807 Nix](https://github.com/blevinn/aginctus/issues/807) | Registration and first image-backed plugin; default/custom/existing image wiring, then extend per migration |
| [#808 Integration and retirement](https://github.com/blevinn/aginctus/issues/808) | All slices; parity/evidence audit, author documentation and justified remaining packages |

Herdr is the first end-to-end exercise, not a mandatory dependency of OpenCode
or all plugins. Use a minimal external fixture during foundation work so public
API mistakes surface before product extraction. Nix support should accompany
usable image-backed migrations, not arrive only after all plugins have moved.

## Validation and completion criteria

For this initial documentation PR, validate Markdown links/discoverability and
canonical CLI examples. Review this proposal against current source and related
authority/state contracts. Passing those checks does not implement the design.

Implementation PRs add meaningful tests appropriate to their behavior:

1. Public-only external plugin fixture, API/import-boundary checks, strict config
   rejection, deterministic IDs and generated selection tests.
2. Deterministic fake-based tests of preflight-before-mutation, deferred
   dependencies, required unknown/skipped outcomes, postflight failure,
   cancellation, readiness deadlines and partial/uncertain writes.
3. Event ordering/projection, snapshot subscription gaps, slow/disconnected sinks,
   terminal results, stale observations and sensitive-output tests.
4. Nix evaluation/build checks for default/custom/existing images, supported
   systems, reduced plugin selections and compatibility outputs.
5. Disposable Incus integration for compatible/incompatible images, preserved
   volumes/SSH trust, resource ownership/project scope, negative authorization,
   guest readiness and recovery after interrupted work.
6. Separately authorized live inference/tool acceptance where relevant, with
   explicit evidence scope and no implication that hermetic/VM success replaces it.

Run relevant package checks and repository-required gates for each increment;
publish exact commands, prerequisites and current results in its PR. Keep the
[testing index](../testing/README.md) authoritative for evidence selection.

The epic closes only when migrated integrations use registered capabilities;
generic core has no product definitions outside composition; existing CLI,
configuration, image outputs, authorization and secret protocols retain parity
or have reviewed migrations; and the combined public plugin/image/check/status
path is verified. Update current architecture and canonical operator references
as functionality actually ships, not when a proposal merges.

## Decisions to settle during implementation

- Exact public Go signatures and minimal resource vocabulary, proven by external
  fixtures rather than mirroring all internal implementation types.
- Build manifest syntax, external plugin pinning workflow and generated-file
  policy, including Nix module/vendor reproducibility.
- Image-contract metadata format and the division between offline metadata checks
  and guest-only compatibility checks.
- Versioned event/status schema details, retention defaults and journal adapter,
  meeting the ordering/gap/failure semantics above.
- Compatibility adapters and final destinations for protocol, registry and
  specialist test tooling after dependency audits.

These open details do not reopen the agreed public boundary, configurable image
pointers, first-class conditions, structured observability or authorization
separation. They must be resolved in small reviewed slices with executable proof.
