# Internal package ownership and closeout ledger

Tracking: [#799](https://github.com/blevinn/aginctus/issues/799). Architectural destination:
[plugin-oriented core](plugin-oriented-core.md).

## Scope and snapshot

This is the **core ownership** ledger, not a declaration that [plugin
migration #797](https://github.com/blevinn/aginctus/issues/797), environment
authority, or manual acceptance is complete. Baseline: `main` at
`52c099bb980095ff357758c85925fd890446e510` (2026-11-21, after #985).
The recursive Git tree contains **18** immediate `internal/` subtrees.
Numbers below count tracked blobs including fixtures and documentation; tests
are included among Go files. This replaces the original 2026-11-15 inventory
of 22 subtrees (494 blobs, 478 Go files, 237 Go test files), rather than
presenting the old table as current.

| Subtree | Blobs | Go | Go tests | Architectural owner / remaining disposition |
| --- | ---: | ---: | ---: | --- |
| `cli` | 184 | 177 | 91 | Command composition, request adapters, rendering; further root cleanup #725/#755 |
| `config` | 12 | 9 | 6 | Candidate configuration and precedence; not accepted-state authority |
| `filestate` | 8 | 8 | 4 | Narrow private-file/locking protocol; retain separately from state schema and secrets ownership |
| `gateway` | 79 | 78 | 40 | Concrete gateway behavior and transitional operations; extract through #805 |
| `gatewaycontrol` | 6 | 6 | 1 | Transitional provider control and concrete adapters; reconcile under #806 |
| `herdrclient` | 2 | 2 | 1 | Herdr-specific planning/transport; migrate under #803 |
| `host` | 33 | 32 | 17 | Host operations, authority, networking and firewalld executor; #985 |
| `incus` | 30 | 30 | 15 | Incus access, image and volume lifecycle; distinct from accepted state |
| `mcpschema` | 9 | 7 | 4 | MCP schema and policy/evidence; preserve grant boundary, evolve under #806 |
| `modelregistry` | 2 | 2 | 1 | Typed model registry and its persistence protocol; audit with #806 |
| `oauthgateway` | 2 | 2 | 1 | Dedicated OAuth HTTP/protocol surface; retain until ownership migration is proven |
| `observability` | 10 | 10 | 5 | Runtime progress/status and reporting; #801, distinct from test evidence |
| `orchestration` | 105 | 100 | 58 | Drivers, management-network planning, accepted-state coordination; #799/#800 |
| `plugins` | 28 | 26 | 12 | Core registry, deterministic build generator and application composition; #802 |
| `providerregistry` | 2 | 2 | 1 | Provider metadata registry/persistence; do not merge into model registry by name alone |
| `responsescontract` | 2 | 2 | 1 | Responses wire/protocol verification; distinct from provider identity |
| `testing` | 56 | 50 | 24 | Disposable testenv execution and classified validation evidence; distinct from runtime health |
| `workload` | 8 | 8 | 4 | Scoped workload rules and transitional assembly; continue via domain/CLI and plugin tracks |

## Established boundaries

- `internal/host/net/firewall` owns reusable host network policy and
  firewalld command transport; gateway-specific topology, egress discovery and
  audit policy stay with the gateway until #805. #873, #964 and #985 established
  the split. `gateway.SystemFirewalld` is a compatibility delegate, not a
  separate permission or privileged runtime.
- `internal/incus/statevolume` and `internal/incus/images` own Incus object
  lifecycle. `internal/orchestration/state` owns configured/accepted/current
  revision coordination. Neither a container volume nor a file path is authority
  to promote accepted state, cross projects or bypass controller fencing (#704).
- `internal/orchestration/managementnetwork` owns the bridge reconciliation
  plan; host link/firewall verification remains with `host/net`. The recovery
  PR #884 ensured the #875 move reached `main`.
- `internal/testing/testenv` and `internal/testing/validation` own operator
  test execution versus evidence catalog/reporting, respectively. #973
  reconciled #949's branch-only merge and preserved Nix output compatibility.
- `internal/plugins` owns pure registry/graph operations;
  `internal/plugins/build` owns static manifest generation; and
  `internal/plugins/composition` is the application-selected composition
  point. #974 and the follow-up composition recovery established that layout.
  Build inclusion does not enable a runtime instance or grant authorization.
- `pkg/` contains intentionally public Go contracts; product implementations
  at repository-root `plugins/` cannot rely on internal implementation imports.
  Generic registry/build packages must not import the application composition
  point or concrete root product plugins.

## Executable checks and what they prove

- `internal/host/net/firewall/import_direction_test.go` guards the host
  firewall against CLI, gateway and concrete-product imports.
- `internal/cli/architecture_guards_test.go` guards command/operation direction
  and Cobra composition.
- `internal/plugins/ownership_imports_test.go` guards public/implementation
  and generic-plugin/application-composition import direction. These are static
  source-level rules, not authentication, confinement or deployment tests.
- Existing external-module plugin fixtures, build-selection tests and CLI
  ownership tests continue to test their own contracts.

## Closure boundary and validation

After the closeout PR passes the full repository gate and the current import
ledger is reviewed, close **#799**. Remaining product-specific moves belong to
#803–#806, code generation/build selection to #802, orchestration behavior
to #800, observability to #801, CLI extraction to #725/#755, and full
end-to-end retirement/acceptance to #808. Do not reopen #799 for unrelated
product feature work merely because a specialist directory still exists.

Required before the closeout merge:

```sh
nix develop --no-update-lock-file -c go test ./internal/plugins/... ./internal/host/net/firewall ./internal/cli/...
nix develop --no-update-lock-file -c go test ./...
nix develop --no-update-lock-file -c just check all
nix flake check --no-update-lock-file
git diff --check
```

Required PR CI and any applicable targeted Nix checks must pass on the final
head. The source-only audit does **not** assert a manual NixOS host dry-run,
disposable Incus acceptance, live inference or provider authorization.

## Historical note

The initial inventory, at `5039da74273d812cca85fd55aa69609e38abe2ba`,
classified `firewall`, `statevolume`, `managementnetwork`,
`acceptedstate`, `testenv`, `validation`, `pluginbuild` and
`appplugins` as separate top-level packages. Their move histories are
#872–#875, #884, #886, #948–#949, #973–#975, and the subsequent recovery
work. The current table, not historical issue titles or merge status, is the
source of truth for what reached `main`.
