# Testing and acceptance: choose an evidence tier

This page is the **canonical testing entry point** for Aginctus. Different test tiers answer different questions and confer different authority. Nothing in this index itself authorizes Incus writes, host firewall changes, credential use or billable provider calls. Select a tier before opening its detailed runbook.

| Tier | Start here | Side effects | A PASS establishes | Does **not** establish |
| --- | --- | --- | --- | --- |
| Hermetic repository validation | [Development checks](../development.md#validation-tiers) and [reporting contract](../design/test-reporting.md) | Local build/test fixtures; no live Incus or provider | Reproducible Go/Python/shell/Nix/doc contracts in CI | Host deployment, network denial or inference |
| Disposable nested-Incus VM | [VM harness](../vm-harness.md) | Explicit opt-in local QEMU VM, VM-local Incus | Selected guest/NixOS/Incus scenario in a disposable VM | Host pilot deployment or upstream provider traffic |
| Dedicated testenv worker | [Worker security and lifecycle](../testenv-worker.md); [outer VM image](../testenv-incus-vm-image.md) | Separately approved **dedicated** worker Incus admin, pool/project/image lifecycle | Evidence of the *selected* worker test; depends on executed gates | Safe use on a pilot host or permission for coding agents to administer Incus |
| Read-only pilot inspection | [OpenCode stage 2](../acceptance-opencode.md#2-read-only-pilot-inspection) and [operator preflight](../operator-preflight.md) | Inspects explicitly selected host/Incus state; may read limited guest metadata | Topology/configuration readiness | HTTP authentication, egress denial, model or tool execution |
| Opt-in owned Herdr lifecycle | [OpenCode stage 3](../acceptance-opencode.md#3-opt-in-real-herdr-lifecycle-not-yet-the-full-agent-task) | Mutates run-owned workspaces only with explicit approval | Selected lifecycle behavior | End-to-end coding task or inference |
| Live provider and agent acceptance | [OpenCode stage 4](../acceptance-opencode.md#4-operator-authorized-live-nine-phase-acceptance); [gateway smoke](../gateway-inference-smoke.md) | Separately approved potentially billable requests, credentials and cleanup | Only the specific observed route, key and task evidence | General platform safety, durable network isolation or fresh-host repeatability |

## Operational decisions

1. **Do you need safe code changes validated?** Run `just check ci` or `just check report` in the pinned development shell. The structured `just check report-json` variant is intended for machine exchange; never treat it as live acceptance.
2. **Do you need guest/Incus behavior isolated from the pilot?** Read the VM harness trust boundary before opting into a disposable test. A dedicated persistent worker is a separate, privileged system, not a substitute for the disposable VM safety boundary.
3. **Do you need to know whether a deployed pilot is ready?** Begin at stage 2 read-only preflight; it does not authorize stage 3 or 4.
4. **Do you need actual model/agent evidence?** Use the operator-held staged acceptance, explicitly authorize credentials and any billable request, record independent attribution and revocation evidence, and independently verify cleanup.

## Reference and historical evidence

- [OpenCode staged acceptance](../acceptance-opencode.md) owns the canonical operator decision sequence; the individual `acceptance-opencode-*.md` pages hold specialized commands.
- [v0.1 deployment acceptance procedure](../v0.1-deployment-acceptance.md) is a **historical, incompletely exercised fresh-host procedure**. The fresh-host repeat/cleanup gate was deferred to v0.2 #580; the runbook is not a pass report.
- [Test reporting design and implemented output contracts](../design/test-reporting.md) has detailed report/evidence semantics. A report only covers its own provenance and selected gates.
- The developer-facing [test architecture refactor](https://github.com/blevinn/aginctus/issues/645) may change script placement; validate commands against the checked-out version, not a stale worktree.

**Keep literal secret values out of tests, command output and Markdown.** Run host/VM/provider checks only when the associated operator or dedicated worker explicitly authorizes them.
