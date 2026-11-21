# CLI root mutable injection seam audit

Tracks #903, #755, #902 and #1021. Initial audit on `main` (2026-11-21); updated disposition after #1013/#1014/#1019 merged.
These are top-level **mutable function/transport** variables in production
CLI composition. They exist primarily to substitute test behaviors from the
same package; they are not inherently Cobra global registrations, which the
existing architecture guard already forbids.

This inventory intentionally **does not** claim the seams have been removed.
Converting them blindly would break root-level privilege/behavior tests and
could widen the scope of trusted host operations. Each owner and replacement
path is explicit so #903/#755 can use a defensible closure decision.

| Root file | Mutable injection seams | Owning capability | Planned disposition |
| --- | --- | --- | --- |
| `commands_aggregate.go` | `removeAggregateGateway, executeAggregatePlan` | orchestration | Replace with per-Run aggregate handlers after native plan contracts settle |
| `commands_gateway.go` | `gatewayEgressQuery` | gateway/control-plane | Inject verifier reader and read-only query through gateway handler construction |
| `commands_network.go` | `getFirewallNetwork, firewallEffectiveUID, firewallCommand` | host/net/firewall | Inject a host firewall capability bundle at root assembly; retain privilege proof |
| `gateway_siwc.go` | `exchangeGatewaySIWC, prepareGatewaySIWC` | plugin provider session | Inject session capabilities; no implicit credential authority |
| `gateway_status.go` | `inspectGatewayProjects, inspectGatewayInstances` | gateway/Incus observer | Pass observer via gateway status handler |
| `herdr_wiring.go` | `herdrAuthorizationGuestExec, launchHerdrConsole, transitionHerdrClient, herdrHostTrustGuestExec, herdrMachineGuestExec` | plugins/herdr | Provide concrete pinned-host and guest capabilities via request-scoped wiring |
| `images_mutation_wiring.go` | `importScopedImage, acquireScopedImageMutationLease, importOwnedScopedImage` | incus/images | Per-invocation CreateActions / OwnedCreateActions already exist; remove mutable root forwarding seams after root tests migrate |
| `instance_status_wiring.go` | `inspectIncusInstances, inspectScopedInstances` | incus observer | Explicit project-scoped reader per invocation |
| `images_wiring.go` | `runIncusImage, inspectScopedImages` | incus/images | Inject command and inventory reader per request; preserve ambient v0.1 compatibility separately |
| `network_status.go` | `inspectIncusNetworks` | incus observer | Inject read-only inventory into network status handler |
| `operator_wiring.go` | `inspectOperatorIncus, inspectLocalProjects, createLocalProject, inspectOperatorUnit` | host/operator and incus | Replace global observers with explicit independently authorized capabilities |
| `run.go` | `executeManagementNetwork, executeHerdrClient, executeWorkload, setIncusHTTPSAddress, ensureCredentialStorage, attachGatewayCredentials, auditGatewayCredentials` | root composition | Provide a per-Run dependency bundle; keep public command tree distinct from testenv worker |
| `workload_gateway_provision.go` | `provisionGatewayGuest` | gateway/plugin | Move guest executor injection into gateway provisioning handler |
| `workload_lifecycle.go` | `runWorkloadIncus, transitionWorkload` | orchestration/native | Pass local-project and lifecycle capabilities per operation |
| `testenv.go` | `executeTestenvVM, serveTestenvMCP` | testenv | Inject independent public testenv handlers; retain worker isolation |
| `workload_scoped_wiring.go` | `planScopedExistingWorkloadDiff, ensureScopedExistingWorkload, transitionScopedWorkloadState` | orchestration/native | Use typed request-scoped orchestration capabilities |

## Reconciled migrations and active follow-up

The following callbacks are **already eliminated** from production root
wiring by merged PRs: `gatewayResponsesInput` (#1013),
`discoverFirewallPolicy` (#1014) and network-specific
`lockEgress` / `inspectEgressLink` / `effectiveEgressZone` (#1019).
Do not retain those stale entries simply to inflate the migration backlog.
The `gatewayEgressQuery` override remained deliberately after #1019,
because a full-root preview/help integration test still injected it.

The new independent follow-up branches proposed for #1021 are #1030
(gateway read-only query injection) and #1031 (operator project-plan and
project-ensure test capability isolation). Until those PRs merge, the
corresponding documented callbacks are **still present** on main and must
not be represented as removed. #1032 is the acceptance checklist, not
host validation.

The table records remaining mutable callbacks, but it does not endorse
their permanence or establish that they were individually approved as
safe exceptions. Native/plugin ownership is tracked separately under
#803/#804/#805/#806. The operator must review each security-sensitive
exception before closing #755/#725.

## Priority and closure policy

1. **Security-first:** operator project and host firewall/egress seams are
   highest priority; injection must not bypass independent proof of effective
   root authority, exact Incus project or firewall backend.
2. **Native/orchestration overlap:** workload, aggregate and gateway topology
   seams must migrate with their existing native/plugin owner, not an
   alternative CLI controller. Preserve v0.1 ambient Incus alias semantics.
3. **Root entrypoint:** `run.go` and testenv transport seams should become
   per-invocation dependencies when the new client-backed CLI wiring
   stabilizes; no package-level mutable Cobra tree is allowed.
4. **Low risk:** read-only observers may be migrated incrementally provided
   syntax/help still trigger no host or Incus activity.

Per-seam closure requires a test showing (a) an explicit capability was
injected into a fresh invocation, (b) invalid syntax and help do not call it,
(c) authorization failures cannot use a test-only alternate transport, and
(d) root integration behavior and output remain compatible.

**Wave 1 outcome:** every root test file has a tracked owner and these
mutable seams have a named migration owner. **Remaining work:** safe
replacement of the seams above, a per-function test ownership audit, and
real NixOS/Incus acceptance. This is not final architectural signoff; #903
and #755 should not be closed solely on the Wave 1 ledger.
