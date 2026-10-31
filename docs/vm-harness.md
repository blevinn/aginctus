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
nix develop -c just test-vm-synthetic-tool-gateway
nix --quiet develop --no-update-lock-file -c just test-vm-full-stack
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
integration under #558. It cannot satisfy #531/#431 by itself. The opt-in `synthetic-tool-gateway` NixOS scenario packages the synthetic
provider, state machine, gateway probe and transport verifier in the same
immutable Nix source directory. It runs real Incus/LiteLLM/PostgreSQL and
issues a short-lived scoped virtual key, then verifies four function-call
HTTP turns and key revocation. It cannot validate real guest tool actions
because the function results are **explicitly fake**. The text-only
`synthetic-gateway` scenario remains unchanged. This networked scenario
is heavyweight, is not part of check-all, and requires a disposable KVM
worker and OCI provisioning. The separate full-stack implementation below
adds actual Herdr/OpenCode execution; its dedicated-worker acceptance remains
an explicit gate.

## Synthetic Herdr/OpenCode full-stack scenario (#558)

`just test-vm-full-stack` selects the fixed `full-stack` registry entry and
`vm-harness-full-stack` NixOS driver. It is opt-in, excluded from hermetic CI,
and must run only on the dedicated, unprivileged x86_64 KVM worker described
above. No suitable worker was used to accept this implementation. A passing
hostless test or derivation evaluation does **not** establish a working E2E run.
Existing topology acceptance is separate evidence and need not be rerun just
to validate this scenario's hermetic changes.

The scenario imports the [test-only OCI staging module](#test-only-immutable-oci-staging-interface-532),
uses its native-platform immutable leaf digests, and checks staged aliases
before and after real `gateway up`. After provisioning it installs guest-local
IPv4/IPv6 outer-interface output/forward reject rules with packet counters.
An attempted external packet during deployment or agent execution fails the test. Workload
containers remain management-only; LiteLLM alone reaches the synthetic
listener on the private egress bridge. No live provider is registered.

Full-stack initializes a guest-local **20 GiB loopback Btrfs `default` pool**,
using the explicit loop-pool pattern from `storage-network.nix`. All workload
and gateway roots and custom volumes use that pool. The driver independently
reads its driver, guest-local loop backing and cross-project volume inventory
before the scenario and in a `finally` block after success or failure. Scoped
workspace cleanup must preserve the existing storage inventory. This is
pilot-representative Btrfs behavior, not real-device performance or deletion
evidence: the pool, images, persistent volumes, possible snapshots and loop
file remain for outer VM/disk disposal. No host pool is accessed or pruned.

The real production Herdr/OpenCode workload image is extended **only for this
test** with the pinned Go toolchain and immutable guest-root credential helper.
Herdr client SSH identity creation,
authorization, host-key pinning and machine registration happen inside the
fresh disposable VM through the shipped helpers and CLI. No host credentials,
host workspaces or ambient SSH agent are transported.

`scripts/vm_harness/full_stack.py` owns a run-scoped fixture and in-process
adapter with the existing nine phase names and workspace/run-ID arguments:

```text
preflight → open → start → prompt → wait → credential-check → gateway-evidence
                                              ↘ stop → close
```

The existing mock/live shell dispatcher and live marker requirements are
unchanged. This synthetic adapter never emits `upstream-inference-confirmed`.
It does not install hooks into, enable, or authorize live acceptance.

Required gates, each failing closed:

- The deliberately broken Go fixture must fail its actual Go assertion first.
- Before scoped workspace operations, an independent observer creates a foreign
  sentinel workspace and snapshots the complete workspace-ID/label inventory,
  including any unrelated existing workspaces. A separate real workspace
  undergoes a fixed injected post-open failure, scoped close and independent
  inventory comparison before the successful agent scenario starts. Both that
  failure path and the agent path must restore the exact baseline inventory.
  A missing/changed sentinel, changed unrelated entry or extra workspace fails.
  Scoped cleanup never closes the sentinel or any unrelated baseline entry;
  the sentinel remains until complete VM disposal.
- Real Herdr creates the labeled workspace, starts the pinned OpenCode agent,
  submits a nonce-bearing task and waits for done/idle. The trusted test
  upstream requests `read`, `edit`, and `bash` in order; no fake tool results
  are supplied by the harness.
- Independent `opencode session list`/`export` readback must find exactly one
  native session for that workspace, three completed tool parts with exact
  call IDs/inputs, the terminal synthetic message and a stop finish. Raw
  transcripts stay in guest-runner memory and are never printed or persisted.
- Independent file reads must show exactly the expected source change,
  unchanged module/test/config fixtures, and the nonce marker created by the
  actual bash test command. A fresh Go test runs on checked copies in the
  outer disposable VM, separately from the agent's test.
- A fresh 15-minute virtual key is limited to the synthetic alias with a
  budget and rate limit. Anonymous/invalid-key requests and an out-of-scope
  model request must be denied; the scoped model listing must be exact.
  Credential checks include private workload-key metadata and known provider
  credential locations. The immutable guest-root helper receives the **actual
  gateway master key and synthetic provider token over stdin only**. It
  compares those bytes against every observed process's environment and argv,
  the private key and exact `gateway.json`, mountinfo (including bind roots),
  and regular files in the credential directory and known state/configuration
  roots: `/var/lib/aginctus/state/{system,user}`, `/var/lib/herdr`,
  `/home/agent/.config`, and `/home/agent/.local/share/opencode`. Known gateway
  secret mounts are rejected. The scoped workload key is explicitly permitted.
  No secret bytes, matching content, raw paths or process observations are
  printed. Unreadable live process state, symlinked scan entries, unknown
  non-regular files (except Herdr sockets), oversized observations and scan
  limits fail closed. Limits are 4096 processes/scan entries, 1 MiB per process
  field or mount inventory, 64 MiB per regular file and 256 MiB total file reads.
  This is a bounded point-in-time observation by trusted guest root, not
  exhaustive secret discovery or an attestation against a compromised guest.
- Gateway-side PostgreSQL spend records must contain four distinct successful
  native Responses request IDs, the fresh key's hash, the exact alias, and
  the **same native session ID independently read from OpenCode**. The
  runner's issuance associates that fresh key with this run; no prompt-only
  run-ID attribution or fabricated native session is accepted. Missing schema
  fields, delayed/unavailable logging or extra agent model requests fail.
  Spend success labels alone are insufficient: the native transcript,
  upstream ordered continuations and independently tested files are separate
  required observations. This is synthetic correlation, not cryptographic
  provenance or evidence of a billable provider response.
- The workload must connect to the private gateway and independently report
  `ENETUNREACH` for direct public IPv4. A separate LiteLLM external TCP attempt
  must fail and increment the guest outer-interface forwarding-deny counter.
  This is a narrow no-route/packet-control observation, not fleet-wide
  credential-egress or arbitrary firewall-bypass acceptance.
- Stop and owned close run after partial failures. Herdr inventory must confirm
  workspace absence. The fixture is removed and its absence independently
  read back. Key deletion must be followed by actual rejection and removal of
  the matching revoked `workload-key`. Expected key bytes travel to the
  guest-root removal helper only over stdin. The helper requires a non-symlink,
  agent-owned mode-0600 regular file with one link in the private directory;
  it compares exact bytes and rechecks the inode/content after capture into a
  root-private sibling directory before unlinking. A rotated or unrelated key
  is never deleted: a mismatching capture is restored without overwriting a
  replacement, or retained for root recovery if another rotation blocks
  restoration. Such uncertainty fails. A separate guest-root invocation
  independently requires key-path absence, including no dangling symlink or
  rotated replacement. Gateway configuration is retained. Model deletion
  by alias absence, and the synthetic listener must stop. Failed teardown is
  a failure, even after a passing task. An uncertain remote create still
  requires destroying the entire disposable VM.

Hostless validation, safe in the current multipurpose checkout:

```sh
nix --quiet develop --no-update-lock-file -c python3 -m unittest scripts.vm_harness.test_full_stack scripts.vm_harness.test_guest_credentials scripts.vm_harness.test_tool_sequence scripts.vm_harness.test_synthetic_responses scripts.vm_harness.test_runner -v
nix --quiet develop --no-update-lock-file -c go test ./internal/testenv ./tests/oci-stage
nix --quiet eval --no-update-lock-file .#vm-harness-full-stack.drvPath
nix --quiet develop --no-update-lock-file -c just check report-json
```

Next operator gate, **only on a separately authorized dedicated KVM worker**:
apply worker CPU/storage quotas, confirm no pilot credentials/routes/Incus
privilege, then run the fixed command above. Preserve the private runner
result with revision and lock hash, reviewed staging receipts, phase outcomes,
and packet-counter observations. Inspect failures without sharing raw driver
transcripts. The runner has a 15-minute total runtime deadline, including OCI
staging; insufficient provisioning time is a failed gate, not a retry loop.
Afterward independently verify complete QEMU VM/disk deletion on success and
failure. Cache-miss/offline-pull failure acceptance and outer-worker disposal
remain operator gates; staged alias presence alone does not prove either.

### Persisted per-gate observations (#461)

The guest observer writes `/run/aginctus-full-stack/result.json` in its outer
`finally` block, including on task or teardown failure. The immutable NixOS
driver calls a fixed immutable reader and revalidates the returned JSON before
writing `full-stack.json` in its private output directory. It adds separate
storage-preservation and packet-counter outcomes, including a storage readback
after a failed task. The runner reads only that fixed artifact before removing
its temporary driver directory and embeds the allowlisted `full_stack` object
in `.vm-harness-results/<24-hex-run-id>/result.json`.

The optional extension preserves schema-v1 reports and their existing generic
exit status. It contains the nine named `phases` and fixed `gates`, each with
`pass`, `fail` or `not_verified`. A phase/gate is marked failed before its check
and passed only when the observer completes that check. Unreached checks stay
unverified. Tool transcript, exact fixture diff, fresh independent test,
credential scope, gateway correlation, workload network, injected-failure
cleanup control, fixture removal, foreign inventory preservation, revocation,
key absence, model removal and listener shutdown are separate observations.
`phases.close` includes independent owned-workspace absence readback. The
injected failure control has its own observer, so its expected fault cannot
overwrite the main run's phases or claim the main task ran. Partial cleanup
successes survive a later failure. Storage preservation does not mean removal.

The artifact contract rejects unknown fields/values, is limited to 4096 bytes,
and transports no transcripts, payloads, keys, hashes of keys, paths, headers or
arbitrary diagnostics. Reads reject symlinks, non-regular/multiply linked files,
wrong owners and group/world access. Directories are private, outputs are
exclusive mode-0600 files, and path operations are anchored to directory file
descriptors. The driver uses its existing output-directory convention, with
the runner explicitly selecting a fresh private temporary directory. No
guest-selected host path or executable is accepted.

Inspect a selected persisted result without launching any VM or provider:

```sh
nix --quiet develop --no-update-lock-file -c just check vm-report <run-id>
nix --quiet develop --no-update-lock-file -c just check vm-report-json <run-id>
```

Passing new runs require both a zero driver exit and a complete valid artifact.
Missing/invalid evidence makes a zero-exit run fail; on failed/timed-out runs,
any completed valid artifact is retained. Failure before publication, VM loss,
SIGKILL or timeout before driver export can leave no extension. Absence is not
proof, and stdout phase labels are never imported as evidence. Old v1 reports
without the extension remain readable, including `full-stack` reports.

The explicit source is `guest_observation_not_attestation`: the guest and the
unprivileged driver account remain trusted. This is not cryptographic
attestation, installed-worker acceptance or independent host-disposal proof.
Both `upstream_inference` and whole-environment `cleanup` remain `not_verified`,
even when owned workspace/key/listener checks pass. The guest cannot attest
its own outer disk deletion. No real provider/OAuth acceptance, autonomous
reasoning quality, pilot deployment or live release sign-off is claimed. MCP
remains read-only and unsupported testenv capabilities remain unsupported.

The staging helper's `c.WarnError(c.Done, ...)` reports connection-finalization
errors as warnings rather than promoting them into its exit status. Neither
its receipt nor the full-stack runner's per-gate observations establish staging
connection cleanup, storage cleanup or outer VM/disk removal. Those remain
separate operator observations.

## Test-only immutable OCI staging interface (#532)

`nix/vm-harness/oci-staging.nix` is an explicit, additive guest module for
dedicated-worker acceptance. Import it alongside `aginctus-host.nix` using
`(import ./oci-staging.nix { inherit self system; })`. The opt-in `full-stack`
scenario imports it and performs the provisioning/check calls below. The
existing topology and synthetic transport scenarios retain their separate
networked tag-based behavior.

The sole input manifest, `tests/oci-stage/pins.json`, records anonymous registry
verification on 2026-10-30 for the declared LiteLLM and PostgreSQL source tags.
It includes immutable index, per-platform manifest, config, compressed-layer
size and expected Incus fingerprint for Linux amd64 and arm64. Source tags are
provenance only; neither verification nor staging resolves mutable tags.
The module forces gateway settings to the native platform's **leaf digest**,
so Compose and staging use exactly the same reference. arm64 pins are verified
registry metadata, not evidence of arm64 VM boot support.

Hostless validation (registry verification requires public HTTPS, but downloads
only bounded metadata, no layers, and reads no registry credentials):

```sh
nix --quiet develop --no-update-lock-file -c go test ./tests/oci-stage ./internal/orchestration/drivers/compose
nix --quiet develop --no-update-lock-file -c go run ./tests/oci-stage plan
nix --quiet develop --no-update-lock-file -c go run ./tests/oci-stage verify
nix --quiet develop --no-update-lock-file -c just check report-json
```

`verify` hashes the actual bytes returned by the registries, checks each leaf's
membership in the pinned index, verifies config OS/architecture and layer sizes,
and reproduces the pinned Incus Compose layer-digest fingerprint algorithm.
No layer contents or signatures are authenticated by this metadata-only check;
Incus must verify actual content during import. Registry deletion/rate limiting
remains a provisioning failure, never permission to substitute a newer tag.

Inside the **disposable guest only**, after module import and image build,
with a fresh initialized Incus daemon, no external remotes/auth, and no live
provider credentials:

```sh
incus admin init --auto --storage-backend dir
incus config set core.https_address 127.0.0.1:8443
test "$(incus config get core.https_address)" = 127.0.0.1:8443
ss -lntp
systemctl start aginctus-test-oci-stage.service
oci-stage check
# Integration boundary: enforce worker/guest external-egress denial here,
# retain independently measured packet counters, then deploy and infer.
XDG_STATE_HOME=/run/aginctus-vm-gateway-state aginctus --configuration-file /etc/aginctus/config.json gateway up
oci-stage check
```

Do not run these guest commands on the operator host or pilot. In a NixOS
driver, use `machine.succeed(...)` after `start_all()` and waiting for
`incus.service`. The helper's marker is an accidental-use guard, not isolation
from root. Its Unix connection uses the fixed guest Incus socket and ignores
socket/remote environment overrides. The staging client replaces loaded Incus
configuration with anonymous defaults before resolving sources. The service
uses private empty Incus config directories. No provider model is registered
and no provider credentials are supplied by this module.

Staging refuses non-loopback HTTPS and foreign gateway projects, creates only
the marked gateway project when missing, then uses the **locked Compose image
resource implementation** to import directly into that project's isolated
image store. The current driver does not configure a shared image cache;
seeding a separate cache project would not satisfy its project-local lookup.
Aliases use the library's exact digest-reference normalization. A read-only
`check` uses only guest Incus GETs, refuses missing/wrong fingerprint or
architecture and incomplete OCI properties, and cannot repair or pull images.
The `oci.volumes` metadata marker is required because its absence permits the
library's registry metadata fallback. A poisoned alias is rejected, not
automatically replaced. Unit tests exercise negative integrity checks and the
pinned Compose implementation consuming staged aliases with pull-never.

Budget: two sequential native-platform imports, about 508 MB compressed on
amd64 or 513 MB on arm64 (decimal); each image's metadata-verified layer total
must be at most 512 MiB. Metadata reads are capped at 4 MiB per document. The
helper has a ten-minute context deadline; the service adds an eleven-minute
timeout, 1 GiB helper memory and one CPU equivalent. **These are not incusd
download/retry/unpack quotas**: daemon-side operations can outlive client
cancellation. Allocate at most two vCPUs, 8 GiB RAM and a 32 GiB disposable
guest disk at the worker boundary, plus a worker storage quota, before running.
Compressed sizes do not bound extracted images or Nix store usage. On success
or failure destroy the complete disposable VM/disk; never prune pilot stores.
Do not retry failed staging indefinitely. The service has no automatic boot
activation or restart policy.

Successful `verify`, `stage` and `check` emit a small JSON receipt with the
manifest SHA-256, action and architecture, without registry tokens or provider
payloads. Preserve the reviewed receipt with the source revision, lock hash,
worker bounds, guest listener inspection, provisioning outcome and separate
packet measurements. Error diagnostics and VM transcripts require review
before sharing. `offlineInference` and `cleanup` remain `not_verified` even
after a successful check: a present image is not proof that no remote pull was
attempted, and a deadline is not proof of deletion.

Remaining dedicated-worker acceptance: actually boot x86_64 KVM, stage the
images, verify the only daemon TCP listener is loopback, deny and measure
external registry traffic **after provisioning**, run the integrated synthetic
inference stage with no provider secrets, demonstrate cache-miss failure under
that denial, and independently verify VM/disk deletion. Do not label offline CI
or packet-level isolation verified without those measurements. Issues #532,
[#524](https://github.com/blevinn/aginctus/issues/524) and
[#431](https://github.com/blevinn/aginctus/issues/431) remain open.

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
