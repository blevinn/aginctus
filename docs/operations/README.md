# Operations: choose a trusted operator task

Start with [getting started](../getting-started.md) before provisioning. Every mutating step needs separate authorization for the **selected local Incus project, resource and host**; these pages are not blanket approval.

- [Host operations](host/README.md) — NixOS/Home Manager boundaries, Incus preflight, firewall policy and state audits.
- [Gateway workflow](gateway/README.md) — read-only baseline, separately approved egress maintenance, model registration, scoped keys, inference and recovery.
- [Workload operations](workloads/README.md) — Herdr registration, SSH identity and workspace/resource lifecycle.

Use [reference](../reference/README.md) for CLI semantics and config sources; [testing](../testing/README.md) for what a validation PASS does and does not establish.
