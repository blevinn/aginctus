# Disposable NixOS VM harness (v0.2, experimental)

Parent design [#425](https://github.com/blevinn/aginctus/issues/425). This is
**not** v0.1 pilot acceptance and does not verify real provider/OAuth traffic.

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

## Trusted supervisor implementation (in progress)

Issue [#459](https://github.com/blevinn/aginctus/issues/459) is being implemented
in isolated increments. The initial Go backend has:

- QEMU guest-agent `guest-exec` / `guest-exec-status` over a local Unix
  socket, with **argv-array execution inside the guest**, no host shell;
- explicit guest command runtime and output limits, plus redacted protocol
  failures (guest error descriptions are not printed);
- 96-bit random run IDs and private `0700` ownership directories with
  `0600` manifests recording worker UID, QEMU PID, Linux process start time,
  and a maximum two-hour lifetime;
- strict rejection of unknown/stale IDs, symlinks, unexpected socket types,
  foreign resource ownership, and oversized Unix socket paths.

**Not yet wired to an agent-facing command:** `aginctus testenv vm
up|status|exec|reset|destroy` and MCP mutation tools remain unavailable.
The QGA transport is unit-tested against a **synthetic Unix socket**, not
the real pinned QEMU/NixOS VM. Registering a run with a PID is an
internal prerequisite, **not** proof that the process is the trusted VM.

Critical remaining work: build a pinned **offline/fully disconnected VM**
artifact without host store mounts, install the trusted launcher in an
agent-unmodifiable location, attach QGA only to the run-owned socket,
perform process identity/PIDFD-safe termination, bound disk/CPU/RAM and TTL
independently, and verify crash recovery plus cleanup before exposing
mutating tools. The worker must run under a separate unprivileged Unix
identity (not the pilot operator's account).

## Abandoned-session recovery scan (work in progress)

`RecoverAbandoned` now implements one bounded, **trusted-worker-only**
recovery pass over the run store and its delegated cgroup subtree.
It scans at most 64 syntactically valid run IDs, rejects malformed
or symlinked ownership records, and skips sessions whose matching
PID/starttime is still valid and whose TTL has not expired.

For an expired or abandoned session, it **never signals an arbitrary
stored PID**. Instead it opens the run-owned cgroup directory,
executes `cgroup.kill`, verifies `cgroup.events` reports
`populated 0`, and attempts to remove the cgroup. Success and
failure are written to an atomically replaced private
`recovery.json`. A failure or missing cgroup cannot be declared
clean. Existing `session.json` plus absent cgroup is treated as
a previously finished session. Foreign paths and untrusted
ownership records are never grounds for deletion.

**This is not yet an independently running watchdog.** There is
currently no systemd service or timer invoking the recovery pass,
no deployment configuration that authenticates a worker service,
and no positive KVM test verifying cleanup after a real supervisor
crash. The next #461 packaging slice must schedule this scan
independently at a bounded interval and on service startup, while
ensuring the agent cannot edit supervisor executables, policy,
state directories or delegated cgroup controls. The recovery
scan does not remove qcow2 images, guest Nix-store images, or
other run artifacts; disk retention and independent cleanup
attestation remain future work.

## Delegated cgroup v2 resource guard (next supervisor increment)

The foreground supervisor can now require a **dedicated delegated
cgroup v2 subtree** owned by the test worker. This is a host
installation prerequisite, **not** an MCP/client-controlled argument:

- `cpu`, `memory`, and `pids` must appear in both
  `cgroup.controllers` and `cgroup.subtree_control`. A missing
  controller, inaccessible subtree, non-worker-owned root or symlinked
  root aborts the launch. No automatic `sudo`, host systemd mutation,
  or silent fallback to unbounded QEMU is permitted.
- Before child creation the supervisor writes `cpu.max = 200000 100000`
  (two CPUs), `memory.max = 5368709120` (5 GiB including host QEMU
  overhead), `memory.swap.max = 0`, `memory.oom.group = 1`,
  and `pids.max = 256` into the new run-specific cgroup.
- Linux `clone3(CLONE_INTO_CGROUP)` is selected by
  `syscall.SysProcAttr.UseCgroupFD`, so there is no period between
  creating the QEMU process and moving it into its quota.
- On completion, the supervisor calls `cgroup.kill`, requires
  `populated 0` from `cgroup.events`, and removes that cgroup.
  A failed kill, unreadable events, nonempty cgroup or failed removal
  surfaces an error; the session receipt records cgroup status
  independently from `cleanup: not_verified`.

The exported `RunPinnedVMSession` now **requires** a validated
`CgroupManager`. The legacy non-cgroup helper remains private and
is used for hostless fake-process unit tests only. There is **no**
agent-facing mutation enabled by this change.

Example configuration for a future dedicated systemd user-worker
installation: place the worker service in a cgroup with
`Delegate=cpu memory pids`, grant that worker a distinct delegated
subtree, and pass its immutable absolute path to
`OpenCgroupManager`. Exact unit files and the full isolated worker
packaging are tracked in #461. Ordinary interactive user sessions
usually **do not** have suitable delegation; failure is expected.
The worker must not have the pilot user's Incus socket or credentials.

**Outstanding:** host disk quotas, unattended crash/reboot recovery,
an independently supervised TTL watchdog, verified network isolation
under KVM, and guest-agent boot readiness. Cgroup closure alone
is not a complete host isolation proof. Default `just check-all`
does not launch QEMU or require cgroup delegation.

## Foreground VM supervisor increment (#459)

The session library now starts a **trusted Nix-store VM launcher** using
a fixed argv array, an independent private per-run directory, and a
deliberately minimal environment. Host `QEMU_OPTS`, `QEMU_NET_OPTS`,
`NIX_PATH`, `SHARED_DIR`, `SSH_AUTH_SOCK`, `INCUS_*` and provider
secrets are **not inherited**. The generated launcher is given a
run-owned `NIX_DISK_IMAGE`, `TMPDIR` and a fixed private QEMU
virtio-serial guest-agent socket path.

`RunPinnedVMSession` explicitly refuses root, validates a Nix-store
executable and access to `/dev/kvm`. It reserves a cryptographic run
ID **before starting** the guest, binds the QEMU process identity
after launch, keeps the Go process handle, and waits until exit,
cancellation or a maximum two-hour TTL. Cancellation/expiry sends
TERM to the **held child process handle**, escalates to KILL after
two seconds, and waits for the direct child to be reaped. Only the
session's private `run.json` and `session.json` receipts are
written; foreign resources are never enumerated or deleted.

**Important limitations:**

- This is an **internal foreground/session API**, not yet
  `aginctus testenv vm up|exec|destroy` or MCP execution. No caller
  can pass a custom launcher, socket, QEMU arguments, or state root
  through those public interfaces because they do not exist yet.
- The TTL is enforced by the **living supervisor process**. An
  independent watchdog and crash/reboot recovery are still required.
  No claim is made that children survive or terminate safely if the
  supervisor itself crashes.
- QEMU vCPU/memory and virtual disk size are configured by the pinned
  NixOS fixture, but **cgroup CPU/RAM limits, actual host disk quotas,
  process-tree teardown, retention GC and PIDFD-attested destruction
  remain unimplemented**.
- `process_reaped: true` means only that the direct process was
  waited on. Both `cleanup` and `upstream_inference` remain
  `not_verified`, and guest readiness is **not** inferred from a
  started process.
- Unit tests run a fake program instead of a real QEMU guest. The
  pinned NixOS launcher, QGA connection, egress boundary and full
  lifecycle still require disposable KVM validation.

This API must be packaged in an agent-unmodifiable supervisor and
run as an independent dedicated **unprivileged** account, with no
pilot-host privileges or secrets. The eventual daemon/IPC API must
hold process handles and validate run ownership before permitting
guest exec/status/reset/destroy. Do not give arbitrary agents a
direct NixOS launcher, Python console or supervisor state-directory
write access.

## Standalone isolated VM image (supervisor prerequisite)

A new explicitly opt-in package builds the persistent VM **launcher
derivation** from the same pinned `guest.nix` configuration as the batch
tests:

```sh
nix build -L --no-link .#testenv-isolated-vm
nix build --no-link --print-out-paths .#testenv-isolated-vm
```

Unlike the batch test driver, this VM variant declares a guest-local Nix
store disk image (`useNixStoreImage=true`), **no host Nix store mount**,
no shared directories/virtiofs, no forwarded ports, and QEMU
`-nic none` (no SLIRP/host management route), while allowing a
guest-side QEMU agent. The supervisor will attach a private
`org.qemu.guest_agent.0` virtio-serial channel under each run-owned
socket; neither an arbitrary agent-supplied socket nor a TCP listener
is acceptable.

**Not a complete sandbox yet:** the generated VM script accepts
QEMU runtime arguments and honors some environment variables. It must
be invoked by an immutable, supervisor-controlled launcher with a
sanitized environment, fixed QEMU options, explicit run-owned disk
location, bounded TTL/CPU/RAM/storage and safe PIDFD-backed cleanup.
Do not hand the standalone launcher to an untrusted agent. An operator
may inspect/build its immutable Nix store closure without booting it;
the guest image has not yet been validated under KVM in this PR.

## Commands

From the repository root on x86_64-linux with KVM, using the dedicated worker:

```sh
nix develop -c just test-vm-runner
nix develop -c just test-vm-smoke
nix develop -c just test-vm-storage-network
nix develop -c just test-vm-runtime-lifecycle
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
Subsequent slices add synthetic gateway inference,
Herdr/OpenCode workflow assertions, fault injection, and redacted artifacts.

## Machine-readable run evidence

Every `runner.py test <scenario>` invocation writes an independent,
per-run `.vm-harness-results/<random-id>/result.json` with the scenario,
pass/fail from the Nix derivation exit status, lockfile fingerprint,
base repository revision, tracked-source dirty status and UTC timestamp.
Provenance is captured before the build; unavailable Git or lockfile metadata
is recorded as `null`. The base revision alone does not identify dirty source,
and this snapshot does not prevent concurrent source changes. Launch failures
also produce a failure report with `failure_stage: launch`; a nonzero build
exit records `failure_stage: build`. It **always** records
`upstream_inference: not_verified` and `cleanup: not_verified`; these
are not inferred from the presence of an Incus service or a green mock.

The report contains no VM transcript, raw headers, credentials, or provider
payload. It does **not** prove that cleanup completed, establish a cryptographic
attestation, or sanitize diagnostic output printed by the Nix builder.
Only run this on a separate unprivileged worker without pilot secrets.
Reports are mode `0600` under run-scoped directories and excluded from Git.

Never record a synthetic provider test as evidence for [#124](https://github.com/blevinn/aginctus/issues/124).
