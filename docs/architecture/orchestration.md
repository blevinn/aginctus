# Orchestration engine and reconciliation boundaries

**Status (current implementation, not a release acceptance claim):** The Jsonnet plan/validation/execution engine and Go Apply/Compose integration remain present for management-network, Herdr, workloads and gateway. A selectable native Incus SDK driver reconciles the management bridge; additional native project/instance primitives exist, but their presence does **not** mean the legacy drivers have been retired or the whole deployment has native parity. Runtime-specific readiness, migration, gateway credentials and guest bootstrap have separate verification gates. See [ADR 0002](../adr/0002-native-incus-reconciliation.md) and the [active cutover design](../design/native-incus-reconciliation.md).

## Current contract and origins

The orchestration engine evaluates trusted Jsonnet into a deterministic execution plan, validates it and invokes selected reconciliation drivers. It began as a replacement for bespoke imperative `ensure` flows; this document retains that historical motivation while describing the current migration boundary.

The original execution drivers, **still present while the native cutover is underway**, are:

- `apply` for individual Incus resources through `incus-apply`;
- `compose` for multi-service projects through `incus-compose`.

Aginctus must integrate both projects as Go modules. It must not shell out to either command-line program.

This design intentionally separates the Aginctus orchestration contract from the configuration syntax of either underlying project. Incus Apply and Compose remain implementation drivers rather than the public Aginctus model.

## Historical motivation for the engine

The early network and Herdr client commands demonstrated the lifecycle semantics, ownership model, and configuration cascade, but each new resource currently adds bespoke reconciliation code.

That approach becomes expensive for agent workloads and the AI gateway, which need combinations of networks, instances, storage, and Compose projects.

The orchestration engine should instead provide:

1. a generation phase that turns Aginctus intent into a concrete plan;
2. a validation phase that rejects an invalid or unsafe plan before mutation;
3. a deterministic execution phase that invokes reconciliation drivers in order;
4. consistent ownership, dry-run, logging, and error reporting around every step.

## Layering

```text
Aginctus configuration / command
            |
            v
 trusted Jsonnet entrypoint
  + embedded Aginctus libraries
            |
            v
 strict JSON orchestration plan
            |
            v
 plan validation
            |
            v
 ordered executor
      |             |             |
      v             v             v
  apply driver   compose driver  native driver
      |             |             |
      v             v             v
 incus-apply Go  incus-compose Go  Aginctus Incus SDK reconciler
      \             /                      |
       \           /                       |
        \         /                        |
         Incus API <-----------------------/
```

Jsonnet does not call Incus. Drivers do not evaluate Jsonnet. The executor does not interpret arbitrary driver-specific behavior beyond the typed step contracts.

## Orchestration plan

The generated JSON document uses an Aginctus-owned schema.

Initial shape:

```json
{
  "apiVersion": "aginctus.dev/v1alpha1",
  "kind": "Orchestration",
  "metadata": {
    "name": "local"
  },
  "steps": [
    {
      "id": "management-network",
      "driver": "apply",
      "configuration": {
        "documents": [
          {
            "kind": "network",
            "name": "aginctus-mgmt",
            "networkType": "bridge",
            "config": {
              "ipv4.address": "auto",
              "ipv4.nat": "false",
              "ipv4.routing": "false",
              "ipv6.address": "none",
              "user.aginctus.managed": "true",
              "user.aginctus.resource": "management-network"
            }
          }
        ]
      }
    },
    {
      "id": "gateway",
      "driver": "compose",
      "configuration": {
        "project": "aginctus-gateway",
        "compose": {
          "services": {}
        }
      }
    }
  ]
}
```

The schema is intentionally explicit about ordering. Steps execute in array order and stop on the first error.

The first version does not provide a dependency graph, implicit parallelism, retries, rollback, or cross-step transactions. Those can be added only when concrete workflows require them.

### Common step fields

Every step has:

- `id`: unique stable identifier within the plan;
- `driver`: the executor implementation;
- `configuration`: driver-specific typed data.

Future common fields may include timeout or conditional execution, but they should not be added preemptively.

## Jsonnet generation

Aginctus embeds its standard Jsonnet library using Go `embed.FS`.

A built-in entrypoint can therefore express orchestration without files installed beside the binary:

```jsonnet
local ag = import 'aginctus/orchestration.libsonnet';

ag.orchestration('local', [
  ag.apply('management-network', {
    documents: [
      ag.network('aginctus-mgmt', {
        'ipv4.address': 'auto',
        'ipv4.nat': 'false',
        'ipv4.routing': 'false',
        'ipv6.address': 'none',
      }),
    ],
  }),

  ag.compose('gateway', {
    project: 'aginctus-gateway',
    compose: {
      services: {},
    },
  }),
])
```

The interpreter is `github.com/google/go-jsonnet`, integrated as a Go module.

### Import policy

Jsonnet evaluation is trusted configuration generation, not a sandbox for arbitrary untrusted code.

Even so, Aginctus should use a restrictive importer:

- embedded Aginctus resources are always available;
- explicitly supplied orchestration source may import other explicitly registered resources;
- arbitrary filesystem and network imports are disabled by default;
- no shell or native-code escape hatch is provided;
- secrets are not exposed as Jsonnet external variables.

This keeps evaluation reproducible and avoids accidental host-file disclosure.

### Inputs

Aginctus passes non-secret effective configuration to Jsonnet as structured data.

Secrets are represented by references and resolved by the relevant runtime integration after plan generation. Provider tokens, SSH private keys, database passwords, and similar values must not appear in rendered plans, debug output, or Jsonnet traces.

## Apply driver

The apply driver consumes a list of resource documents compatible with the selected `incus-apply` version.

Aginctus imports `github.com/abiosoft/incus-apply` directly and invokes an exported library API. It must not execute the `incus-apply` binary.

### Integration gate

The current upstream module is primarily structured as an application and, at the time of this design, does not expose an obvious stable public reconciliation package. Its module also does not directly depend on the Incus Go client.

Therefore implementation must first verify an embeddable API that satisfies these requirements:

- accepts parsed configuration or an `io.Reader`, not only filesystem CLI arguments;
- accepts or creates an Incus connection without spawning the `incus` CLI;
- returns structured errors;
- supports the reconciliation semantics Aginctus requires;
- exposes dry-run or planning hooks if Aginctus is to preserve dry-run behavior.

If upstream does not expose that API, the integration work must add or contribute one. Aginctus must not work around the gap by forking a command.

Aginctus owns its outer orchestration schema and ownership policy; it should avoid copying the internals of incus-apply.

## Compose driver

Use the maintained `github.com/lxc/incus-compose` module directly rather than the archived historical fork.

The current project already exposes embeddable packages, including `project` and `client`. The integration should construct and execute project resources through these packages rather than invoking `incus-compose`.

The initial adapter should cover the subset needed by Aginctus:

- load a Compose model from generated content;
- stamp Aginctus ownership metadata on the Incus project and instances;
- reconcile project resources;
- start the resulting services;
- inspect status;
- stop/down without deleting persistent state unless explicitly requested.

The Aginctus adapter should remain narrow so changes in incus-compose do not leak into the public orchestration schema.

### Toolchain compatibility

Use the Go toolchain specified by the repository's current `go.mod` and Nix development shell. The selected dependencies are pinned in `go.mod`/`go.sum`; verify compatibility against those versions rather than inferring it from upstream development branches.

## Native Incus driver

The native driver reconciles Aginctus-owned Incus resources with the
official Incus Go SDK. It replaces the Apply driver for the resources it
supports, so the migration to native reconciliation does not require a
second orchestration contract.

The driver carries the typed intent of exactly one resource. The
reconciler behind it owns the lifecycle:

```text
typed desired state
        |
        v
observe actual state
        |
        v
plan / diff
        |
        v
validate ownership, scope and policy
        |
        v
execute guarded mutations
        |
        v
verify convergence
```

The current implementation reconciles the Aginctus management bridge. It
is the only host-shared resource: Aginctus projects disable
project-local networks, so the bridge lives in the Incus `default` project
and is referenced by name from project-scoped instances. The reconciler
therefore refuses any client that is not the local Incus Unix socket bound
to that project, refuses a bridge that does not carry the Aginctus
ownership markers unless adoption was explicitly authorized, treats the
DHCP gateway announcement as immutable policy drift rather than a value to
overwrite, writes only the managed configuration keys and preserves every
other observed key, and re-reads the bridge at the mutation boundary and
after the write.

The driver supports dry-run: the planning path performs no mutation.
Repeated reconciliation is idempotent, and a failed or uncertain write is
reported as uncertain rather than as success or as a rollback.

The management-network backend is selected with
`incus.management.network.backend`. See
[Configuration](../reference/configuration.md#management-network-reconciliation-backend).

### Native instance lifecycle

The scoped workload lifecycle commands reconcile the running state of one
existing instance in the explicitly selected Incus project through the same
native reconciler. `aginctus workload start --project NAME WORKLOAD-ID
--confirm-start` and the matching `stop` no longer spawn the `incus` CLI to
perform the transition.

The reconciler observes the instance, verifies the Aginctus ownership
markers, refuses an absent or foreign instance, rechecks the instance at the
mutation boundary, applies the state transition, waits for the Incus
operation and verifies the reached state. It never creates, replaces or
removes an instance, and a failed or unverified transition is reported as
uncertain rather than as success.

The stop transition keeps the graceful Incus CLI semantics: no forced stop,
no stored runtime state and no client-side timeout, so the Incus daemon
applies the same shutdown grace period as `incus stop`.

## Execution semantics

Execution is sequential and fail-fast.

For each step the executor:

1. validates the driver configuration;
2. reports the planned action when dry-run is enabled;
3. invokes the driver;
4. waits for the driver operation to complete;
5. records the step result;
6. proceeds only after success.

A failure identifies both the step ID and driver in the returned error.

A later step must never execute after an earlier step fails.

### Idempotence

Idempotence belongs primarily to the reconciliation drivers.

The Aginctus executor may run the same plan repeatedly and expects:

- matching resources to remain unchanged;
- managed drift to be reconciled;
- foreign collisions to fail safely;
- resources outside the plan not to be deleted unless the driver contract explicitly treats the plan as authoritative.

Aginctus ownership metadata remains mandatory at driver boundaries.

## Dry-run

`--dry-run` must remain an orchestration-level guarantee.

A driver can participate only if it can determine intended mutation without applying it. If an upstream library does not expose adequate planning support, its adapter must not claim dry-run support.

During migration, commands backed by a driver without dry-run support should fail with a capability error rather than silently mutating or reporting a misleading plan.

## Embedded orchestration resources

Built-in workflows live under a dedicated package, for example:

```text
internal/orchestration/
  engine.go
  plan.go
  drivers/
  resources/
    orchestration.libsonnet
    management-network.jsonnet
    herdr-client.jsonnet
    gateway.jsonnet
    workload.jsonnet
```

The `resources` tree is embedded in the Aginctus binary.

This lets the CLI generate a plan for built-in concepts without relying on a mutable installation directory. User-supplied orchestration can be added later without making built-in resources external files.

## Migration from ensure

Migration should be incremental.

Recommended order:

1. implement the engine, plan validator, restricted Jsonnet importer, and fake driver tests;
2. integrate the apply driver;
3. express management-network reconciliation as a built-in Jsonnet resource;
4. integrate the compose driver;
5. move the gateway deployment to Compose;
6. express Herdr client and agent workload infrastructure through orchestration;
7. remove bespoke Incus reconciliation only after equivalent lifecycle, dry-run, ownership, and teardown behavior is covered.

Existing CLI concepts should remain stable during migration. For example, `aginctus network ensure` may generate and execute a one-step orchestration plan rather than disappearing.

## Testing

The engine should be testable without Incus by injecting drivers.

Unit coverage must verify:

- deterministic Jsonnet output;
- embedded import behavior;
- rejection of filesystem imports;
- plan schema validation;
- duplicate step IDs;
- unknown drivers;
- sequential ordering;
- fail-fast behavior;
- dry-run capability enforcement;
- redaction of secret references and driver diagnostics.

Driver integration tests should run against a real disposable Incus environment where upstream behavior matters.

Do not mock the upstream libraries deeply enough that tests merely reproduce assumptions about them.

## Security

The orchestration layer is privileged infrastructure code.

Key requirements:

- no command-line subprocess integration for apply or compose;
- no arbitrary Jsonnet filesystem imports by default;
- no secrets in generated plans;
- validate all generated plans before the first mutation;
- preserve Aginctus ownership metadata and collision checks;
- do not allow Compose configuration to publish unintended host interfaces by default;
- sanitize driver errors before displaying data that may contain credentials;
- pin module versions through `go.mod` and `go.sum`.

## Initial engine implementation (historical implementation notes)

The first implementation establishes the Aginctus-owned orchestration contract without replacing an existing lifecycle path:

- `go-jsonnet` is integrated as the embedded evaluator;
- orchestration plans are decoded into typed Go structures with strict validation;
- `orchestration.libsonnet` is embedded in the binary;
- Jsonnet imports are restricted to embedded or explicitly registered in-memory resources;
- the engine validates every step before mutation, then executes drivers sequentially and fail-fast;
- dry-run execution is rejected when any selected driver cannot support it;
- unit tests use in-memory fake drivers and do not require Incus.

The orchestration layer now has two direct Go drivers. The Compose driver uses `github.com/lxc/incus-compose` for gateway services and intentionally does not claim dry-run support. The Apply driver uses the native backend from the pinned `blevinn/incus-apply` fork; it accepts trusted in-memory resource documents, uses `Plan` for real dry-run, and uses `Execute` for mutation without invoking the `incus` CLI. `management-network.jsonnet` expresses the Aginctus management bridge as an Apply step with ownership metadata. Gateway deployment evaluates a single ordered plan containing the management-network Apply step followed by the Compose gateway step. The standalone `network ensure` / `network teardown` commands use the same resource and Apply driver. `herdr-client.jsonnet` now expresses the Herdr infrastructure container, and `herdr client ensure` executes an ordered network-then-instance plan while requiring the instance to be running. `herdr client teardown` deletes only the Herdr instance. Existing-resource update/delete requires Aginctus ownership markers by default through incus-apply's `RequireExistingConfig` guard; `--force` deliberately disables that guard for explicit adoption or deletion. Native cloud-init and VM-agent waits remain unsupported in the current fork, while workload-instance reconciliation is implemented through the same Apply driver. Runtime-specific bootstrap and VM readiness remain follow-up work.


### Fail-closed immutable drift

Apply-backed Aginctus resources set `rejectUnsupportedChanges` when adopting or
reconciling existing resources. Ownership checks may be bypassed explicitly with
`--force`, but immutable identity/type drift is still rejected rather than silently
ignored. This keeps management-network bridge type and instance isolation semantics
(container versus VM) separate from ownership adoption policy.

### Native management-network reconciliation

The native driver reconciles the management bridge with the Incus SDK. The
`incus.management.network.backend` setting selects it; the legacy Apply
driver remains available and is the default until the native path is proven
against a disposable real Incus and the cutover in
[#159](https://github.com/blevinn/aginctus/issues/159). Every plan that
reconciles the bridge resolves the same setting, so one invocation never
mixes the two backends. That consistency is per invocation: Aginctus holds
no shared lock on the bridge, so concurrent invocations with different
effective settings can still target the same bridge.

The native path keeps the v0.1 command contract: `aginctus network ensure`
and `aginctus network remove` keep their arguments, their output and their
`--dry-run` and `--force` semantics. `--force` remains an explicit operator
decision and now maps to explicit adoption authorization instead of disabling
an ownership guard.

Adoption authorization covers the ownership markers only. The DHCP gateway
announcement is immutable policy: Aginctus writes it only when it creates
the bridge, and an existing bridge must already carry the configured
announcement, whether it is owned, foreign or being adopted. `--force`
never rewrites the announcement, and a network of another type is never a
removal target. A reconcile preserves unmanaged Incus configuration keys and
the operator-supplied network description.
