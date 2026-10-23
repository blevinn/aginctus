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
