# Aginctus documentation

Use this guide to find the **current** operator path, implementation contract or validation tier. A proposed design or preserved pilot runbook is not evidence of a shipped or validated feature.

## Start here

- [Getting started](getting-started.md) — build, inspect prerequisites and understand NixOS/Incus side effects before any deployment.
- [Operations](operations/README.md) — explicit host, gateway and workload administration; authorization and recovery rules.
- [Testing and acceptance](testing/README.md) — choose hermetic, VM, dedicated-worker, read-only or live inference tiers; understand proof limits.
- [Reference](reference/README.md) — current CLI semantics, configuration precedence, declaration contracts, security limitations.
- [Architecture](architecture/README.md) — implemented topology and its transitional boundaries; [native reconciliation ADR](adr/0002-native-incus-reconciliation.md).
- [Development](development/README.md) — reproducible tools, checks, CI and release workflow.

## Important distinctions

| Need | Read | Do not infer |
| --- | --- | --- |
| NixOS activation, firewall and project authority | [Host operations](operations/host/README.md) | A configuration read or a dry-run authorizes activation |
| Gateway registration, egress, scoped tokens and inference | [Gateway operator stages](operations/gateway/README.md) | Health checks establish real provider access or isolation |
| Herdr/OpenCode provisioning, lifecycle and persistence | [Workload operations](operations/workloads/README.md) | Running guests prove a model-directed coding task |
| CLI/config values, security and declared grants | [Reference](reference/README.md) | A declared capability is an effective permission |
| Testing tier and acceptance evidence | [Testing](testing/README.md) | Hermetic/VM reports substitute for an operator-controlled live test |

## Decisions, proposals and historical evidence

- [Design proposals](design/native-incus-reconciliation.md), [Home Manager authority design](design/home-manager-operator.md), [testenv worker design](design/testenv-incus-backend.md), [local model design](design/local-llms.md) and [workspace-sharing design](design/workspace-sharing.md) are not automatic implementation guarantees.
- [Architecture decision records](adr/0001-herdr-client-topology.md) preserve accepted decisions; consult the [second ADR](adr/0002-native-incus-reconciliation.md) for native migration rationale.
- [v0.1 release notes](releases/v0.1.0.md) state what was observed and what was deferred. [Historical materials](history/README.md) retain release-specific handoffs and old milestone pointers rather than masquerading as current procedures.
- [Issue #645](https://github.com/blevinn/aginctus/issues/645) tracks the documentation refactor. Other release-specific work and verification state belong to their GitHub issues; a stale docs page is not a release sign-off.

## Contributor maintenance

Keep each active topic in one canonical page and link to it from the corresponding section index. Update the [docs inventory](internal/document-inventory.md) only as a **historical baseline**, not as a duplicate live index. The hermetic Markdown checker validates link targets and operator-facing discoverability; the CLI example checker checks syntax without executing commands. No documentation example is permission to change a real host, issue credentials or make a billable model request.
