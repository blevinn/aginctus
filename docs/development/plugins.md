# Public plugin and contribution contracts

**Status: experimental declarations, runtime checks, condition execution and lifecycle traces.**
This covers A1–A5 of the [stacked plan](https://github.com/blevinn/aginctus/issues/797#issuecomment-6871452026)
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
Required failed/unknown/skipped results must block success in the executor.
Text validation rejects oversized/control-character payloads, but does not scrub
secrets: producers must emit allowlisted nonsecret diagnostics.

## External-author example and validation

The independently named module under `pkg/plugin/testdata/external/` contains a
provider/MCP/gateway example plus an external consumer. It contributes a
container-free catalog target and a dependent two-container helper target, with
independently configurable image pointers and explicit criteria. The offline
consumer evaluates candidate schema validity and exercises a runtime lifecycle
criterion with an explicitly supplied fake container inspector. It does not perform
the declared guest or live-inference probes. Its
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
Existing commands still use their current product paths. Runtime bindings
independently enforce declared services and engine policy as described below.

Run the host-adapter tests with the public boundary checks:

```sh
nix develop -c go test ./internal/plugins/... ./pkg/plugin/... ./pkg/orchestration/...
nix develop -c go vet ./internal/plugins/... ./pkg/plugin/... ./pkg/orchestration/...
```

The registration/graph tests use public-contract fake implementations and a declaration-only
consumer. They verify scoped dependencies, independent image configuration,
deterministic ordering, metadata/data ownership and failure/cancellation behavior.
They do not deploy guests, evaluate runtime probes or import concrete products.

## Scoped runtime check services

`plugin.CheckEvaluator` is an optional interface alongside discovery and target
contributions. `EvaluateCheck(ctx, request, services)` receives detached target/check
declarations and explicitly supplied capabilities, not an administrator client,
raw driver payload, configuration/credential store or permission inferred from a
logical name. Discovery-only plugins remain valid. Runtime callbacks must finish
their service calls before returning and produce nonsecret findings and safe errors.

The first service is `CheckServices.Containers`, a `ContainerInspector` whose
`Inspect(localContainerID)` method reads lifecycle state only. The host binds the
evaluation context, so the plugin cannot supply a longer-lived context. The service
expires when the callback returns, including when the plugin retains a reference.
Nil services mean unavailable; there is no ambient fallback. Offline checks receive
no container service. This API does not execute guest commands or expose network,
credential, model/tool, secret-reading or mutation capabilities.

`orchestration.ContainerObservation` distinguishes missing, stopped, running and
unknown, carrying observation time and a nonsecret resource incarnation for present
containers. Missing/unknown observations cannot establish an incarnation. Running
is lifecycle evidence, **not application readiness**. Producers must project safe
metadata; string validation is not secret redaction.

`Registry.BindCheck(scopedTarget, checkID, hostInspector)` validates a selected
pre/postcondition and returns an existing-engine `BoundCheck`. It rejects missing
evaluators, undeclared checks, invalid scopes, unavailable services and access
families not yet supported by this adapter. Binding is I/O-free. Each evaluation
gets new detached declarations and an invocation-local reader limited to containers
declared by that target. Invalid, stale or future-dated observations fail closed;
ignored service errors still make the evaluator fail, preserving safe error causes.

The trusted `hostInspector(ctx, targetKey, localContainerID)` must resolve an exact
backend resource and **independently authorize every read**, including principal,
project and ownership requirements. It must not infer authorization from the target
key or from a prior engine check. Engine effect/opt-in policy still gates the
evaluation; these are separate boundaries. Bind all applicable checks into
dependency-first steps, select feasible preflight checks, and set applicable
`BoundCheck.ResourceIncarnation`/`AcceptedRevision` expectations before execution.
This binding path supplies neither those expectations nor write-time authority.

The external-module fixture demonstrates the public-only lifecycle evaluator; the
host tests assemble declarations and bind scoped reads through the existing engine,
including readiness retries, retained-service refusal and ignored-error handling.
These use fake host readers. Real backend/CLI adoption and additional service
families remain follow-up work under #798/#800.

## Existing engine: cancellation-aware preflight

The first A4 increment addresses [#348](https://github.com/blevinn/aginctus/issues/348)
in the existing ordered `internal/orchestration` engine. `Driver.ValidateContext`
is mandatory for every registered engine driver; there is no fallback to a
context-free validator. `Engine.Execute` and aggregate apply's initial validation
use `Engine.ValidateContext` with the request context. All steps must pass
validation before execution begins. Cancellation stops remaining validation and
execution, including when a callback returns nil after cancellation. Wrapped
errors retain cancellation/deadline identity for `errors.Is`; the engine also
retains a simultaneous driver validation error.

Apply forwards the caller context to its existing read-only `PlanContext`;
Compose forwards it to document/project loading with nonsecret validation
placeholders. Native network and state-volume configuration checks remain local.
The context-free `Validate` helpers remain convenience wrappers for standalone
callers without a request context. Request paths must use `ValidateContext`.
Blocking operations must cooperate with cancellation; the engine cannot forcibly
interrupt a driver that ignores its context. Cancellation does not roll back a
mutation already sent during execution, and a canceled step is not reported as
success. Aggregate apply checks cancellation again before credential initialization.

This increment changes cancellation propagation, not the effects or authority of
existing preflight: Apply may inspect Incus, Compose may use temporary files, and
validation is not a new offline-only guarantee. Public plugin configuration and
contribution construction remain I/O-free. Condition scheduling and probe policy
are described below; product adoption remains work under
[#800](https://github.com/blevinn/aginctus/issues/800).

Run the execution and caller regression tests:

```sh
nix develop -c go test ./internal/orchestration/... ./internal/statevolume/... ./internal/cli/... ./internal/gateway/...
```

## Existing engine: condition gates and bounded readiness

The A4 increments add opt-in `Engine.WithConditions(stepBindings, policy)`
to the same ordered executor. These are trusted, in-memory **host bindings**;
serialized plans and current CLI commands do not acquire evaluator callbacks or
new authority. A host maps each selected target to a step in dependency-first
order, retaining `(deployment, instance, target)` in `TargetScope` and local
target/check IDs in public findings. The fake-host integration test in
`internal/plugins/execution_test.go` demonstrates this path with container-free
contributions and no infrastructure access.

- All supplied binding scopes, definitions, evaluator presence, effect policy and
  evidence-identity expectations validate before driver preflight. Unknown step
  bindings and duplicate target scopes/check IDs are rejected. Unbound steps
  retain legacy behavior; the host is responsible for binding **every** applicable
  required declaration. An omitted binding is not proof of target convergence.
- A precondition marked `Preflight` runs before any operation and is evaluated
  again at its operation boundary. Other preconditions are deferred, allowing
  dependency-produced prerequisites. Postconditions run after the driver returns.
  A required condition must pass before any later operation, including dependents.
  Failed/unknown readiness evidence may be polled within its declared window;
  a terminal non-pass stops the plan. Optional diagnostics remain visible.
- `CheckPolicy` defaults to offline checks. Read-only checks require an explicit
  check mode and host authorizer; active probes additionally require active mode
  and every declared opt-in. The authorizer must approve all access requirements
  for the bound scope. Authorization for the whole selection runs before any
  evaluator/operation, and is repeated immediately before each evaluation.
  Authorizers must be cooperative, bounded policy checks that do not require a
  dependency's resource to exist. Resource ownership/preconditions and independent
  write-time authority remain the operation adapter's responsibility.
- Each evaluator receives a deadline-bounded caller context. Evaluation errors,
  invalid/mismatched evidence, expired observations and future timestamps yield
  safe structured unknown findings. Required failures retain error causes through
  `errors.Is`. A callback returning nil after cancellation/timeout cannot establish
  success, and parent cancellation stops execution even for optional checks.
- With `MaxAge == 0`, evidence must be observed during the current evaluation;
  otherwise its age must satisfy the declared maximum. Supplied incarnation and
  accepted-revision expectations must match. The host must supply these bindings
  wherever applicable; empty expectations do not verify resource identity. The
  engine does not resolve accepted state, maintain an observation cache or infer
  identity from a logical target name.
- `Result.Conditions` retains scoped phase-tagged findings with one-based `Attempt`
  numbers, reset for each step/phase/check window. Every attempt is retained,
  including intermediate non-passes. `Result.Operations`
  retains completed driver calls even when a subsequent postcondition fails;
  `Result.Steps` includes only steps that pass their required postconditions.
  Neither collection is a durable journal or proof that canceled writes did not
  reach a server. Recovery still requires appropriate re-observation.

The check mode limits evaluator effects; it does not make legacy driver preflight
offline or authorize operation writes. Bound callbacks are trusted Go code, not a
sandbox. Metadata and nested slices are copied at binding and authorization
boundaries, but closure state remains the host's responsibility.

### Bounded readiness policy

`RetryInterval == 0` requests one attempt. A positive interval enables polling:
the first attempt runs immediately; later attempts wait the interval **after** the
previous attempt completes. `Timeout` bounds the entire window, including
per-attempt authorization, evaluation and waits, rather than resetting on retries.
Selection-wide authorization has its own bounded check before any operation.
Every actual attempt is reauthorized and validates evidence freshness and bound
identity afresh. A preflight pass does not bypass the fresh mutation-boundary
window. A required dependent remains blocked until its prerequisite passes.

Only valid, correlated, fresh **failed or unknown findings** are retryable.
Passed and skipped findings, evaluator errors, denied authorization and unusable
evidence are terminal. Evaluators should represent an observed not-yet-ready
state as a failed or unknown finding; an evaluator error does not request a retry.
This policy applies to offline, read-only and explicitly opted-in active checks;
opting into an active check with a retry interval permits repeated probes within
the window, potentially including repeated network/model/tool effects and costs.
Use a zero interval for a probe that should run only once. The engine never
retries the operation in response to a check result or timeout.

A timeout while waiting preserves the last real observation and returns the
deadline error for a required check; it does not invent a new observation or
attempt. A timed-out evaluation yields unknown evidence. Optional checks use the
same window but do not block later operations on terminal non-passes. Parent
cancellation stops both required and optional checks, including interval waits.
Timeouts remain cooperative: trusted callbacks must honor their context.

Condition-bound engines reject dry run before driver preflight: a simulated
operation must not manufacture postcondition success. Existing unbound engine
dry-run behavior is preserved. Ongoing observation scheduling, deployment-wide
postflight, real product adapters and CLI adoption remain follow-up work
under [#800](https://github.com/blevinn/aginctus/issues/800).

Validate engine gates and the scoped fake-host integration:

```sh
nix develop -c go test ./internal/orchestration/... ./internal/plugins/... ./pkg/plugin/... ./pkg/orchestration/...
nix develop -c go test -race ./internal/orchestration ./internal/plugins
```

## Ordered execution reporting

### Current-run status snapshots

Reporting also returns `Result.Report.Status`, an authoritative execution snapshot
independent of retained trace truncation. The host can create
`internal/observability.NewMonitor()` and pass it in `ExecuteOptions.Monitor` to
enable reporting and inspect snapshots **during** execution. Share only the public
`observability.Reader` with authorized consumers. No global registry or cross-run
subscription grants access to other principals' metadata. A monitor is single-use;
reusing it leaves its original run unchanged and collects a separate result report.
Drivers do not receive the host's monitor capability.

Snapshot capture, sequence assignment and projection share one lock. `Watermark`
includes rejected/omitted sequences; `Dropped` counts retained-history loss, not
lost execution knowledge. Valid transitions update the projection before bounded
retention. `Run` and `Finished` preserve terminal status. Detached `Entries` contain
the latest event per encountered lifecycle/check slot, ordered by sequence, with
only the latest attempt/activity per check window. New attempts clear previous
attempt activity. Neither retries nor plugin-selected activity codes grow status
history. Snapshot size scales with encountered steps/checks, not event volume.

Absent/unstarted slots are **not success**. This initial projection describes
encountered execution, not the planned graph, accepted-state inventory or ongoing
health. Observation timestamps are historical and are never refreshed on reads;
consumers must not reuse a prior pass as current readiness. Core waiting, blocked,
canceled, optional non-pass and uncertain outcomes retain their original meanings.
Progress counts cannot override lifecycle findings. Status is ephemeral and remains
separate from durable recovery/journaling and real backend adoption under #800.

Validate snapshots and engine integration (fake drivers; no infrastructure):

```sh
nix develop -c go test ./internal/observability/... ./internal/orchestration/... ./pkg/observability/... ./pkg/plugin/...
nix develop -c go test -race ./internal/observability ./internal/orchestration
```

### Bounded live subscriptions

The monitor also implements public `observability.Stream`. Capture `Snapshot()`,
then `Subscribe(snapshot.Cursor())`. Registration and bounded replay share the
projection lock: delivery resumes strictly after that watermark or returns an
explicit `Update.Gap` and ends. Gaps require a fresh snapshot and new subscription;
they never contain rejected payloads. Foreign/future cursors return `ErrCursor`.
An old cursor may require a gap when retained history is incomplete or its replay
exceeds the subscription buffer. There is no durable/exactly-once replay promise.

Each run permits **32 active subscriptions**, each buffering **64 updates**.
Publication is nonblocking under the ordering lock, after status application and
retention accounting. A full queue is replaced by a gap and disconnected. Live
subscribers may see events omitted from the retained prefix; `Report.Dropped`
counts history loss, not per-subscriber loss. Rejected events create a gap for all
active subscribers. Observer-specific loss is signaled only on that subscription.

`Subscription.Next(ctx)` blocks only its consumer, returning the context error and
closing on cancellation. `Close()` is idempotent and safe concurrently with reads;
consumers must close abandoned subscriptions to release their run slot. One reader
per subscription drains queued updates and then receives `io.EOF` after close,
terminal completion or a gap. Queued terminal delivery is best-effort; the final
snapshot remains authoritative even when a terminal event overflows. No sink,
writer or observer callback is invoked by the executor, and no goroutine is created
for publication. Sink errors/disconnection cannot change execution results.

Validate bounded replay, overflow, cancellation and snapshot/publication ordering
with the snapshot commands above; these tests need no external services.

### Optional text and structured-log presentation

`internal/observability/presentation` consumes only public reporting vocabulary.
The host may use `Text(writer)` for quoted, line-oriented execution status or
`JSONLines(writer)` for structured JSON logs. JSON records have `schema_version`,
`run_id`, `type` (`snapshot`/`update`) and a typed `snapshot` or `update` payload;
no prose parsing is needed. Gaps carry run correlation and no rejected payload.
Snapshot/update validation rejects malformed metadata before writing. Text quotes
all producer strings and formats timestamps in UTC; JSON escapes control/line
separators. Producers remain responsible for nonsecret codes, scope and revisions.

For a live view, run `presentation.Follow(consumerCtx, monitor, sink)` in a
**host-owned consumer goroutine**, separate from the execution context/outcome.
It registers before writing the initial snapshot, emits events or explicit gaps,
resnapshots/resubscribes on loss and emits authoritative final status. A consumer
attaching after completion gets the final snapshot without replaying history.
Consumer errors are returned only to the host consumer; they must not be joined
into the execution result. Follow closes every subscription on return/recovery.

An arbitrary blocking `io.Writer` cannot be interrupted by context cancellation.
The host owns writer deadlines/close and consumer goroutine shutdown. A blocked
renderer can lose updates but cannot block the executor; final status remains
inspectable independently. Renderer instances are single-consumer, single-run.
These are opt-in adapters for hosts/CLI integrations: existing commands do not
enable reporting, acquire new flags or change stable output. Real product/backend
adoption, planned graph inventory and reusable ongoing-health observations remain
integration/#800 work. No durable log/journal guarantee is added.

Validate rendering, JSON round trips, escaping, gap recovery and optional-sink
isolation against fake engine operations:

```sh
nix develop -c go test ./internal/observability/... ./internal/orchestration/... ./pkg/observability/... ./pkg/plugin/...
nix develop -c go test -race ./internal/observability/... ./internal/orchestration
nix develop -c just check all
```

### Core lifecycle, event vocabulary and retained history

The first A5 increment publishes experimental `pkg/observability` events and a
bounded `Report`. Enable collection explicitly on the existing engine:

```go
result, err := engine.Execute(ctx, plan, orchestration.ExecuteOptions{Reporting: true})
// result.Report remains inspectable even when err is non-nil.
```

Default execution does not collect a report or change CLI output. Reporting uses
the same execution loop, not replay after completion. Core owns run, validation,
authorization, preflight, step, operation, condition-window and check-attempt
transitions. These events carry closed kind/state/reason vocabularies, generated
run IDs, per-run sequence/time, opaque step/operation IDs, logical target scope
where bound, check phase/ID/required policy, attempt number, observation time and
the host-bound accepted-revision expectation where supplied.

Step/operation IDs are `step-N`, correlated by one-based plan position within that
run. Arbitrary plan/step/driver names, configuration, raw errors, finding diagnostic
codes/text, remediation and guest output are not copied into events. Logical scope
and accepted revision must already be nonsecret host metadata. Shape validation
is not secret redaction or an audience authorization policy.

- `completed` means the selected execution finished, **not current application
  health or deployment convergence**. Unbound/omitted checks remain the host's
  responsibility. Dry-run operation/step/run completion is `simulated`.
- Actual attempts are numbered from one within step/phase/check. Condition-window
  start/finish has attempt zero; `waiting` references the last attempted probe.
  A timeout while waiting retains the last observation time, reports the deadline
  reason and adds no fictional attempt or observation.
- Required non-passes block later operations; optional non-passes remain visible
  without changing run success. A step completes only after its required
  postconditions pass, while a prior completed operation remains visible after
  postflight failure. Cancellation during a wait stops even optional checks.
- A real driver error or cancellation after driver entry is conservatively
  `uncertain`, including when a driver returns nil after cancellation. This does
  not assert that a write happened or rolled back. Cancellation before driver
  entry and after a known completed operation remains `canceled`.
- `internal/observability` serializes core transitions and scoped domain progress
  under one collector lock, establishing a shared per-run sequence.
  It retains at most **1,024 events per run**, reserving space for the terminal
  run event. On overflow it keeps the event prefix plus terminal event and counts
  every omitted transition in `Report.Dropped`; sequence gaps expose the loss.
  Dropping reporting events does not drop evaluations or alter execution results.

Collection never invokes observer callbacks or writers and creates no publisher
goroutines, so a sink cannot block execution or decide its outcome. The final
report is available after the call returns; optional host monitors expose the
independent live execution snapshots described above. Subscription and presentation
layers are separate from execution. Neither snapshots nor event delivery establish
cross-run ordering, durable journals, restart recovery, write authority or
accepted-state promotion.

Validate public envelopes, retention and engine lifecycle behavior:

```sh
nix develop -c go test ./pkg/observability/... ./internal/observability/... ./internal/orchestration/... ./internal/plugins/...
nix develop -c go test -race ./pkg/observability ./internal/observability ./internal/orchestration ./internal/plugins
```

## Scoped plugin domain progress

With engine reporting enabled, `CheckServices.Progress` provides an optional public
`observability.Reporter` for the current runtime-check invocation:

```go
if services.Progress != nil {
    services.Progress.Report(observability.Progress{
        Code: "lifecycle-inspection", Current: 1, Total: 1,
    })
}
```

`Progress` accepts a bounded ASCII activity identifier and unsigned `Current`/`Total`
counters (`Current <= Total`; zero/zero is unquantified). It accepts no free-form
text, raw errors, configuration, timestamps, observation claims or correlation IDs.
Codes must be nonsecret domain identifiers; syntax checks do not scrub a secret
that happens to look like an identifier. Counts are informational, not evidence:
100% progress cannot pass a check, finish an operation or release a dependent.

The engine binds run, step/operation, logical target, phase, check, attempt, required
policy and revision metadata. Each retained progress event has kind `progress` and
state `running`, no observation time or failure reason, and the same collector
sequence as surrounding core transitions. Plugins cannot synthesize lifecycle
outcomes. The external-module example demonstrates the public interface without
Aginctus implementation imports; the transitive public-import guard includes it.

The reporter inherits the evaluator's bounded context and expires when that
callback returns. Retained references cannot report into a later attempt. Calls
after cancellation or the exact deadline are discarded, including timer-delivery
gaps; they create neither events nor sequence gaps. Disabled reporting supplies
nil and explicitly masks inherited bindings in nested engine runs. Progress is
available to offline checks too, without granting an inspection capability.

`Report` returns no error and invokes no sink. Invalid payloads during an active
invocation are discarded and counted in `Report.Dropped`; overflow shares the
existing 1,024-event bound and terminal reservation. Dropped progress cannot change
convergence. Concurrent calls are supported and serialized with core events; join
all reporting work before returning from the callback. Ordering among concurrent
calls follows collector lock acquisition, not a fictional producer wall-clock order.
The collector's short internal lock is not an observer/backpressure interface.

This increment binds progress to existing runtime **checks**. Further operation
hooks can receive explicit progress capabilities when their product adapters land.
Plugin reporters grant neither observation access nor durable delivery or backend
authorization; host observation handles are separate capabilities.

Validate runtime progress and the external consumer:

```sh
nix develop -c go test ./internal/plugins/... ./internal/orchestration/... ./internal/observability/... ./pkg/plugin/... ./pkg/observability/...
nix develop -c go test -race ./internal/plugins ./internal/orchestration ./internal/observability ./pkg/plugin ./pkg/observability
```

## Next slices

Further service families, product adoption, planned graph inventory and ongoing
health remain integration work. Static selection/generation, Nix integration and
product extraction follow in their own
reviewed PRs. The existing provider plugin machinery is not switched to this API
by these foundation slices. Additional service families should arrive with concrete
integration needs and preserve the explicit read/write authority boundaries.
