# Native OCI feasibility and dependency inventory

Tracking: #153, #154, #158. Scope: the existing PostgreSQL and LiteLLM topology,
not arbitrary Compose applications. The disposable spike is test-only; it does
not activate native production writes or authorize existing-resource migration.

## Decision and evidence boundary

**Go for the supported public-image strategy:** use daemon-side OCI acquisition through the official SDK's
`CreateImage` with an explicit project and an immutable OCI pull source. This bypasses
Compose's cache project and HTTPS image relay. It does not require exporting or
unpacking images in the operator process. Incus itself performs image conversion
using its OCI implementation and daemon-side image tooling.

`internal/orchestration/nativeoci/spike_test.go` exercises both current public
gateway images against a local-only Incus daemon. It checks repeated import,
full fingerprint metadata, profile-free OCI initialization, application readiness,
start/stop/removal, project isolation and PostgreSQL custom-volume preservation
across explicit instance recreation on Btrfs. All credentials are synthetic.
The PostgreSQL case verifies the resolved OCI entrypoint, command, major
version and data path, then hashes the actual entrypoint file against the
[reviewed 17 Alpine source](https://github.com/docker-library/postgres/blob/ac9627dcf544a8454f212a12ddeebfcdc0803d3e/17/alpine3.24/docker-entrypoint.sh)
before staging fixture files through the disposable administrator's file API.
It checks file ownership and permissions, explicit SCRAM login, a wrong-password
negative control and no credential bytes in Incus instance configuration. After
recreation with a different bootstrap file, the original login and data marker
must survive. The fixture then changes the role password over an authenticated
SCRAM connection using `psql \password`, with the replacement supplied only on
stdin. It requires the new login to read the existing marker and the old login
to fail, both before and after a guest restart. The fixture does not approve a
production delivery or rotation mechanism or attest the image build. A changed
upstream entrypoint requires fresh review before updating the expected digest.
The VM has no production configuration, credentials, mounts or host Incus connection.
Its attachment-audit fixture creates and re-observes PostgreSQL storage named
from an environment UUID on the selected Incus server/project. It exercises
no-op reuse and refusal of same-project volumes with missing or foreign
environment markers. New volume creation requires a writer guard; the fixture
owns its disposable writer, and no production command invokes this path.

`nativeoci.ResolvePublic` reads registry metadata using ORAS and anonymously
negotiated registry bearer authentication. It requires a unique matching Linux
platform in an index, independently validates the image config's platform,
checks SHA-256 descriptor integrity and bounds metadata documents to 4 MiB.
It returns the platform manifest digest, config digest, expected Incus layer
fingerprint and runtime initialization metadata. Config values are excluded
from serialized identity reports and upstream diagnostics are not propagated.
All metadata and anonymous-token transport requires HTTPS; no ambient credential
helpers are read. Context cancellation and request timeouts bound registry reads.

The spike pulls both images by the resolved manifest digest and checks the
actual daemon fingerprint against the independently computed layer fingerprint.
Unit tests demonstrate config-only changes with equal Incus fingerprints but
different manifest/config digests. ORAS is a narrow metadata dependency already
present in the module graph, not a second resource orchestration backend.

Run on a disposable KVM worker with registry access:

```sh
nix --quiet develop -c python3 tests/vm_harness/runner.py test native-oci
```

The runner builds the pinned NixOS driver and records source provenance and
guest-test status. Normal Go tests skip this test. The explicit environment flag
and guest marker are accidental-execution guards, not authentication or proof
that an arbitrary host is disposable.

This is an acquisition/initialization feasibility gate, not native gateway
acceptance. The spike resolves the current configured tags once, then acquires
by immutable digest, with no provider calls.
It does not prove private registry authentication, config-collision-safe reuse
of preexisting daemon image records, service-to-service networking, gateway secrets precedence, controller fencing,
interrupted upgrades, or migration/rollback. The existing `gateway-topology`
scenario continues to test the legacy production path.

### Offline stopped-pair composition gate (#158)

On a [disposable x86_64 Linux KVM worker](../testing/testenv/vm-harness.md), run
the focused NixOS test independently of the networked spike:

```sh
timeout --kill-after=15s 12m nix --quiet build --no-update-lock-file --no-link -L \
  .#vm-harness-native-gateway-stopped-pair
```

The VM uses fixed environment UUIDs, its local-only Incus daemon, an explicit
Btrfs pool and two VM-owned bridges with DHCP disabled. It composes topology
binding, read-only stopped-pair preview before volume ensure, device and
instance planning, and stopped creation. The preview refuses a foreign guest
even when the database volume has yet to be created.
It verifies the exact environment-owned PostgreSQL volume and both stopped
guests' image fingerprints and devices. Repeat must forward no volume or
instance submissions. Revoking the fixture grant after the PostgreSQL
submission preserves the verified PostgreSQL guest and reports LiteLLM as
not attempted. A same-project LiteLLM name occupied by another environment is
refused without changing that guest or creating PostgreSQL.

The test uploads deterministic, already-converted, non-bootable image bytes.
Their SHA-256 fingerprints are checked against Incus, and synthetic manifest
and config pins come from fixture-owned data, independently of live alias
lookups. The real project, aliases and image records must still pass the
executor's reuse checks. This does not prove upstream OCI resolution or
acquisition, OCI layer-to-fingerprint conversion, guest networking, service
readiness, credential delivery, production writer exclusion or cutover. The
disposable fixture is the only writer; its revocable grant confers no authority
outside that VM.

### Offline reverse-order stop and retention gate (#158)

On the same type of disposable KVM worker, build the separate gate with
substitution disabled so a missing test result must be built locally. If the
result already exists locally, add `--rebuild` to force fresh execution and
inspect the guest test and QEMU teardown logs.

```sh
timeout --kill-after=15s 12m nix --quiet build --no-update-lock-file --no-link \
  --option substitute false -L .#vm-harness-native-gateway-stop-retention
```

Each guest boots only a static, signal-aware synthetic process. The fixture
exercises guarded PostgreSQL-role-first startup, a PID 1 handshake before the
LiteLLM-role submission, and start repeat without new submissions. Its test-only
delivery verifier never handles credentials. The test seeds
and syncs fresh marker bytes through the PostgreSQL-role guest's database mount.
It exercises the dormant stop executor, observing LiteLLM stop before any
PostgreSQL stop request, both signal handlers, and stopped state. It reads the
exact marker directly from the retained custom volume without restarting either
guest. Repeat must submit no changes. A separate case revokes the fixture grant
between stops, preserves the stopped LiteLLM and running PostgreSQL, then
resumes without stopping LiteLLM again. A running sibling environment in the
same project and its volume are checked for drift in both cases.

A companion case replaces one explicitly selected stopped LiteLLM incarnation
with a different preverified image and checks that PostgreSQL is unchanged.
It then removes the new stopped LiteLLM guest followed by the stopped PostgreSQL
guest and reads the original marker from the retained custom volume. The
library has no production command. Its guard must independently authorize
discard of the exact root filesystem, including any managed OAuth state, and
recheck the scope, old and new pins, and writer exclusion at each effect.
Removal fails closed on snapshots, backups, unverified inventory or nonstandard
root configuration; missing targets do not permit blind retries. The Incus
DELETE endpoint has no ETag precondition. PostgreSQL image changes and storage
migration are outside this operation.

This is marker-byte retention and graceful shutdown evidence for synthetic
processes. PostgreSQL data integrity and readiness, live gateway behavior,
credential delivery, production writer exclusion, and upgrade/recreation or
cutover are unverified by this gate.

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
| `tests/nix/vm-harness/gateway-topology.nix` | Legacy Apply-before-Compose regression, daemon HTTPS prerequisite, current project and instance names, OCI readiness and route checks. Native topology needs its own acceptance before cutover. |
| `justfile`, `tests/vm_harness/`, `tests/` | Explicit Compose integration entry point, gateway scenarios, fixtures and runner safety tests. Keep regression evidence distinct from native acceptance. |
| `tools/gateway/` and `plugins/litellm/runtime/` | Selective-Go auditing uses Compose service labels; model-registration helpers have historical gateway naming assumptions. Reconcile identity contracts with the gateway architecture separately from this source-layout migration. |
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

Private authenticated registries are not supported by `ResolvePublic`; no
implicit credential-helper discovery or private acquisition fallback is provided.
Before #154 is complete, finish the daemon config-only collision/cache policy
and image-declared healthcheck/volume compatibility assessment. The remaining
implementation effort is resource-specific: guarded import/alias ownership,
explicit two-service startup/readiness and attachment/persistence policy. It
does not require building a generic Compose executor.

Before #158/#159, prove
full synthetic gateway readiness/dependencies, negative isolation, owned image
update/recreation, persistent state and credentials, cancellation/recovery and
explicit v0.1 migration/rollback. Keep legacy drivers and the HTTPS doctor check
until no supported path depends on them. No pilot operation is part of these tests.
