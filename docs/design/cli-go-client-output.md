# Go client and structured CLI output contract

**Status:** Accepted architecture direction (2026-11-15); contract-level design,
not an implemented command reference. This document describes the MVP target
and the incremental migration contract.

**Related:** [CLI north star](cli-north-star.md),
[domain-owned CLI migration](cli-domain-ownership.md),
[three-layer state](three-layer-state.md),
[test reporting](test-reporting.md), and
[plugin-oriented core](plugin-oriented-core.md).

## Decision

Expose a **local, in-process Go client** under `pkg/client` and publicly
documented **projection and result types** under `pkg/api`. The CLI is one
adapter over the same domain-owned use cases; it must not own business logic
or export its Cobra parsing/formatting code as a library. No network daemon,
RPC protocol, transport abstraction, or remote authorization scheme is
introduced for the MVP. Keep requests/results transport-neutral so that a
future remote client can reuse contracts deliberately.

`pkg/api` types are stable **public projections**, not aliases for internal
Incus objects, reconciliation structures, or private domain implementations.
Explicit mappings at domain boundaries prevent internal refactoring from
silently breaking public serialization. Keep mappings owned near their
domains, not in a universal `internal/cli/operations` layer.

Dependency direction:

```text
cmd/aginctus -> internal/cli (Cobra, dispatch, renderer, exit status)
                        -> pkg/client (typed in-process operations)
                                   -> internal/<domain> (authoritative use cases)
pkg/api <---------------- pkg/client and internal/<domain> projections
```

`pkg/client` must not grow a parallel implementation of the domain logic.
`pkg/api` must not depend on `internal/*`, Cobra, Incus SDK objects,
process writers or CLI flags. The client must not depend on Cobra or invoke
the CLI as a subprocess. Existing `pkg/observability` run events remain
execution telemetry, **not** resource status or durable operation history.

## Public Go client

One concrete client, constructed with explicit options, exposes resource
services: `Environments`, `Workloads`, `Images`, `State`, `System`,
`Config`, `Plugins` and `UI`, subject to implementation availability.
Class aliases (`agents`, `frontends`, `gateways`) are filtered views of
`Workloads`, not duplicate service implementations. `console` is a
frontend method, not a separate resource service.

The following is illustrative Go usage rather than a commitment to exact
constructor or method spelling:

```go
c, err := client.New(client.Options{Environment: "dev"})
if err != nil { return err }

list, err := c.Workloads.List(ctx, api.ListWorkloadsRequest{
    Class: api.WorkloadClassAgent,
})
```

Contracts:
- Every I/O-capable method accepts `context.Context`; callers may invoke
  independent client instances concurrently. No package-global mutable
  active environment or implicit escalation.
- The invocation's resolved default environment applies when a request
  omits a target; a request's explicit environment overrides that default.
  Resolution shares the CLI's configuration code and verified Incus
  binding. Selecting an environment does **not** confer authorization.
- Use named typed request/result structs. Avoid unbounded
  `map[string]any` requests, arbitrary selector expressions, and
  generic "execute command" entry points.
- Resource services should initially be concrete types. Export interfaces
  only for demonstrated consumer-facing needs; consumers can define narrow
  interfaces to mock their own dependencies.
- Read-only planning is an explicit operation (e.g. `PlanAccept`),
  distinct from the mutation (`Accept`), not a boolean that toggles an
  otherwise mutating call. Execution revalidates authority and state.
- Workload `exec` / `shell` and frontend UI sessions use distinct
  context-bound interactive/stream contracts. They are not fabricated
  JSON-result operations. Input/output and terminal signals are caller
  capabilities, without bypassing resource ownership or permissions.
- The client is a library, not a daemon; do not promise that calls can be
  made remotely until a transport and remote authority have actually
  been designed.

## Serialized schema conventions

Every **top-level object result** has `apiVersion` and `kind`. Use
`aginctus.dev/v1alpha1` for initial experimental schema fixtures, subject
to deliberate revision before a stable release. These describe schema
compatibility, **not** the Aginctus binary version, Incus API version,
plugin version, or an accepted-state revision.

Use exported named Go projections and conventional Go JSON tags:
lower-camel field names such as `environmentId` and `acceptedRevision`.
JSON is the canonical machine encoding; YAML is generated from the same
JSON representation, so field spelling, omission and scalar behavior do
not diverge. Do not expose internal Incus/Go SDK DTOs directly.

For example, a collection document:

```json
{
  "apiVersion": "aginctus.dev/v1alpha1",
  "kind": "WorkloadList",
  "items": [
    {
      "metadata": {
        "name": "researcher",
        "environment": "dev"
      },
      "class": "agent",
      "implementation": "opencode",
      "components": [{"name": "primary"}]
    }
  ]
}
```

The shape is **illustrative**, not a promise that these resource field
names and query results already exist. Collection items use the normal
resource projection without redundant per-item type envelope; each top-level
collection declares its own type. Represent empty collections as `[]`,
not `null`. Do not add universal `success` / `data` / `result` wrappers.
Not every response represents an individually addressable resource; plans,
diffs and diagnostic results do not require fabricated resource metadata.

Distinguish:
- **Resource metadata:** verified identity, name, environment/project binding
  where applicable, and ownership; do not copy all Kubernetes metadata.
- **Configured/accepted state:** separate authoritative state layers with
  independently understood provenance and accepted revision.
- **Observed status:** explicitly identified observation, age and
  convergence evidence, not an implicit merged "current" resource.
- **Plans:** typed `AcceptancePlan`, `ReconciliationPlan` and `RemovalPlan`,
  including relevant dependencies, guards and possible replacements or
  destruction. Plans are read-only previews, not authorization tokens.
- **Mutation results:** typed outcomes that distinguish committed accepted
  revision, controller request acknowledged, reconciliation attempt finished,
  and verified convergence. Never present "request queued" as healthy.
- **Errors:** bounded stable code vocabulary and safe messages; callers
  use `errors.As` / `errors.Is` or documented code matching rather than
  string comparison.

Default collection ordering is deterministic and documented per resource.
Omission, zero values and nullability must be intentional. Do not introduce
generic pagination, labeling, filters, or a plugin-defined arbitrary schema
system without actual use cases. Plugin-specific data needs an explicit
typed extension boundary and its own compatibility/redaction policy.

Go source types are authoritative but not a substitute for schema
documentation. Build golden JSON/YAML fixtures and, once the first concrete
object family is stable, generate/check JSON Schema from those same public
contracts. Review schema-affecting changes separately from internal data
model refactors. Keep serialized schema evolution, Go source compatibility,
and CLI release compatibility distinct.

## CLI output

Introduce root `--output` / `-o` with command-appropriate support:

| Format | Scope | Semantics |
| --- | --- | --- |
| `table` | Collection/human-readable commands | Concise default human view |
| `wide` | Collection/human-readable commands | More columns; same result |
| `json` | Typed result commands | Versioned structured JSON |
| `yaml` | Typed result commands | Same logical JSON object as YAML |
| `name` | Resource collections | Stable identifiers, one per line |

Other human views (e.g. `state diff`, a plan, a single-resource detail)
can have their own default human layout. `-o json` and `-o yaml` must
serialize the **same underlying typed result**; an output option cannot
change query/authorization/transition behavior. `-o name` is a projection
for shell scripting, not a versioned API document.

Do not implement templates, JSONPath, custom columns, CSV, or new
resource-specific ad hoc renderers as part of the MVP. A common renderer
should own machine serialization and format validation; domain-specific
human table column selection can be supplied without domain operations
knowing about terminal formatting.

For machine-oriented output:
- Stdout is a **single complete successful result document**; no
  incidental logs, warnings, progress, prompts or banners.
- Diagnostics and failures go to stderr, with nonzero process status.
  Structured error serialization can respect the selected machine
  format, but must not claim success; avoid partial success documents.
- Render only trusted, deliberately redacted public projections.
  Generic serialization is **not** a security boundary. Do not expose
  secrets or foreign Incus resources through extra fields.
- Reject explicit `-o` forms that an interactive session cannot support,
  rather than silently ignoring them. Session stdout/stderr belong to
  the session protocol, not the object serializer.
- Rendering must never mutate state, resolve authority differently,
  or turn a failed operation into an exit-zero response.

## Incremental implementation and gates

1. **Design PR:** publish this contract; update design index without
   presenting it as current CLI reference behavior.
2. **Schema/renderer foundation:** add minimal `pkg/api` type metadata
   and typed error conventions plus narrowly scoped shared JSON/YAML
   serialization, with isolated tests and no existing command changes.
3. **First vertical slice:** migrate a read-only, domain-owned workload
   list/show/status path to `pkg/client`, with matching `-o` support;
   keep legacy behavior until its compatibility decision is explicit.
4. **Lifecycle:** add public plan and mutation result projections for
   accept/reconcile/remove, preserve three-layer authority and rechecks.
5. **Extend:** add image/state/system/config/plugin projections,
   interactive session contracts, then retire duplicative CLI adapters.

Do not make a public client method a wrapper over a Cobra handler, legacy
stdout/stderr command runner or permissive raw Incus operation. Where
domain ownership is not yet settled, migrate the domain operation first.
Keep the implementation in isolated branches/worktrees so it does not
disrupt parallel refactors; PRs only in `blevinn/aginctus`. Each PR must
stand independently or explicitly depend on its predecessor.

### Deferred decisions

Exact per-resource schemas, client constructor shape, environment
selection precedence, stable exit-code mapping, async reconciliation
request/attempt identifiers, generated-schema tooling, plugin extensions,
remote transport and client-server version negotiation are **not**
approved by this document. Do not silently settle them with incidental
implementation details.
