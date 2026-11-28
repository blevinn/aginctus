# Disposable NixOS VM harness (v0.2, experimental)

For test tier selection and proof limits, start with the [testing overview](../README.md). Parent design [#425](https://github.com/blevinn/aginctus/issues/425). This is
**not** v0.1 pilot acceptance and does not verify real provider/OAuth traffic.

## Shared host declaration

The NixOS test-driver guest and the separately built persistent Incus VM
image import `tests/nix/vm-harness/aginctus-host.nix`, the single declaration of
host-level Aginctus/Incus services and pinned image defaults. The test-driver
wrapper owns only QEMU sizing; the persistent image owns the guest agent,
EFI disk and image packaging. Both leave workload/gateway reconciliation
operator-controlled until guest Incus is initialized. Sharing the host
module does not establish real KVM/outer Incus isolation or synthetic model
acceptance.

## Security and execution boundary

The opt-in smoke scenario uses the pinned NixOS test driver to launch a QEMU
VM with **real Incus inside the VM**. It does not connect to the host's Incus
daemon or need a host-root terminal. The runner accepts only a fixed scenario
identifier and strips inherited Incus remotes/sockets, SSH-agent forwarding,
external QEMU flags, Nix configuration overrides, and live integration opt-ins.

**Important limitation:** `vm-debug` opens the NixOS driver's interactive
*Python* console. That Python process executes on the calling host account;
`machine.succeed("...")` runs commands inside the guest, but arbitrary Python
code typed in the console can access the worker account's host files. The test
driver itself is not a security boundary. Do not give this console to an
untrusted agent with the same Unix identity as the pilot operator.

Trusted coding agents may execute repository-defined batch tests on their
operator-authorized development host, including a multipurpose host, under
[AGENTS.md](../../../AGENTS.md). No independently admitted worker, separate
Unix account or per-run approval is required for this local batch path.
Use the normal unprivileged build account; keep credentials, host Incus,
pilot services and sensitive host paths out of test inputs and effects.
A dedicated isolated worker (preferably a separate KVM host) remains
recommended and is required for untrusted agents. Guest root can inspect all
guest state; the host-side driver is not a security sandbox. As a later slice,
#428 will gain a typed guest command transport that does not expose the Python
test-driver console. Persistent/external worker admission is a separate gate.

Nix builds can still download locked dependencies and consume host disk/CPU.
No real network isolation or quota is claimed by this runner alone. Existing
`just check-all` remains hostless and does not start this VM.

## Managed worker entrypoint (experimental)

The fixed managed `native-instance` build is a separate operator path,
using the disposable-worker image rather than the legacy batch runner.
For local development testing, use an authorized x86_64 KVM builder with
sufficient storage and effective Nix sandboxing; no independent worker
admission is required. Record the exact tested source; formal acceptance
still requires a reviewed immutable checkout:

```sh
just testenv-worker-lifecycle
just testenv-managed-native-instance
```

The recipes request sandboxing, strip common inherited daemon/QEMU/SSH
overrides, select the fixed derivation and request no remote builders.
These options are not a substitute for verifying the actual Nix daemon
policy or caller identity. The build prints its output store path; inspect
`testenv-fixture-receipt.json` there. A cached result is not a fresh trial.
The worker scenario plan reports `managed_target` separately from the
legacy batch target; it remains `not_admitted` until runtime acceptance.
No ordinary VM test or MCP execution default changes. See the
[worker fixture runbook](testenv-incus-vm-image.md) for prerequisites.

## Integrated managed regression

From a clean, reviewed Git checkout on an authorized local x86_64 KVM
builder, run the fixed nine-fixture regression sequentially:

```sh
nix develop -c just testenv-regression > /tmp/testenv-regression.json
```

The existing Python batch launcher selects only the reviewed targets:
standalone native-instance, worker lifecycle, managed native-instance,
independent recovery, payload transfer, nested KVM, project admission,
runtime resources and offline network. No arbitrary Nix target, remote
builder, host Incus, debug console or live provider is accepted. Each build
requests sandboxing and no remote builders; verify the effective daemon
policy separately. Use a clean isolated worktree and sufficient disk.
The clean tracked-source requirement is specific to this integrated
evidence run, not a restriction on ordinary authorized development tests.

The JSON report records the revision and lock digest, per-fixture derivation
and output, validated receipts, outcome and observed freshness. Progress
and build diagnostics go to stderr, not the JSON report. No raw logs are
retained by this command. A cache hit is a successful derivation result
but remains freshness-unverified; only observed matching test-driver log
markers support a fresh-run observation. The standalone and independent
recovery results are associated with the evaluated derivation; the recovery
receipt itself does not embed source identity. Managed receipts must match
the revision, lock digest, expected scenario and every scoped gate. A failed
build or invalid receipt fails the suite and leaves remaining entries
not run. This is a distinct developer regression report, not a v1 VM
evidence receipt or admission token. No automatic CI, default migration,
physical reclamation or independent host teardown claim.

## Native CLI entrypoint (experimental)

The `native-instance` scenario boots an isolated real Incus daemon, imports a
minimal NixOS container image and exercises the native scoped-instance SDK APIs:
create, config/description update, repeated ensure, start/stop, dry-run without
writes, stale ETag refusal, attachment preservation, and unsafe drift rejection.
Its storage volume and read-only workspace fixture exist only inside the VM.
The packaged CLI also performs confirmed existing-only apply and stop as a guest
operator with real system/current-user unit discovery and a cooperative writer
lease. It verifies read-only previews, repeated apply/stop, controller denial and
missing-instance refusal. No actor evidence is injected into this CLI scenario.
It does not activate a controller or verify production migration/actor authority.

```sh
nix --quiet develop -c python3 tests/vm_harness/runner.py test native-instance
```

Requires an x86_64-linux KVM worker and sufficient disk space. The Go acceptance
tests skip without both the explicit opt-in and the guest-only marker. Hermetic
checks do not run the VM; a skipped Go test is not real-Incus acceptance.

## Native lifecycle stack scenarios

The joined lifecycle, stopped gateway-pair, and gateway stop/retention
scenarios are offline fixtures registered in `testenv vm list` and `plan`,
the batch runner, and the v1 receipt readers. From an authorized development
host or disposable worker, select one explicitly:

```sh
nix develop --offline -c just test-vm-native-joined-lifecycle
nix develop --offline -c just test-vm-native-gateway-stopped-pair
nix develop --offline -c just test-vm-native-gateway-stop-retention
```

The existing `acceptance-project-authority` scenario (`just
test-vm-acceptance-project-authority`) also covers exact-project TLS peer trust
lookup and revocation. Those assertions run inside the same fixture; a receipt
reports the scenario outcome, not a separately verified trust-lookup result or
a deployed state-service authorization guarantee.

`native-oci` is a separate networked feasibility spike, selectable only in
the Python runner and excluded from the v1 receipt readers:

```sh
nix develop -c python3 tests/vm_harness/runner.py test native-oci
```

The runner's outer `native-oci` runtime limit is 30 minutes to accommodate the
guest scenario's 25-minute test timeout plus VM startup/shutdown overhead. Use
an x86_64 Linux KVM worker with sufficient disk and public OCI registry access;
no credentials or live provider are required. Receipts intentionally report
`cleanup: not_verified` and `upstream_inference: not_verified`. An offline
scenario's successful Nix build may reuse a realized output; the receipt cannot
establish fresh VM execution or independently verify cleanup.

The separate accepted-workload observation gate runs on an x86_64 Linux KVM
worker:

```sh
env -u QEMU_OPTS -u QEMU_KERNEL_PARAMS -u SSH_AUTH_SOCK \
  timeout --kill-after=15s 12m nix --quiet build --no-update-lock-file --no-link -L \
  .#vm-harness-native-accepted-incus-observation
```

Use `--rebuild` only when the exact target has already been built. It is rejected
for an unrealized target; a normal build is the fresh invocation in that case.
The current local VM exercise used Incus client/server 7.5.1, passed all three
observer subtests, verified both project inventories unchanged, and completed
QEMU/VM cleanup. This is exercise-only evidence from this host, not proof of a
dedicated worker or live provider.

The observer runs as an unprivileged guest user with an ephemeral,
project-restricted TLS credential and a same-UID immutable revision fixture.
It checks selected convergence and drift, permits absence only in the verified
project, and rejects wrong scope and a readable same-project foreign owner.
Check the guest Go `PASS` and unchanged-inventory message in the build log.
The fixture does not establish production accepted-state provenance, a writer
grant, privileged activation, or operator acceptance. The synthetic enrolled
TLS client identity is confined to the disposable guest and restricted to the
`team` project; the Unix admin socket is denied to the observer. This checks
Incus API authorization behavior, not host containment or production credential
provisioning.

To reproduce the local exercise, ensure Nix flakes, x86_64 Linux KVM
(`/dev/kvm`), Incus, and enough scratch disk are available. Before running the
build above, use `git rev-parse HEAD` to record the revision and verify no live
provider or host daemon is selected; the target starts its own disposable
Incus guest. Local results are exercise-only and do not establish behavior on a
dedicated worker or a live provider.

The last recorded hermetic snapshot was eight passing suites. The standalone
Go race suites and layout check passed on the refreshed PR candidate; the
standalone VM test skipped without its VM opt-in, as intended. Nix formatting
passed after formatting the merged flake definitions. After merging `main`,
rerun `just check ci`, `just check layout`, Nix formatting, the race suites, and
the VM target on the resulting head. GitHub checks for the refreshed head are
not yet available.

`aginctus testenv vm` is the new typed operator CLI. In this first increment,
`list` and `plan` are offline and read-only, and `test` reuses **the existing
Python batch runner and its evidence files**, selected by fixed scenario name:

```sh
aginctus testenv vm list
aginctus testenv vm plan smoke
aginctus testenv vm test smoke
aginctus testenv vm test storage-network
```

Execute `test` **from the repository checkout** on an authorized development
host or dedicated test worker using an unprivileged account. The Python script
is sourced from that checkout, which a coding agent may modify; this is
**not** an immutable installed supervisor or a safe way to grant an untrusted
agent host execution.
Native persistent VM exec/up/reset/destroy and MCP tool execution remain
disabled until the separate trusted supervisor and permission work lands.
`list`, `plan` and `help testenv` do not load host configuration or
contact the host Incus daemon.

## Local MCP entrypoint (read-only initial release)

`aginctus testenv mcp` launches a **local stdio MCP server**. It does
not listen on TCP or HTTP. This increment targets MCP protocol
`2026-07-28` with `server/discover`, `tools/list`, and `tools/call`.
The JSON-RPC request must include
`params._meta["io.modelcontextprotocol/protocolVersion"] = "2026-07-28"`
and `params._meta["io.modelcontextprotocol/clientCapabilities"] = {}`.
The stdout stream contains only newline-delimited JSON-RPC messages.
Missing required metadata returns `-32602` (Invalid params). Unsupported
versions return `-32022` (Unsupported protocol version), with `data.supported`
and `data.requested`, including on `server/discover`; clients may retry with
a supported version. Legacy `initialize` handshakes are not supported.

MCP tools currently available:

- `vm_list_scenarios`: read-only list of the same scenarios as
  `aginctus testenv vm list`.
- `vm_plan` with argument `{"scenario":"smoke"}`: read-only pinned
  target description, equivalent to `aginctus testenv vm plan smoke`.

`vm_test`, `vm_exec`, `vm_create`, `vm_destroy` and arbitrary
shell commands are deliberately **not exposed through MCP**. Merely connecting
an MCP client is not authorization for VM execution. Persistent, agent-safe
VM operations must first pass the trusted supervisor/worker security gate.

To add a stdio MCP connection to a client, configure its command as
`aginctus` and its argument array as `["testenv", "mcp"]` (use the
trusted installed CLI binary, not a shell string or a network URL). The
server's source checkout does not establish a credential or authorization
boundary. Use only on a dedicated non-privileged worker.

## Commands

From the repository root on an authorized x86_64-linux development host or worker with KVM:

```sh
nix develop -c just test-vm-runner
nix develop -c just test-vm-smoke
nix develop -c just test-vm-storage-network
nix develop -c just test-vm-runtime-lifecycle
nix develop -c just test-vm-gateway-topology
nix develop -c just test-vm-synthetic-gateway
nix develop -c just test-vm-synthetic-tool-gateway
nix develop -c just test-vm-ownership-recovery
nix develop -c just vm-debug
```

Within the interactive driver, use `start_all()`, then commands such as
`machine.wait_for_unit("incus.service")` and
`machine.succeed("incus info")`. The driver session and VM are disposed when
the driver exits. Use only synthetic data in guest fixtures.

The initial smoke fixture uses an Incus `dir` pool. The separate
`storage-network` scenario provisions a Btrfs pool on a guest-local loop image,
checks snapshot/daemon-restart visibility, and tests Aginctus management bridge
idempotency and foreign-resource protection. The `runtime-lifecycle` test imports real Herdr and OpenCode images,
reconciles their Incus instances, and invokes the real CLI's gateway render
contract; it **does not yet boot LiteLLM/PostgreSQL or prove inference**.
The `synthetic-gateway` scenario runs that same real gateway, then
starts an in-VM test-only upstream on the guest's private egress bridge.
It registers a synthetic-only Responses model with actual LiteLLM using
the guest's protected master key, issues a scoped virtual key through the
real gateway, checks unauthenticated model access rejection, then requires
terminal SSE with a unique nonce and an independent count from the
synthetic upstream. **No external provider is contacted.** This proves
synthetic LiteLLM model routing only: it does not establish Herdr/OpenCode
tool execution, gateway-side audit correlation, or packet-level isolation.
The reporter must continue to show `upstream_inference: not_verified` for
**real provider inference** even if synthetic routing succeeds.

The opt-in `gateway-topology` scenario goes further: it calls the real
`aginctus gateway up` orchestration path, then requires Postgres `pg_isready`,
LiteLLM's real `/health/liveliness`, and coexisting Herdr/OpenCode workloads.
It also checks the management network's no-NAT/no-routing policy. It
may need OCI registry access to download the pinned-version images inside
the disposable guest and configures Incus HTTPS **only on guest loopback**
for the compose image cache. This is not a hermetic/image-digest-pinned OCI
test, not an agent workflow, and does **not** send a provider request or prove
gateway model routing. It deliberately fails if real gateway deployment
or application readiness fails; a rendered Compose model is insufficient.

For this networked scenario the runner builds the pinned `.driver` output,
then executes its fixed batch script outside the Nix build sandbox in a fresh
temporary working directory, with the same sanitized environment and a
15-minute timeout. Ordinary `nix build .#vm-harness-gateway-topology` cannot
fetch OCI images inside a network-isolated build sandbox. The fixture selects
the locked `pkgs.incus` daemon for OCI gateway controls, limits host DHCP to
the outer NIC, permits only DNS/DHCP on the inner bridges, and keeps L2 bridge
traffic separate from the routed management denial. An explicit management
CIDR makes LiteLLM use management DNS, so PostgreSQL lookup does not depend
on which of its two NICs happens to be enumerated first. Gateway Compose
rendering and deployment both set this DNS server when a management IPv4 CIDR
is configured.

Subsequent slices add registered synthetic Responses traffic, real
Herdr/OpenCode tool execution, packet-level negative network assertions,
fault injection, and independently verified resource/evidence cleanup.
## Scenario procedures and historical anchors

The detailed synthetic Responses, OpenCode tool, Herdr full-stack and immutable
OCI staging scenarios are maintained in the [scenario catalog](scenarios.md).
The harness setup above and the reporting/verification boundary below apply to
all those fixtures. The following former headings remain as link targets for
older references; their commands now live in one dedicated catalog.

### Synthetic Responses fixture (protocol layer)

See the [scenario procedure](scenarios.md#synthetic-responses-fixture-protocol-layer).

### Next synthetic tool-protocol acceptance gate

See the [scenario procedure](scenarios.md#next-synthetic-tool-protocol-acceptance-gate).

### Disposable VM execution of the actual OpenCode tool protocol

See the [scenario procedure](scenarios.md#disposable-vm-execution-of-the-actual-opencode-tool-protocol).

### Real LiteLLM Responses function-call transport (pre-Herdr)

See the [scenario procedure](scenarios.md#real-litellm-responses-function-call-transport-pre-herdr).

### Synthetic Herdr/OpenCode full-stack scenario (#558)

See the [scenario procedure](scenarios.md#synthetic-herdropencode-full-stack-scenario-558).

### Test-only immutable OCI staging interface (#532)

See the [scenario procedure](scenarios.md#test-only-immutable-oci-staging-interface-532).

## Machine-readable run evidence

Every `runner.py test <scenario>` invocation writes an independent,
per-run `.vm-harness-results/<random-id>/result.json` with the scenario,
pass/fail from the Nix derivation or networked batch-driver exit status, lockfile fingerprint,
base repository revision, tracked-source dirty status and UTC timestamp.
Provenance is captured before the build; unavailable Git or lockfile metadata
is recorded as `null`. The base revision alone does not identify dirty source,
and this snapshot does not prevent concurrent source changes. Launch failures
also produce a failure report with `failure_stage: launch`; a nonzero build
exit records `failure_stage: build`; networked batch-driver failures record
`failure_stage: runtime`. It **always** records
`upstream_inference: not_verified` and `cleanup: not_verified`; these
are not inferred from the presence of an Incus service or a green mock.

The report contains no VM transcript, raw headers, credentials, or provider
payload. It does **not** prove that cleanup completed, establish a cryptographic
attestation, or sanitize diagnostic output printed by the Nix builder.
Run using the authorized unprivileged batch path described above; do not expose pilot secrets to the test.
Reports are mode `0600` under run-scoped directories and excluded from Git.

## Read-only concise evidence view

After an authorized VM run produces `.vm-harness-results/<run-id>/result.json`,
copy its **24-character hexadecimal run ID** from the report path. Inspect the
*existing* record from the same repository checkout:

```sh
nix develop -c just check vm-report <run-id>
nix develop -c just check vm-report-json <run-id>
```

These commands do not discover or start VMs, run Nix/Incus, read host Incus
configuration, or enable live acceptance. They validate the original private
v1 evidence contract, verify owner-only directory/file permissions, refuse
symlinks, and read only the chosen report (up to 16 KiB). Their output is
bounded and synthetic-only: a short terminal summary or one JSON object on
stdout. Passing records exit 0; failed, missing or invalid evidence exits
nonzero, and invalid evidence produces a generic `error` result instead of
printing raw payloads or filesystem paths.

The view uses the same **conceptual** report envelope as hermetic tests
(`schema_version`, `profile`, `status`, `summary`, `results`) but carries
`profile: disposable_vm`. It never calls the hermetic CI reporter or appends
VM results to `just check ci`. The optional source metadata is sanitized and
allowlisted. Every view retains the original
`upstream_inference: not_verified` and `cleanup: not_verified`, even when
the VM derivation passed. This is a presentation of the original evidence,
not an additional test or an attestation. A run under another Unix user cannot
be inspected by silently relaxing evidence permissions; use that worker
identity or move only an explicitly reviewed redacted report.

Never record a synthetic provider test as evidence for [#124](https://github.com/blevinn/aginctus/issues/124).
