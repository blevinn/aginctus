# Test environment: choose an isolated harness

- [Disposable nested-Incus VM harness](vm-harness.md) — opt-in QEMU/NixOS setup, test-only diagnostics and evidence; **not** the pilot host.
- [Scenario catalog](scenarios.md) — explicit synthetic and full-stack test recipes, including immutable OCI staging.
- [Dedicated recovery worker contract](testenv-worker.md) — required host isolation, credentials and instance policy.
- [Dedicated worker runbook](testenv-worker-operations.md) — separately authorized image/pool, lifecycle, recovery and acceptance checks.
- [Outer Incus VM image](testenv-incus-vm-image.md) — explicit operator import into a dedicated worker, never a coding agent's host authority.

The [testing tier overview](../README.md) explains what each PASS can establish. Running on the wrong host or under an agent with an Incus administrator socket crosses a security boundary; do not treat host filesystem access as sandboxed merely because the worker runs systemd restrictions.
