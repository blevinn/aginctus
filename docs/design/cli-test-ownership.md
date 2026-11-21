# Ownership of CLI family tests

**Status:** Proposed migration contract, design-only. Tracks
[#903](https://github.com/blevinn/aginctus/issues/903) and
[#755](https://github.com/blevinn/aginctus/issues/755).
Read with [domain-owned CLI migration](cli-domain-ownership.md).

## Decision

Tests live beside their **owning behavior**, not simply where they currently
compile. A test that enters `internal/cli.Run` to assert syntax on one
family is usually testing that family's Cobra registration and should be
rewritten to exercise the corresponding `Register` function with injected
handlers. Tests for Incus/project/image policy belong with the Incus domain.
Tests for native plans, firewall decisions, gateway store mutations and
product-specific trust belong with those domains and plugins.

A small, auditable number of root tests may continue to test entry-point
composition, global options, cross-family routing and the separation of the
public command tree from the privileged testenv worker. **Calling `Run()`
alone does not make a test a root integration test.**

Snapshot at `61ddc39` on 2026-11-15: **70 direct CLI root test files**,
zero tests under `internal/cli/commands/*`. This matrix lists every root
test file; each destination may require splitting the file by individual test
function. A target owner with a slash-separated alternative is a **split**,
not a request to move the file as a whole.

## Complete root test-file disposition

| Root test file | Intended owner(s) | Migration disposition |
| --- | --- | --- |
| `aggregate_remove_test.go` | `orchestration + commands/aggregate` | Split apply/plan domain tests from syntax/output tests |
| `aggregate_status_test.go` | `orchestration + commands/aggregate` | Split apply/plan domain tests from syntax/output tests |
| `aggregate_test.go` | `orchestration + commands/aggregate` | Split apply/plan domain tests from syntax/output tests |
| `architecture_test.go` | `CLI composition` | Retain only true root/global compatibility assertions |
| `commands_aggregate_test.go` | `commands/aggregate` | Direct Register handler/syntax tests; leave only root cross-family cases |
| `commands_basic_test.go` | `CLI composition` | Retain only true root/global compatibility assertions |
| `commands_gateway_assets_test.go` | `commands/gateway/assets + gatewaycontrol` | Direct Register syntax; domain-store behaviors move with gatewaycontrol |
| `commands_gateway_core_test.go` | `commands/gateway` | Direct Register help/syntax and handler callbacks |
| `commands_gateway_egress_test.go` | `commands/gateway/egress` | Direct Register help/syntax and handler callbacks |
| `commands_gateway_registry_test.go` | `commands/gateway/registry` | Direct Register help/syntax and handler callbacks |
| `commands_herdr_test.go` | `commands/herdr` | Direct Register handler/syntax tests; leave only root cross-family cases |
| `commands_images_test.go` | `commands/images` | Direct Register handler/syntax tests; leave only root cross-family cases |
| `commands_leaf_help_test.go` | `CLI root + command families` | Split cross-command discovery from leaf-specific help/flag assertions |
| `commands_network_test.go` | `commands/network` | Direct Register handler/syntax tests; leave only root cross-family cases |
| `commands_operator_test.go` | `commands/operator` | Direct Register handler/syntax tests; leave only root cross-family cases |
| `commands_root_test.go` | `CLI composition` | Retain only true root/global compatibility assertions |
| `commands_syntax_matrix_test.go` | `CLI composition` | Retain only true root/global compatibility assertions |
| `commands_testenv_test.go` | `commands/testenv` | Direct Register handler/syntax tests; leave only root cross-family cases |
| `commands_workload_test.go` | `commands/workload` | Direct Register handler/syntax tests; leave only root cross-family cases |
| `credential_storage_test.go` | `gateway domain + CLI adapter` | Move storage policy tests to gateway; retain CLI invocation contract |
| `doctor_https_test.go` | `commands/doctor + host/Incus` | Split optional fix flag and host HTTP configuration evidence |
| `egress_dhcp_render_test.go` | `gateway + host/net/firewall` | Move policy/render fixtures with owner; CLI output remains separate |
| `gateway_cli_test.go` | `commands/gateway + CLI root` | Move family syntax; retain cross-command entry contract |
| `gateway_credential_attach_test.go` | `gatewaycontrol/gateway/plugins + commands/gateway` | Split domain state/credentials/SIWC behavior from flags/exit contracts |
| `gateway_credential_audit_test.go` | `gatewaycontrol/gateway/plugins + commands/gateway` | Split domain state/credentials/SIWC behavior from flags/exit contracts |
| `gateway_declarations_test.go` | `gatewaycontrol/gateway/plugins + commands/gateway` | Split domain state/credentials/SIWC behavior from flags/exit contracts |
| `gateway_model_render_test.go` | `gatewaycontrol/gateway/plugins + commands/gateway` | Split domain state/credentials/SIWC behavior from flags/exit contracts |
| `gateway_registration_test.go` | `gatewaycontrol/gateway/plugins + commands/gateway` | Split domain state/credentials/SIWC behavior from flags/exit contracts |
| `gateway_responses_verify_test.go` | `gatewaycontrol/gateway/plugins + commands/gateway` | Split domain state/credentials/SIWC behavior from flags/exit contracts |
| `gateway_siwc_test.go` | `gatewaycontrol/gateway/plugins + commands/gateway` | Split domain state/credentials/SIWC behavior from flags/exit contracts |
| `gateway_status_test.go` | `gatewaycontrol/gateway/plugins + commands/gateway` | Split domain state/credentials/SIWC behavior from flags/exit contracts |
| `help_test.go` | `CLI root + command families` | Split cross-command discovery from leaf-specific help/flag assertions |
| `herdr_authorize_test.go` | `Herdr plugin/client + commands/herdr` | Split trust/host pinning/guest transport from command syntax |
| `herdr_console_test.go` | `Herdr plugin/client + commands/herdr` | Split trust/host pinning/guest transport from command syntax |
| `herdr_lifecycle_test.go` | `Herdr plugin/client + commands/herdr` | Split trust/host pinning/guest transport from command syntax |
| `herdr_pin_host_test.go` | `Herdr plugin/client + commands/herdr` | Split trust/host pinning/guest transport from command syntax |
| `herdr_register_test.go` | `Herdr plugin/client + commands/herdr` | Split trust/host pinning/guest transport from command syntax |
| `images_owner_audit_test.go` | `incus/images + commands/images` | Split scoped/legacy policy, lease and actual mutation from syntax and CLI output |
| `images_replace_plan_test.go` | `incus/images + commands/images` | Split scoped/legacy policy, lease and actual mutation from syntax and CLI output |
| `images_scoped_import_test.go` | `incus/images + commands/images` | Split scoped/legacy policy, lease and actual mutation from syntax and CLI output |
| `images_scoped_lease_test.go` | `incus/images + commands/images` | Split scoped/legacy policy, lease and actual mutation from syntax and CLI output |
| `images_scoped_owned_import_test.go` | `incus/images + commands/images` | Split scoped/legacy policy, lease and actual mutation from syntax and CLI output |
| `images_scoped_test.go` | `incus/images + commands/images` | Split scoped/legacy policy, lease and actual mutation from syntax and CLI output |
| `images_test.go` | `incus/images + commands/images` | Split scoped/legacy policy, lease and actual mutation from syntax and CLI output |
| `instance_status_scoped_test.go` | `incus/workload + commands/workload` | Split ownership/status classification from CLI presentation |
| `instance_status_test.go` | `incus/workload + commands/workload` | Split ownership/status classification from CLI presentation |
| `network_firewall_egress_test.go` | `host/net/firewall + commands/network` | Split firewall backend/reconcile policy from command syntax |
| `network_firewall_nixos_nft_test.go` | `host/net/firewall + commands/network` | Split firewall backend/reconcile policy from command syntax |
| `network_firewall_policy_test.go` | `host/net/firewall + commands/network` | Split firewall backend/reconcile policy from command syntax |
| `network_firewall_reconcile_test.go` | `host/net/firewall + commands/network` | Split firewall backend/reconcile policy from command syntax |
| `network_status_test.go` | `host/net/firewall + commands/network` | Split firewall backend/reconcile policy from command syntax |
| `opencode_go_plugin_test.go` | `plugin integration owner + CLI root` | Separate compiled plugin capability contract from root inventory |
| `operator_package_audit_test.go` | `controllerauthority/incus/host + commands/operator` | Split authority/state/units policy from CLI audit flags and output |
| `operator_preflight_test.go` | `controllerauthority/incus/host + commands/operator` | Split authority/state/units policy from CLI audit flags and output |
| `operator_project_confinement_test.go` | `controllerauthority/incus/host + commands/operator` | Split authority/state/units policy from CLI audit flags and output |
| `operator_project_ensure_test.go` | `controllerauthority/incus/host + commands/operator` | Split authority/state/units policy from CLI audit flags and output |
| `operator_project_plan_test.go` | `controllerauthority/incus/host + commands/operator` | Split authority/state/units policy from CLI audit flags and output |
| `operator_state_audit_test.go` | `controllerauthority/incus/host + commands/operator` | Split authority/state/units policy from CLI audit flags and output |
| `operator_units_audit_test.go` | `controllerauthority/incus/host + commands/operator` | Split authority/state/units policy from CLI audit flags and output |
| `plugins_test.go` | `CLI composition` | Retain only true root/global compatibility assertions |
| `run_test.go` | `CLI root + all owning families` | Split by Test function; retain only entry-point, global-flag and cross-family contract cases |
| `testenv_test.go` | `testenv + commands/testenv` | VM backend policy in testenv; public worker isolation stays cross-command |
| `version_test.go` | `CLI composition` | Retain only true root/global compatibility assertions |
| `workload_apply_scoped_dry_run_test.go` | `orchestration/native + commands/workload` | Separate scoped apply/transition/status policy from syntax/confirmation |
| `workload_apply_scoped_execute_test.go` | `orchestration/native + commands/workload` | Separate scoped apply/transition/status policy from syntax/confirmation |
| `workload_apply_scoped_test.go` | `orchestration/native + commands/workload` | Separate scoped apply/transition/status policy from syntax/confirmation |
| `workload_gateway_provision_test.go` | `orchestration/native + commands/workload` | Separate scoped apply/transition/status policy from syntax/confirmation |
| `workload_lifecycle_test.go` | `orchestration/native + commands/workload` | Separate scoped apply/transition/status policy from syntax/confirmation |
| `workload_start_scoped_test.go` | `orchestration/native + commands/workload` | Separate scoped apply/transition/status policy from syntax/confirmation |
| `workload_stop_scoped_test.go` | `orchestration/native + commands/workload` | Separate scoped apply/transition/status policy from syntax/confirmation |

## Required test patterns for command registration

For each `internal/cli/commands/<family>/`:

1. Construct a fresh local Cobra root. Call the family's exported
   `Register(root, ..., handlers)` directly; do not import the parent
   `internal/cli` package, as that would create an import cycle.
2. Provide a loader which records calls and returns a deliberately synthetic
   configuration. Its behavior is controlled by the test, not by
   `package cli` globals.
3. Supply typed handler callbacks that record action/arguments/scope and fail
   the test if called before valid syntax/affirmative confirmation.
4. Test invalid flags/arguments, help and command aliases **without** loader
   or handler calls. Test valid invocation, required flags, independent
   command construction and stdout/stderr/status propagation.
5. Preserve one or more root-level `Run` tests for global flags, legacy
   translation, composition, public tree help and worker isolation. Do not
   delete those just because a leaf-level test exists.

## How to move existing tests safely

For each family: first **add** direct `Register` tests in
`commands/<family>` with a test-controlled load/handler seam, then
remove/rewrite exactly the root tests made redundant by those assertions.
Never drop an owner/permission/side-effect refusal test without a replacement
that exercises the same boundary. Split `run_test.go` by each `Test*`
function; do not mechanically paste it into a new giant testing package.

Domain policy and mutation tests should be migrated **with their production
behavior**. Moving a test into `cli/operations/<family>` is only an
intermediate step when its algorithm still resides there. The long-term
owner is defined in `cli-domain-ownership.md`.

Avoid a general `testutil` package that gives every command unbounded
host/Incus capabilities, and avoid exporting package-private methods solely
to let a test import them. Prefer direct injected interfaces and small local
fixtures.

## Signoff criteria

- Every root test has an owner and an explicit reason to remain or move.
- Every registration family has colocated direct `Register` syntax/handler
  tests; root integration tests test the **composition** boundary, not private
  implementation detail.
- No migration weakens the no-configuration-before-syntax, no-write-on-denial,
  exact-project, namespace ownership, worker separation, failure redaction or
  fresh-Cobra-tree contracts.
- Go test/static analysis and Nix/hermetic security checks pass on each
  incremental PR; full release/manual NixOS host acceptance is tracked
  separately and never inferred from a PR green check.
- No target file-count quota is imposed. A cohesive command test suite can
  reasonably contain multiple small files.

All local coding must use isolated worktrees. Stacks are updated without
rebase/force-push; after merge, retarget the immediate successor to `main`
before forward-integrating main. Repository writes only to
`blevinn/aginctus`.

## Gateway registration audit (2026-11-21)

Following direct `Register` tests for gateway root, gateway egress, and
workload, the remaining gateway family owners are
`commands/gateway/registry` and `commands/gateway/assets`. The independent
registration-test PRs add direct syntax, selector, help and no-loader/store
access assertions beside those owners without touching the live plugin
implementation.

The following root tests **remain deliberately** until end-to-end coverage
has an independently demonstrated replacement:

| Root test | Justification |
| --- | --- |
| `commands_gateway_assets_test.go:TestGatewayAssetCommandsAndSelectors` | Crosses root configuration/store wiring, built-in plugin inventory, provider/model/MCP/catalog persistence and real CLI JSON rendering |
| `commands_gateway_assets_test.go:TestGatewayAssetSyntaxAndHelpStayOffline` | Confirms actual `Run` exit codes, help/output contract and no creation of XDG state paths, not merely a Cobra direct parser |
| `commands_gateway_registry_test.go:TestGatewayRegistryLegacyMapping` | Root compatibility rewriting of deprecated provider/model positional spellings |
| `commands_gateway_registry_test.go:TestGatewayRegistryCobraSyntaxBeforeConfig` | Actual root `Run` exit-2/Usage contract and no config load |
| `commands_gateway_core_test.go` | Full root error handling and help/exit contract |
| `commands_gateway_egress_test.go` | Public root mutation confirmations and explicit no-config denial |

These test names are not evidence of duplication: direct command tests own
handler dispatch and validation timing; root tests own the complete entrypoint
contract. Deleting the latter just to reduce the count would weaken the
architectural gate. Revisit them after #906 separates gateway asset store
actions into typed `gatewaycontrol` operations, when those domain tests can
cover persistence and auth paths independently.

No completed host acceptance is implied. Keep #903 and #755 open until the
remaining root test-function inventory and global mutable-hook audit are
finished and the CLI acceptance runbook is executed.
