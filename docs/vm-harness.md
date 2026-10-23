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

## Commands

From the repository root on x86_64-linux with KVM, using the dedicated worker:

```sh
nix develop -c just test-vm-runner
nix develop -c just test-vm-smoke
nix develop -c just vm-debug
```

Within the interactive driver, use `start_all()`, then commands such as
`machine.wait_for_unit("incus.service")` and
`machine.succeed("incus info")`. The driver session and VM are disposed when
the driver exits. Use only synthetic data in guest fixtures.

The initial smoke fixture uses an Incus `dir` pool; a later test slice will
exercise isolated Btrfs. Subsequent slices add synthetic gateway inference,
Herdr/OpenCode workflow assertions, fault injection, and redacted artifacts.

Never record a synthetic provider test as evidence for [#124](https://github.com/blevinn/aginctus/issues/124).
