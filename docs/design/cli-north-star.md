# CLI north star: environment-centric command surface

**Status:** Accepted top-level direction (2026-11-15); subcommand trees, wire syntax,
compatibility and implementation are **not** settled by this document.

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
| `state` | Examine and reason over configured, accepted and observed state | Exact comparison/promotion verbs remain open |
| `system` | Host-level administration and diagnostics | Current single-host scope; not an environment |
| `config` | Inspect fully resolved configuration, provenance and selection rationale | Read-only, across CLI, environment and system scopes |
| `plugins` | Read-only plugin inventory and usage | Priority 1: inventory/capabilities; priority 2: bindings/usage |
| `operations` | Provisional operational concept | Name and precise purpose may change; **not** a required plumbing namespace |

`operations` is a consciously provisional name (sometimes shortened to
`ops` in discussion). Do not use it as a miscellaneous catch-all or infer
an approved subcommand tree from its presence in this list.

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

## Cross-cutting introspection

**`config`** answers *what would Aginctus use, where did the effective
configuration come from, and why was it selected?* It covers CLI execution,
active-environment selection, environment configuration, plugin selection and
system configuration. It shows effective decisions and provenance, with
sensitive values redacted; it does not change the underlying configuration.

**`state`** answers *what was configured/proposed, what was accepted, what is
observed, and how do they compare?* It is not a synonym for `config` or
runtime `status`. Acceptance/promotion may require mutation and separately
scoped authority; exact verbs and UX are deferred. Deliberate observed drift
does not itself alter accepted state.

**`plugins`** is read-only. Priority 1 is inventory of compiled plugins,
versions, dependencies and capabilities, independent of an environment.
Priority 2 is inspection of which implementations are selected and used by
environment resources, and how they bind to workloads. Plugin inclusion is
neither runtime activation nor authorization.

## Images: agreed split

`images` is top-level because immutable image artifacts can be reused by
multiple environments and may be shared at an Incus project/image-store
scope. Images are not container workloads.

The **normal** interface starts with `images list` and `images show`:
`list` defaults to images referenced by the selected/active environment;
`show` explains definition/source, immutable artifact identity, provenance
and aliases as available. An explicitly selected Incus project can expose
its image-store inventory, which is **not** interchangeable with one
environment's image dependencies. Do not add `images status` without a
distinct, justified health model.

**Explicit image builds require plumbing mode.** The same is true of manual
imports, removals and alias mutations. Normal reconciliation may build/reuse
images as needed. Exact build arguments and the image-store targeting syntax
are deferred. Distinguish image definitions, immutable image artifacts (for
example, fingerprint) and mutable aliases.

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

- Concrete `environment`, `workloads`, `state`, `system`,
  `operations` or plugin leaf-command trees, lifecycle verbs, JSON schemas,
  or exit-status conventions.
- Whether `operations` should be renamed or eventually disappear.
- Precise environment selection precedence, read-only project inventory
  targeting, frontend choice where several exist, or image identifiers.
- Plumbing command placement beyond the opt-in model; confirmation/preview
  syntax, recovery procedures, or the specific eligible primitive catalog.
- Migration deadlines for current legacy CLI spellings or detailed
  `aginctus-dev` command/binary consolidation.

Implementation work should begin with a separate CLI migration map comparing
existing commands to this vocabulary. Preserve old behavior and safety gates
until that map identifies explicit compatibility transitions. Refactoring
Cobra folders without resolving user-facing semantics is not sufficient.
