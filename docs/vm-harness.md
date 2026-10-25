# Disposable NixOS VM harness (v0.2, experimental)

Parent design [#425](https://github.com/blevinn/aginctus/issues/425). This is
**not** v0.1 pilot acceptance and does not verify real provider/OAuth traffic.

## Shared host declaration

The NixOS test-driver guest and the separately built persistent Incus VM
image import `nix/vm-harness/aginctus-host.nix`, the single declaration of
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

Run the harness under a **dedicated unprivileged Unix worker**, preferably
on a separate KVM runner/host, with no pilot credentials, host Incus privileges,
SSH agent, pilot network routes, or writable sensitive host paths. Guest root
can inspect all guest state. As a later slice, #428 will gain a typed guest
command transport that does not expose the Python test-driver console.

Nix builds can still download locked dependencies and consume host disk/CPU.
No real network isolation or quota is claimed by this runner alone. Existing
`just check-all` remains hostless and does not start this VM.

## Native CLI entrypoint (experimental)

`aginctus testenv vm` is the new typed operator CLI. In this first increment,
`list` and `plan` are offline and read-only, and `test` reuses **the existing
Python batch runner and its evidence files**, selected by fixed scenario name:

```sh
aginctus testenv vm list
aginctus testenv vm plan smoke
aginctus testenv vm test smoke
aginctus testenv vm test storage-network
```

Execute `test` **from the repository checkout** on an unprivileged,
dedicated test worker. The current Python script is sourced from that checkout,
which a coding agent may modify; this is **not** an immutable installed
supervisor or a safe way to grant an untrusted agent host execution.
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

From the repository root on x86_64-linux with KVM, using the dedicated worker:

```sh
nix develop -c just test-vm-runner
nix develop -c just test-vm-smoke
nix develop -c just test-vm-storage-network
nix develop -c just test-vm-runtime-lifecycle
nix develop -c just test-vm-gateway-topology
nix develop -c just test-vm-synthetic-gateway
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
## Synthetic Responses fixture (protocol layer)

`scripts/vm_harness/synthetic_responses.py` implements a small, local-only
OpenAI Responses-style upstream with a hardcoded **synthetic-only** key and
model, validated nonce, bounded request bodies, SSE terminal completion and
per-nonce request counts. It makes no external requests and suppresses
request/header logging. Hostless tests exercise its negative authentication
checks and streaming protocol. It is intentionally **not LiteLLM**: routing
through the real gateway, registered aliases/scoped virtual keys and genuine
OpenCode tool execution are separate required acceptance layers. Neither
the fixture's `response.completed` nor its nonce count proves gateway-side
billing/provenance or a coding-agent run. Never use its static key on a pilot.

## Next synthetic tool-protocol acceptance gate

Issue #531 now has an **opt-in loopback OpenCode tool probe** in
`guest/opencode/test_real_runtime.py`, with two modes that use the
real pinned OpenCode executable and the production Aginctus launcher:

- `--inspect-tools` observes only genuine bounded Responses tool
  declaration names/schema SHA-256 digests and native session headers;
  it **does not cause or verify tool execution**.
- `--execute-tools` replaces the local text-only mock with the
  explicitly selected `ToolSequence` fixture. The synthetic model
  requests `read`, `edit`, and `bash` (`go test ./...`) in order.
  The test fails without all three real tool continuations, an actual
  corrected `calc.go`, an independently observed tool-created test
  marker, and a fresh passing Go test in the isolated temporary workspace.

Neither mode uses Herdr, Incus, LiteLLM or real providers. The pinned
client must prove its actual tool-name/schema and continuation behavior
before this optional protocol can be admitted to the NixOS full-stack
scenario; the synthetic sequence deliberately rejects anything it does
not recognize. A redacted tool continuation does not itself attest
execution. Even a successful standalone client probe is **not** a
full VM/Herdr/gateway audit or network-isolation result. Existing
`synthetic-gateway` remains text-only and unchanged.

## Disposable VM execution of the actual OpenCode tool protocol

`just test-vm-opencode-tool-loopback` is a separate, heavyweight NixOS
test-driver VM scenario. It installs the actual flake-pinned OpenCode and
Go runtimes, boots without a default network route or SSH, and executes
the opt-in scripted Responses `read/edit/bash` test under a non-root
guest-only identity. The full test fixture is assembled into one immutable
Nix store tree; arbitrary host path selection and inherited testenv opt-in
flags are not forwarded.

The test requires actual source editing, a nonce-marked tool-created file,
and passing independent `go test ./...` in the private guest workspace.
It deliberately **does not** use Herdr, LiteLLM, the synthetic gateway VM,
or live providers. Its passing status must **not** be reported as nine-phase
full-stack or gateway-audit acceptance. A pinned tool schema/continuation
mismatch is a failed test requiring investigation, never a reason to return
a synthetic green result. No outer persistent Incus worker/MCP mutation is
enabled by this scenario.

## Real LiteLLM Responses function-call transport (pre-Herdr)

The opt-in `gateway_tool_transport.py` verifier builds four **fake-result**
Responses HTTP turns through the **real LiteLLM** alias and the test-only
synthetic upstream: `read`, `edit`, `bash`, then a terminal message.
It rejects mismatched response IDs, call IDs, ordering, missing terminal
completion, and unexpected upstream observation counts. The unchanged
`synthetic-gateway` test remains text-only. `synthetic_gateway_probe.py`
supports the separate trusted fixture startup switch `--tool-sequence`,
but no public client can remotely enable tool mode.

**No tool actually executes in this transport probe.** The
`function_call_output` bodies are explicit synthetic acknowledgements,
not legitimate agent receipts. This increment only isolates potential
LiteLLM Responses translation problems before real Herdr/OpenCode
integration under #558. It cannot satisfy #531/#431 by itself. The
separate operator-only NixOS VM scenario and packaged Python source
dependencies follow in the next stack increment.

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
Only run this on a separate unprivileged worker without pilot secrets.
Reports are mode `0600` under run-scoped directories and excluded from Git.

## Read-only concise evidence view

After a separately authorized VM run produces `.vm-harness-results/<run-id>/result.json`,
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
