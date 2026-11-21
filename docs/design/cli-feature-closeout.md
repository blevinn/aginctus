# CLI architecture closure: verified boundaries and remaining gates

Status: **code migration substantially complete; final acceptance not signed off**.
Related: #725, #753, #755, #902, #903; acceptance #580.
Current code migration issues #904 (images), #905 (firewall), and #906
(gateway asset actions) closed individually after hermetic CI and scoped
ownership review. Do not interpret those issue closures as a live-host PASS.

## Function-level root test disposition

The full 70-file root-test inventory lives in
[cli-root-test-inventory.md](cli-root-test-inventory.md). Its owner decisions
remain applicable at the individual function level; the table below states
the **reviewed boundary cases** where family-only unit tests and true root
integration are easily confused.

| Test functions / files | Contract actually exercised | Disposition |
| --- | --- | --- |
| `TestGatewayResponsesVerifyFromStdin` | Full gateway Cobra flags, Responses stream summary, invalid input denial and secret redaction | Keep full root path; input injection becomes invocation-scoped in the separate Responses PR |
| `TestGatewayAssetCommandsAndSelectors`, `TestGatewayAssetSyntaxAndHelpStayOffline` | Root config/asset-store wiring, JSON shape, XDG no-create, plugin instance flow and public error status | Keep root integration; `internal/gatewaycontrol/asset_actions_test.go` independently owns store/action semantics |
| `TestGatewayRegistryLegacyMapping`, `TestGatewayRegistryCobraSyntaxBeforeConfig` | Legacy positional-argument translation, actual public exit/Usage and loader-isolation contract | Retain root; `commands/gateway/registry/commands_test.go` owns direct dispatch |
| `TestFirewalldReconcileUsesOwnedZoneAndMinimalRules`, `TestFirewalldDisownedNetworkRemovesRuntimeExceptions` | Host-backed firewall policy and negative-security protocol via CLI renderer | Retain cross-boundary test, but inject `FirewallActions` locally rather than modifying production root globals |
| `TestFirewallReconcileRejectsMixedHostBackendsBeforeMutation`, `TestUnprivilegedFirewallReconcileDoesNotExecuteCommands` | Backend ambiguity and effective-root denial before any host mutation | Keep explicit negative-security test with per-invocation actions |
| `TestNativeNftReconcileScopedToOwnedChildChain`, `TestNativeNftDeletedBridgeClearsOnlyOwnedPermissions`, `TestNativeNftMissingInputHookNeverMutates`, `TestNativeNftBackendRequiresExplicitSelection` | Native child-chain-only policy and fail-closed host cases | Keep root policy output evidence, migrate test command/UID/discovery injections to per-test action bundle |
| `TestGatewayEgressCobraRequiresExplicitConfirmationsBeforeConfig` | Public egress confirmations before config load | Retain root entrypoint, alongside direct `commands/gateway/egress` registration tests |
| `TestEgressNoUnprivilegedMutationOrRetiredStaging`, `TestDiscoverEgressIdentityAndRoutes` | Root privilege refusal and gateway/route/Incus identity evidence | Retain; shared egress capability bindings still await per-request injection |
| `TestFirewallPolicyRejectsUnownedBeforeDisplay` | CLI report refuses unowned bridge before exposing configuration | Retain negative root output contract; independent Incus network read capability remains a separately tracked mutable root seam |

The above is a focused function-level review of sensitive/representative
root tests, **not** a claim that every individual test has been reviewed.
Remaining file entries have explicit owner/disposition in the inventory.
Do not delete actual root integration evidence solely to hit a file-count
target. When moving a function, preserve its security denial and command
status/output contract alongside domain tests.

## Remaining root mutable transport exceptions

The canonical 17-group owner matrix is
[cli-root-mutable-seams.md](cli-root-mutable-seams.md). The new architecture
test rejects *new undocumented package-scope function-literal injection
hooks*; it does not silently treat existing mutable variables as safe.

Highest priority for later per-invocation conversion:
1. **Host/Incus authority:** `commands_network.go`,
   `network_firewall_egress.go` and `operator_wiring.go`. Do not allow
   injected transport to bypass effective-root, exact-project or backend
   evidence. Firewall reconciliation (#1014) and egress evidence discovery (#1019)
   now have per-call action routes in independently reviewable PRs;
   the remaining shared host/Incus observation bindings are not yet removed.
2. **Incus images:** `images_mutation_wiring.go` and `images_wiring.go`.
   Keep ambient v0.1 alias replacement separate from scoped create-only
   imported image lifecycle.
3. **Root assembly:** `run.go` and `testenv.go`. New capability bundles
   must be scoped to a fresh command invocation and preserve the public
   vs privileged testenv worker separation.
4. **Native/plugin overlaps:** Herdr, gateway topology, OpenCode, workload
   lifecycle/aggregate hooks are owned by #803/#804/#805/#806 or their
   native orchestration tracking issues. Do not migrate these a second
   time in a CLI-specific service layer.

These are **documented architectural debt**, not signoff. A follow-on issue must own residual mutable injection migration if it is
explicitly deferred beyond this CLI epic. Issue closure is inappropriate
until that deferral is accepted and reviewed.

## Validation gates before #755 and #725 can close

Code and dependency verification:
- Hermetic CI must succeed on the final merged `main`, not merely the
  pre-merge PR heads; run `go test ./internal/cli/... ./cmd/... ./internal/testenv/...`,
  `go vet ./internal/cli/... ./cmd/...`, and the repository's Nix/just
  security gates.
- Verify fresh Cobra trees, no root/command dependency reversal, no
  global mutable Cobra commands, and no secret-bearing output changes.
- Recheck **new** codepaths from concurrent environment/client PRs.
  They are not assumed to have been audited by a prior branch snapshot.

Manual operator signoff:
- Follow [fresh-host repeatability #580](https://github.com/blevinn/aginctus/issues/580)
  and the [acceptance documentation](../testing/acceptance/README.md),
  on the actual NixOS development host with a disposable VM for destructive
  Incus/firewall operations.
- Confirm explicit project authorization, rejected image adoption and
  fingerprint collisions, lease denial/release, uncertain post-write
  no-rollback, firewall root/zone/backend/route negative cases, gateway
  credential and plugin readiness, workload lifecycle and testenv worker
  isolation.
- Record operator, host/VM revision, source commit SHA, commands, redacted
  output, expected and observed results, cleanup and repeat, and unresolved
  defects. Provider login/inference requires separate explicit consent.

**Do not close #755 or #725 solely on this document or successful CI.**
The accepted end state requires either documented migration of unavoidable
root global hooks or an explicitly approved, issue-owned deferral, plus
reviewed host-acceptance results.
