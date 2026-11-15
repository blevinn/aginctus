# CLI north star: environment-centric command surface

**Status:** Accepted top-level vocabulary and selected command semantics (2026-11-15).
Additional leaf details, compatibility and implementation remain open. This is a target
interface, **not** documentation of currently supported CLI syntax.

**Related architecture:** [Project-scoped environment authority](../adr/0006-environment-incus-authority.md),
[three-layer state](three-layer-state.md), [plugin-oriented core](plugin-oriented-core.md),
and [domain-owned CLI migration](cli-domain-ownership.md).
The [CLI reference](../reference/cli.md) describes the implemented interface;
the commands illustrated here are **target vocabulary**, not an assertion that
they already exist.

## Organizing principle

The environment is the primary unit of configuration, accepted state,
reconciliation and resource lifecycle. First-class resource commands are
ergonomic views into an environment, **not** independent provisioning roots.
Keep the public vocabulary focused on user concepts rather than current
Incus/Nix implementation boundaries. An environment's source (NixOS,
Home Manager, local file) does not determine its authorization.

Aginctus has one production-oriented command, `aginctus`. Developer and
test-environment tooling belongs under a separate `aginctus-dev` executable,
not under normal production command discovery. The existing `testenv` CLI
and `aginctus-testenv` worker binary are migration inputs, not settled
`aginctus-dev` command paths.

## Agreed top-level names

| Command | Role | Notes |
| --- | --- | --- |
| `environment` | Environment discovery, management and reconciliation | Primary lifecycle boundary |
| `workloads` | Environment-owned, potentially multi-container services | General resource class interface |
| `agents` | Agent-class workloads | Alias of `workloads --class agent` |
| `frontends` | Frontend-class workloads | Alias of `workloads --class frontend` |
| `gateways` | Gateway-class workloads | Alias of `workloads --class gateway` |
| `images` | Image artifact/dependency inspection; optional imperative primitives | Not a workload class |
| `ui` | Interact with an environment's frontend through a selected UI method | Client interaction, not frontend provisioning |
| `console` | Open the console-based UI | Alias for `ui --console` |
| `state` | Read-only inspection of configured, accepted and observed state | `show`, `diff`, `history`, `explain` |
| `system` | Host-level administration and diagnostics | Current single-host scope; not an environment |
| `config` | Inspect fully resolved configuration, provenance and selection rationale | Read-only, across CLI, environment and system scopes |
| `plugins` | Read-only plugin inventory and usage | Priority 1: inventory/capabilities; priority 2: bindings/usage |
No generic `operations` / `ops` namespace belongs in the MVP. Reconciliation
health belongs to environment/resource status, revision context and provenance
to `state`, and direct imperative resource mutations to gated plumbing commands.
Operation journals, logs and asynchronous activity might merit a distinct
resource interface later, but no such public noun is pre-approved.

### Workload model and convenience aliases

A **workload** is a managed service that may consist of one or multiple
containers. The initial workload classes are `agent`, `frontend` and
`gateway`. Class identity is separate from the selected implementation:
for example, a frontend may be supplied by the Herdr plugin; `herdr` is
**not** a top-level Aginctus command.

Class contracts should remain extensible in code, without building dynamic
plugin-defined classes or dynamic CLI namespaces now. `agents`, `frontends`
and `gateways` route through the same command implementations as
`workloads` with a fixed class filter. Conflicting explicit class filters
must fail rather than override the alias. The accepted interface is
conceptual; the precise placement of `--class` among leaf commands remains
for the next design phase.

The frontend workload **hosts** user interaction services. `ui` is the
**client-side interaction** namespace and can eventually expose console,
web or other methods. `console` is only a convenient spelling for
`ui --console`; it is not a separately reconciled workload.

Workload names are **unique across the entire environment**, regardless of
class: `workloads show NAME` must not need class disambiguation. Class aliases
must fail for a name belonging to another class. Workloads currently form a
**flat** environment namespace, may depend on other workloads, and may each
contain multiple containers. Nested workloads are deferred; constituent
containers are not separate porcelain workloads. A class change must not
silently reinterpret an existing workload as a different class.

## Environment selection and authority

Environment-aware resource commands (`workloads`, class aliases, `ui`,
`console`, and environment-referenced image inspection) may omit
`--environment` and select the **active environment** from configuration
or an environment variable. An explicit root-level `--environment NAME`
overrides that selection. The detailed precedence **between** configuration
and environment variables, naming/identity resolution and handling of
ambiguous selection remain to be specified. Selection is never authorization.

Preserve [ADR 0006](../adr/0006-environment-incus-authority.md):

- Multiple independently managed Aginctus environments may inhabit one
  Incus project; each environment binds to exactly one verified server/project.
- Incus projects establish the logical three-layer state authority scope.
  Environment accepted revision chains are independently versioned within
  that project. An environment is not identical to a project.
- Incus credentials and project permissions govern API access. Environment
  labels, names, the active-environment selector and configuration origin do
  not create privileges.
- System-owned host services, daemon policy and privileged networking remain
  distinct from environment reconciliation and require independent authority.
- There is no privileged merge of root/system and user/Home Manager environment
  candidates. A separate future system-level three-layer state mechanism is
  possible but not part of this CLI naming decision.

Do not introduce a second ambient project selector for ordinary environment
actions: the target environment determines its authoritative Incus binding.
Explicit project/image-store inspection and administrative host commands are
distinct scopes. Their exact selector syntax and conflict rules remain open.

## Declarative environment and resource lifecycle

Environment-wide and targeted resource operations share one porcelain vocabulary:

| Verb | Environment | Resource |
| --- | --- | --- |
| `list` | Discover environments | List logical workloads |
| `show` | Show environment identity/configuration | Inspect a workload and its components |
| `status` | Health and convergence | Aggregate workload/component health |
| `accept [--plan]` | Promote eligible configured additions/changes to accepted state | Promote only a selected workload's eligible changes |
| `reconcile [--plan]` | Request convergence toward accepted state | Reconcile only the selected workload |
| `remove [--plan]` | Authorize removal of an environment absent from configured state | Authorize removal of a workload absent from configured state |

No standalone `environment plan` and no porcelain `apply`. Acceptance,
reconciliation and removal have different state effects:

- `accept` updates **accepted state** from configured state. It must not implicitly
  authorize deletion of configured-absent resources; such resources stay accepted
  until separately removed. It does not directly reconcile observed resources.
  Accepted modifications can still require restarts or replacements during later
  convergence; plans must disclose material consequences.
- `remove` deletes the selected declaration from **accepted state** only if it
  is **already absent from configured state**. This authorizes subsequent
  physical removal through reconciliation, potentially by an automatic controller.
  `remove` does not itself imperatively destroy the observed resource.
  Exceptional forced actions belong in plumbing.
- `reconcile` changes **observed state** toward the currently accepted revision,
  never rewrites accepted state. A controller is normally expected to reconcile
  automatically; the CLI requests a targeted or aggregate pass using the same
  engine, without competing with the owning controller.
- `--plan` on each transition is strictly read-only, showing that transition's
  proposed effects without granting authorization for execution. Actual mutation
  revalidates state, target and authority at execution time.
- Targeted acceptance/removal modifies the relevant item in the environment's
  authoritative revision chain, not a separate per-class state database. Targeted
  reconciliation may **inspect dependencies** but must not mutate a different
  workload to satisfy them. Unsatisfied dependencies cause a clear failure;
  environment-wide reconciliation handles dependency ordering.

Examples of target syntax, subject to final argument and flag design:

```sh
aginctus environment accept dev --plan
aginctus environment accept dev
aginctus agents reconcile researcher --plan
aginctus agents reconcile researcher
aginctus agents remove researcher --plan
```

Resource-level `exec` and `shell` are **ordinary porcelain operations**,
despite being imperative: they provide tactical access to a managed workload,
not direct Incus configuration manipulation.

```sh
aginctus workloads exec researcher -- ps aux
aginctus gateways shell inference --component postgres
```

Both operate on a workload's constituent component using the caller's
authorized identity. The only component can be selected implicitly; for a
multi-container workload, an explicitly declared unambiguous interactive
default may be selected, otherwise require `--component NAME`. A missing
or ambiguous component is an error, not a guess. `shell` may share execution
machinery with `exec`. Neither changes accepted state or triggers reconciliation.

## Frontend interaction and selection

`frontends` is the frontend-class workload alias, whereas `ui` invokes a
frontend interaction method and `console` means `ui --console`.
Resolve the frontend in this order:

1. Explicit `--frontend NAME`.
2. Configured default frontend in the selected environment.
3. The sole frontend if exactly one exists.
4. Otherwise fail with available choices, or report no configured frontend.

An invalid configured default is an error, not permission to fall back.
The selected frontend plugin defines its **native/default UI method**:
bare `aginctus ui` invokes that method. `ui --console` requests a
console method only if supported, and future `ui --web` is possible.
The CLI handles resolution, capability validation and authorization, not
a hardcoded Herdr client implementation.

## Read-only inspection vocabulary

`aginctus state` exposes `show`, `diff`, `history`, `explain`.
It is read-only and defaults to the active environment, with explicitly
selected project-wide inspection where appropriate. `show` selects a state
layer; `diff` compares layers or accepted revisions; `history` inspects
accepted revision history; `explain` traces differences and provenance
without inventing unavailable causality. Acceptance, reconciliation and removal
remain on environment/resource nouns.

`aginctus system` initially exposes only `status` and `doctor` for
host readiness and diagnostics. Do not add `system config` as a duplicate
of cross-cutting `config`, or an abstract `system resources` catch-all.
Future host-scoped reconciliation or state lifecycle is not an MVP assumption.

`aginctus config` initially exposes `show`, `get`, `explain`.
It is read-only: fully resolved values and source precedence must match
actual CLI execution, with secrets redacted. Possible future `set`/`unset`
convenience would edit local configured inputs only, **not** automatically
accept or reconcile their changes.

`aginctus plugins` initially exposes `list` and `show` for Pri1
compiled inventory/capabilities, and `usage` for Pri2 environment-scoped
implementation bindings and consumers. All are read-only. Inventory does
not require environment selection; usage defaults to the active environment.

## Cross-cutting introspection

**`config`** answers *what would Aginctus use, where did the effective
configuration come from, and why was it selected?* It covers CLI execution,
active-environment selection, environment configuration, plugin selection and
system configuration. It shows effective decisions and provenance, with
sensitive values redacted; it does not change the underlying configuration.

**`state`** answers *what was configured/proposed, what was accepted, what is
observed, and how do they compare?* It is not a synonym for `config` or
runtime `status`. Its agreed read-only verbs are specified above;
acceptance/removal remain on resource and environment commands. Deliberate
observed drift does not itself alter accepted state.

**`plugins`** is read-only. Priority 1 is inventory of compiled plugins,
versions, dependencies and capabilities, independent of an environment.
Priority 2 is inspection of which implementations are selected and used by
environment resources, and how they bind to workloads. Plugin inclusion is
neither runtime activation nor authorization.

## Images: inspection, environment pins and explicit GC

`images` is top-level because immutable image artifacts can be reused by
multiple environments and may be shared at an Incus project/image-store
scope. Images are not container workloads.

The normal image interface initially has `list`, `show`, `pin`, `unpin`,
`gc` and `gc --plan`:

- `list` defaults to images referenced by the selected/active environment;
  `show` explains image definition/source, immutable artifact identity,
  provenance and aliases. Explicit project-level image-store inventory is
  distinct from environment references; exact selector syntax remains open.
- `pin` and `unpin` express **environment-owned retention policy** and
  reference immutable artifact identities, not aliases. More than one
  environment may independently pin the same image; deleting an environment
  removes **its** pins without necessarily deleting shared artifacts.
  Where the pin enters configured versus accepted state is still open.
- `gc --plan` is read-only, showing eligible artifacts and their retention
  reasoning. `gc` is an **explicitly invoked** porcelain cleanup pass.
  No automatic GC service, trigger or schedule is presumed in the MVP.
  GC examines the actual shared store scope, honoring pins in all relevant
  environments, configured/accepted references, running/stopped consumers,
  ownership and retention rules. Missing or untrustworthy evidence prevents
  deletion; eligibility is rechecked before each mutation.
- Do not add `images status` without a separate health model, or generalize
  `gc` to other resource families without a concrete need.

`build`, `import`, direct `remove`, and mutable alias management are
**plumbing-only** imperative image operations. Explicit builds therefore
require `--plumbing`, even though ordinary environment reconciliation can
build or reuse the same artifacts. `images remove` directly deletes an
artifact and must not be confused with porcelain resource `remove`, which
changes accepted state.

Illustrative target syntax:

```sh
aginctus images list
aginctus images pin IMAGE
aginctus images unpin IMAGE
aginctus images gc --plan
aginctus images gc
aginctus --plumbing images build IMAGE
```

## Porcelain and plumbing

The Git porcelain/plumbing analogy describes **implementation layering**,
not a required `plumbing` command subtree. High-level commands compose
shared domain primitives; the independently invocable low-level commands
use those same implementations. A lower-level imperative action may
intentionally create observed drift from accepted state, without updating
accepted state or claiming environment conformance.

Low-level command invocation is **disabled by default**. An explicit root
`--plumbing` flag or an effective configuration setting enables those
commands and exposes them in help/completion; without opt-in, they are
not callable. Command registration/help should explain an attempted disabled
invocation clearly. The precise config key and command placement are deferred.

The opt-in is a feature/UX gate **not a privilege grant**. All operations,
including plumbing, must independently enforce target identity, applicable
Incus/host authorization, ownership protections, concurrency/fencing and
destructive-action safeguards. Deliberate nonconformance does not permit a
privilege escalation or silent adoption of foreign resources. The normal
porcelain remains the intended path to accepted-state convergence.

Illustrative interface only (not implemented syntax guarantees):

```sh
aginctus workloads list
aginctus --environment dev agents list
aginctus ui --console
aginctus console
aginctus images list
aginctus --plumbing images build IMAGE
```

## Intentional non-decisions and next design phase

This document does **not** prescribe:

- Exact leaf argument/flag placement, output schemas, error and exit codes,
  migration dates or compatibility adapters.
- Exact precedence between environment variables and configuration for the
  active environment, project-specific selectors or identity resolution.
- The concrete request/enqueue mechanics of controller-mediated reconciliation,
  dependency readiness thresholds, or ownership/adoption migration procedures.
- Whether image pins are initially staged in configured state or changed
  directly through an authorized accepted-state operation, and how GC records
  tombstones/retention evidence.
- Detailed plumbing primitives, confirmations or recovery procedures, or
  `aginctus-dev` command/binary consolidation.

The absence of a generic `operations` command in the MVP is intentional;
a future operation-history interface needs concrete requirements before it
gains a top-level namespace.

Implementation work should begin with a separate CLI migration map comparing
existing commands to this vocabulary. Preserve old behavior and safety gates
until that map identifies explicit compatibility transitions. Refactoring
Cobra folders without resolving user-facing semantics is not sufficient.
