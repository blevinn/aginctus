# Documentation disposition inventory (baseline 2026-11-08)

**Parent:** #645; inventory exit criteria: #646. This is a *pre-refactor classification* of tracked Markdown on the `main` commit `cc7368fed8ce`, **not** a claim that the proposed destinations already exist. Treat paths as proposals until consolidation PRs merge; keep the history in Git rather than duplicating narratives in live operator docs. Re-evaluate with `git ls-files '*.md'` for future additions.

## Canonical ownership policy

| Topic | Current authoritative starting point | Intended home |
| --- | --- | --- |
| Installation and host approval | `docs/getting-started.md` | Landing and host operations |
| Runtime topology and native migration | `docs/architecture.md` / ADR 0002 | Architecture (present), design/ADR (planned) |
| Gateway/provider/egress operator workflow | `docs/gateway-operator-path.md` | Operations / gateway |
| Herdr registration and identity | `docs/herdr-machine-registration.md` | Operations / workloads; identity contract in reference |
| OpenCode staged acceptance | `docs/acceptance-opencode.md` | Testing / acceptance |
| VM/testenv stages and ownership | `docs/vm-harness.md` and `docs/testenv-worker.md` | Testing / testenv |
| Command semantics and configuration precedence | `docs/cli.md`, `docs/configuration.md` | Reference; Cobra help governs syntax |
| Release-specific evidence | `docs/releases/v0.1.0.md`, `docs/v0.1-deployment-acceptance.md` | Releases / historical or testing, not current guarantees |
| Future proposals | `docs/design/` and ADRs | Design / ADR; explicitly not implemented |
| Contributor and vulnerability policies | Existing issues #627 / #626 | Root contributor/policy docs once approved |

## Per-file disposition

| Current source | Category | Action | Proposed canonical destination | Retention rationale |
| --- | --- | --- | --- | --- |
| `AGENTS.md` | Entry / maintainer | Keep | `AGENTS.md` | Root project contract |
| `CHANGELOG.md` | Entry / maintainer | Keep | `CHANGELOG.md` | Root project contract |
| `docs/acceptance-opencode-herdr.md` | Acceptance/testing | Merge/move | `docs/testing/acceptance/acceptance-opencode-herdr.md` | One tier entry; release proof remains separately scoped |
| `docs/acceptance-opencode-inference.md` | Acceptance/testing | Merge/move | `docs/testing/acceptance/acceptance-opencode-inference.md` | One tier entry; release proof remains separately scoped |
| `docs/acceptance-opencode-live.md` | Acceptance/testing | Merge/move | `docs/testing/acceptance/acceptance-opencode-live.md` | One tier entry; release proof remains separately scoped |
| `docs/acceptance-opencode-preflight.md` | Acceptance/testing | Merge/move | `docs/testing/acceptance/acceptance-opencode-preflight.md` | One tier entry; release proof remains separately scoped |
| `docs/acceptance-opencode.md` | Acceptance/testing | Merge/move | `docs/testing/acceptance/acceptance-opencode.md` | One tier entry; release proof remains separately scoped |
| `docs/adr/0001-herdr-client-topology.md` | Decision record | Keep | `docs/adr/0001-herdr-client-topology.md` | Accepted decisions, not procedures |
| `docs/adr/0002-native-incus-reconciliation.md` | Decision record | Keep | `docs/adr/0002-native-incus-reconciliation.md` | Accepted decisions, not procedures |
| `docs/ai-gateway.md` | Architecture/design | Split | `docs/architecture/gateway.md` | Current contract vs future design |
| `docs/architecture.md` | Architecture | Move/rewrite | `docs/architecture/README.md` | Current topology; reconcile with code |
| `docs/architecture/litellm-private-volume.md` | Architecture | Keep / audit status | `docs/architecture/litellm-private-volume.md` | Architecture detail |
| `docs/architecture/native-chatgpt-oauth.md` | Architecture | Keep / audit status | `docs/architecture/native-chatgpt-oauth.md` | Architecture detail |
| `docs/cli.md` | Reference | Move / tighten | `docs/reference/cli.md` | Canonical contracts |
| `docs/configuration.md` | Reference | Move / tighten | `docs/reference/configuration.md` | Canonical contracts |
| `docs/design/home-manager-operator.md` | Proposal/design | Keep / recheck status | `docs/design/home-manager-operator.md` | Do not claim deployed behavior |
| `docs/design/local-llms.md` | Proposal/design | Keep / recheck status | `docs/design/local-llms.md` | Do not claim deployed behavior |
| `docs/design/native-incus-reconciliation.md` | Proposal/design | Keep / recheck status | `docs/design/native-incus-reconciliation.md` | Do not claim deployed behavior |
| `docs/design/native-oci-feasibility.md` | Proposal/design | Keep / recheck status | `docs/design/native-oci-feasibility.md` | Do not claim deployed behavior |
| `docs/design/test-reporting.md` | Proposal/design | Keep / recheck status | `docs/design/test-reporting.md` | Do not claim deployed behavior |
| `docs/design/testenv-incus-backend.md` | Proposal/design | Keep / recheck status | `docs/design/testenv-incus-backend.md` | Do not claim deployed behavior |
| `docs/design/workspace-sharing.md` | Proposal/design | Keep / recheck status | `docs/design/workspace-sharing.md` | Do not claim deployed behavior |
| `docs/development.md` | Development | Move / tighten | `docs/development/development.md` | Developer/release procedure |
| `docs/firewall-host-backends.md` | Host operations | Move | `docs/operations/host/firewall-host-backends.md` | Explicit host authority and side effects |
| `docs/firewall-pilot-validation.md` | Host operations | Move | `docs/operations/host/firewall-pilot-validation.md` | Explicit host authority and side effects |
| `docs/gateway-api-key-pilot.md` | Gateway operations | Merge/move | `docs/operations/gateway/gateway-api-key-pilot.md` | Canonical stages; preserve consent and recovery |
| `docs/gateway-assets.md` | Reference | Move / tighten | `docs/reference/gateway-assets.md` | Canonical contracts |
| `docs/gateway-egress-network.md` | Gateway operations | Merge/move | `docs/operations/gateway/gateway-egress-network.md` | Canonical stages; preserve consent and recovery |
| `docs/gateway-firewalld-egress.md` | Gateway operations | Merge/move | `docs/operations/gateway/gateway-firewalld-egress.md` | Canonical stages; preserve consent and recovery |
| `docs/gateway-inference-smoke.md` | Gateway operations | Merge/move | `docs/operations/gateway/gateway-inference-smoke.md` | Canonical stages; preserve consent and recovery |
| `docs/gateway-operator-path.md` | Gateway operations | Merge/move | `docs/operations/gateway/gateway-operator-path.md` | Canonical stages; preserve consent and recovery |
| `docs/gateway-workload-key-pilot.md` | Gateway operations | Merge/move | `docs/operations/gateway/gateway-workload-key-pilot.md` | Canonical stages; preserve consent and recovery |
| `docs/getting-started.md` | Entry | Rewrite/keep | `docs/getting-started.md` | Short task-oriented entry point |
| `docs/herdr-machine-registration.md` | Workload operations | Merge/move | `docs/operations/workloads/herdr-machine-registration.md` | Herdr SSH/lifecycle guidance |
| `docs/herdr-persistent-state-volumes.md` | Workload operations | Merge/move | `docs/operations/workloads/herdr-persistent-state-volumes.md` | Herdr SSH/lifecycle guidance |
| `docs/home-manager.md` | Review | Evaluate | `docs/home-manager.md` | Confirm purpose and references |
| `docs/litellm-oci-egress-pilot.md` | Gateway operations | Merge/move | `docs/operations/gateway/litellm-oci-egress-pilot.md` | Canonical stages; preserve consent and recovery |
| `docs/management-dhcp-gateway.md` | Gateway operations | Merge/move | `docs/operations/gateway/management-dhcp-gateway.md` | Canonical stages; preserve consent and recovery |
| `docs/mcp-configuration.md` | Reference | Move | `docs/reference/mcp.md` | Declarations vs unimplemented grants |
| `docs/milestones/0001-herdr-console.md` | Historical | Retain pointer | `docs/milestones/0001-herdr-console.md` | GitHub issues authoritative |
| `docs/milestones/0002-v0.2.md` | Historical | Retain pointer | `docs/milestones/0002-v0.2.md` | GitHub issues authoritative |
| `docs/opencode-runtime-validation.md` | Acceptance/testing | Merge/move | `docs/testing/acceptance/opencode-runtime-validation.md` | One tier entry; release proof remains separately scoped |
| `docs/operator-preflight.md` | Host operations | Move | `docs/operations/host/operator-preflight.md` | Explicit host authority and side effects |
| `docs/operator-state-audit.md` | Host operations | Move | `docs/operations/host/operator-state-audit.md` | Explicit host authority and side effects |
| `docs/operator-units-audit.md` | Host operations | Move | `docs/operations/host/operator-units-audit.md` | Explicit host authority and side effects |
| `docs/orchestration-streams.md` | Architecture / reference | Move / split | `docs/architecture/orchestration-streams.md` | Current native/legacy boundary and plan contract |
| `docs/orchestration.md` | Architecture / reference | Move / split | `docs/architecture/orchestration.md` | Current native/legacy boundary and plan contract |
| `docs/project-overview.md` | Architecture | Merge | `docs/architecture/README.md` | Merge overview with implemented topology |
| `docs/README.md` | Entry | Rewrite/keep | `docs/README.md` | Short task-oriented entry point |
| `docs/releases/TEMPLATE.md` | Release evidence | Keep | `docs/releases/TEMPLATE.md` | Immutable release-specific history |
| `docs/releases/v0.1.0.md` | Release evidence | Keep | `docs/releases/v0.1.0.md` | Immutable release-specific history |
| `docs/releasing.md` | Development | Move / tighten | `docs/development/releasing.md` | Developer/release procedure |
| `docs/security.md` | Reference | Move / tighten | `docs/reference/security.md` | Canonical contracts |
| `docs/selective-go-forwarding.md` | Gateway operations | Merge/move | `docs/operations/gateway/selective-go-forwarding.md` | Canonical stages; preserve consent and recovery |
| `docs/ssh-credentials.md` | Reference | Move / tighten | `docs/reference/ssh-credentials.md` | Canonical contracts |
| `docs/testenv-incus-vm-image.md` | Testenv | Split/move | `docs/testing/testenv/testenv-incus-vm-image.md` | Harness, security, runner and evidence separated |
| `docs/testenv-worker.md` | Testenv | Split/move | `docs/testing/testenv/testenv-worker.md` | Harness, security, runner and evidence separated |
| `docs/v0.1-deployment-acceptance.md` | Acceptance/testing | Merge/move | `docs/testing/acceptance/v0.1-deployment-acceptance.md` | One tier entry; release proof remains separately scoped |
| `docs/vm-harness.md` | Testenv | Split/move | `docs/testing/testenv/vm-harness.md` | Harness, security, runner and evidence separated |
| `docs/workloads/lifecycle.md` | Component reference | Keep / link | `docs/workloads/lifecycle.md` | Current workload contracts |
| `docs/workloads/opencode.md` | Component reference | Keep / link | `docs/workloads/opencode.md` | Current workload contracts |
| `README.md` | Entry / maintainer | Keep | `README.md` | Root project contract |

## Migration and validation rules

- Use a **single canonical procedure** per side-effecting task. Never delete independent approval, credential safety, host trust, recovery, or rollback constraints just to shorten text.
- Distinguish proposed APIs, available CLI code, release proof and live host/provider validation. Preserve the v0.1 fresh-host reproducibility deferral (#580) and the separate release acceptance gate.
- Retain ADRs, distinct design rationale and release evidence. Where a document is superseded, use a short pointer or history page rather than leaving conflicting active instructions.
- Keep additions in scoped PRs, using isolated worktrees when working locally; no edits outside `blevinn/aginctus`. Preserve relative links and the existing hermetic validators.
- After relocation, update this **baseline** with a separately tracked current layout and retire it from active navigation when it is no longer needed.
