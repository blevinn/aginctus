# Declarative orchestration design

Status: implemented initial orchestration engine and direct Go Apply/Compose drivers for management-network, Herdr client, workload instance, and gateway deployment. Runtime-specific readiness, gateway credentials, and full guest bootstrap remain separate work.

## Goal

Replace Aginctus-specific imperative `ensure` flows with a small orchestration engine that evaluates trusted Jsonnet into a deterministic execution plan and then invokes Incus reconciliation libraries in sequence.

The first two execution drivers are:

- `apply` for individual Incus resources through `incus-apply`;
- `compose` for multi-service projects through `incus-compose`.

Aginctus must integrate both projects as Go modules. It must not shell out to either command-line program.

This design intentionally separates the Aginctus orchestration contract from the configuration syntax of either underlying project. Incus Apply and Compose remain implementation drivers rather than the public Aginctus model.

## Why change the current ensure model

The existing network and Herdr client commands prove the lifecycle semantics, ownership model, and configuration cascade, but each new resource currently adds bespoke reconciliation code.

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
      |             |
      v             v
  apply driver   compose driver
      |             |
      v             v
incus-apply Go  incus-compose Go
      \             /
       \           /
        Incus API
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
            "type": "bridge",
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
    workloads/
      opencode.jsonnet
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

## Initial engine implementation

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
