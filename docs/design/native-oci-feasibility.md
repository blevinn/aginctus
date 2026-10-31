# Native OCI feasibility and dependency inventory

Tracking: #153, #154, #158. Scope: the existing PostgreSQL and LiteLLM topology,
not arbitrary Compose applications. The disposable spike is test-only; it does
not activate native production writes or authorize existing-resource migration.

## Decision and evidence boundary

Proceed with a daemon-side OCI acquisition prototype using the official SDK's
`CreateImage` with an explicit project and an OCI pull source. This bypasses
Compose's cache project and HTTPS image relay. It does not require exporting or
unpacking images in the operator process. Incus itself performs image conversion
using its OCI implementation and daemon-side image tooling.

`internal/orchestration/nativeoci/spike_test.go` exercises both current public
gateway images against a local-only Incus daemon. It checks repeated import,
full fingerprint metadata, profile-free OCI initialization, application readiness,
start/stop/removal, project isolation and PostgreSQL custom-volume preservation
across explicit instance recreation on Btrfs. All credentials are synthetic.
The VM has no production configuration, credentials, mounts or Incus connection.

Run on a disposable KVM worker with registry access:

```sh
nix --quiet develop -c python3 scripts/vm_harness/runner.py test native-oci
```

The runner builds the pinned NixOS driver and records source provenance and
guest-test status. Normal Go tests skip this test. The explicit environment flag
and guest marker are accidental-execution guards, not authentication or proof
that an arbitrary host is disposable.

This is an acquisition/initialization feasibility gate, not native gateway
acceptance. The spike uses the current configured tags and no provider calls.
It does not prove immutable resolution, private registry authentication,
service-to-service networking, gateway secrets precedence, controller fencing,
interrupted upgrades, or migration/rollback. The existing `gateway-topology`
scenario continues to test the legacy production path.

## Image contract needed before production

- Resolve a registry tag to a platform-specific manifest digest before mutation;
  pull by that immutable digest, not by a tag that can move between observations.
- Record registry/manifest identity separately from the Incus fingerprint. The
  pinned SDK computes OCI fingerprints by hashing the ordered layer digest
  strings. Config-only changes can therefore share a fingerprint. Never equate
  fingerprint equality with complete image identity or stamp an existing image
  as owned merely because its layers match.
- Preflight project policy, alias, fingerprint collisions and image claims;
  repeat safety checks under the selected actor's mutation guard. No cross-project
  cache adoption or alias replacement based only on resource names.
- Create aliases explicitly and retain old images until references and recovery
  policy permit separately authorized cleanup. Report partial completion without
  automatic deletion when a pull or alias operation has an uncertain outcome.
- Retain OCI initialization metadata, environment defaults, entrypoint, working
  directory and UID/GID semantics. Review image-declared volumes and healthchecks
  instead of assuming all Compose behavior comes from the daemon.
- Current gateway defaults use public registries. No private-registry credential
  API is established by this spike. Do not put registry credentials in source
  URLs, image properties, plans, errors or argv. A narrowly scoped authenticated
  acquisition helper remains permissible if daemon-side acquisition cannot
  satisfy that requirement. It must not become another orchestration backend.
- Bound operation waits and propagate cancellation. Incus operation cancellation
  does not prove the server rolled back an import; inspect state before retry.

## Repository dependency inventory

The inventory covers tracked production code, tests, generated fixtures,
configuration, packaging, scripts and documentation on the v0.1 release baseline.

| Surface | Dependency and preserved behavior |
| --- | --- |
| `go.mod`, `go.sum` | Both direct libraries and their pinned fork replacements; Compose also brings Compose schema, registry and graph helpers. Keep the official Incus SDK. Remove indirect modules only after `go mod tidy` and dependency graph verification. |
| `internal/orchestration/drivers/apply/` | Apply parsing, planning and execution, ownership predicates, immutable-drift refusal and dry-run. |
| `internal/cli/workload_apply_scoped.go` | Direct Apply import for existing-only read-only scoped diff; the preview remains unauthorized for writes. |
| `internal/orchestration/drivers/compose/` | Project creation/ownership, OCI cache/seed and alias checks, service ordering, drift/recreation, command/config staging and device preservation. Extensive fake and disposable integration tests depend on upstream types. |
| `internal/orchestration/{plan.go,engine.go,resources/}` | Driver contracts and embedded Jsonnet Apply/Compose bodies for network, Herdr, workload and gateway. Preserve restricted imports, deterministic execution and fail-fast results. |
| `internal/{managementnetwork,herdrclient,workload}/` | Generate Apply resources and invoke the engine; retain host-owned bridge isolation, initialization/readiness, exact selection and state-volume safeguards. |
| `internal/gateway/` | Compose rendering, project/naming assumptions, credential mount audit, instance drift, egress attachment/runtime audit, safe removal and Compose-dependent tests. Preserve database/OAuth state, secret precedence, exclusive attachment and no host port publishing. |
| `internal/cli/{aggregate.go,run.go,gateway_status.go,workload_gateway_provision.go}` | Lifecycle dispatch, render/status and gateway project lookups; doctor reports/configures HTTPS for the current cache path. |
| `internal/config/`, `nix/{settings-options,nixos-module}.nix`, fixtures | Public `gateway.compose.project` and corresponding defaults/parity tests. Introduce an explicit compatibility failure or migration path, not implicit co-location/adoption. |
| `flake.nix` | Both Go binaries vendor the shared module graph and hash; the Incus executable wrapper also serves existing non-orchestration commands. No separate Apply/Compose Nix package is installed. Do not remove Incus CLI packaging merely to remove orchestration libraries. |
| `nix/vm-harness/gateway-topology.nix` | Legacy Apply-before-Compose regression, daemon HTTPS prerequisite, current project and instance names, OCI readiness and route checks. Native topology needs its own acceptance before cutover. |
| `justfile`, `scripts/vm_harness/`, `tests/` | Explicit Compose integration entry point, gateway scenarios, fixtures and runner safety tests. Keep regression evidence distinct from native acceptance. |
| `scripts/gateway/` | Selective-Go auditing uses Compose service labels; model-registration helpers contain legacy gateway naming assumptions. Migrate identity contracts along with the gateway, not independently. |
| `docs/` | Orchestration, gateway, egress, management DHCP, credential volume, scoped CLI, workspace and selective-Go documents reference legacy behavior. Preserve historical release evidence; update current operator instructions at subsystem cutover. |

Useful audit commands (tracked files only, excluding concurrent worktrees):

```sh
git grep -n -i -E 'incus-apply|incus-compose|incus_apply|incus_compose' -- ':!go.sum' ':!flake.lock'
git grep -n -E 'drivers/(apply|compose)|gateway\.compose|DriverApply|DriverCompose'
```

## Native simplifications and remaining gates

The gateway needs two known services, one database volume, protected credential
attachments and explicit NICs. Native handlers can eliminate generic Compose
parsing, implicit image-reader seed instances, whole-project teardown, temporary
Compose files and cross-project HTTPS cache copies. Keep runtime readiness and
secret resolution outside the serializable resource plan.

The management bridge remains host-owned in `default`, with no NAT, routing or
DHCP default gateway. A project-scoped workload action may validate an attachment
but must not provision or repair that bridge. Only LiteLLM may receive the
separately authorized egress attachment. Firewall policy is a separate authority.

Before #154 is complete, validate immutable pulls and relevant OCI config/volume
semantics and document any unsupported authentication. Before #158/#159, prove
full synthetic gateway readiness/dependencies, negative isolation, owned image
update/recreation, persistent state and credentials, cancellation/recovery and
explicit v0.1 migration/rollback. Keep legacy drivers and the HTTPS doctor check
until no supported path depends on them. No pilot operation is part of these tests.
