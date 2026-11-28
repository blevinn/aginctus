# Root CLI test ownership ledger

Tracks #903 / #755 / #902. Snapshot on `main` at 2026-11-21,
after the image/firewall and gateway direct-`Register` test migrations.

**Scope:** Every `internal/cli/*_test.go` file is listed exactly once.
This is a *file-level accounting and ownership gate*, not a claim that
every individual function already lives in its final package.
Cross-family CLI `Run`/exit/output, credentials, explicit privilege
refusals, and actual persistence are intentionally retained even where
direct family `Register` tests now exist.

The mechanically checked ledger must be updated when a root test file is
added or deleted. A retained root test is not a reason to duplicate domain
algorithms in production. The next function-level audit may split tests
without dropping their integration evidence.

| Root test | Current owner / focus | Disposition |
| --- | --- | --- |
| `aggregate_remove_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_status_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `architecture_guards_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `architecture_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_basic_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `help_budget_test.go` | CLI composition / bounded help presentation | Retain; verify output limits across registered commands |
| `commands_config_scope_test.go` | Cross-family configured project preference presentation | Retain root Run/format/authorization disclaimer evidence |
| `commands_system_status_test.go` | Cross-family Incus server observation presentation | Retain root Run/format and missing-backend evidence |
| `environment_read_capabilities_test.go` | CLI project-scoped domain read composition | Retain root positive and fail-closed cross-family output evidence |
| `read_capabilities_test.go` | Unified read-only CLI dependency injection | Retain cross-family root invocation and fail-closed evidence |
| `commands_state_reads_test.go` | State read-only invocation and missing-authority contract | Retain root Run and structured output integration evidence |
| `commands_native_intent_plan_test.go` | Authenticated snapshot planning and installed CLI invocation | Retain scope, deterministic output and no-configuration denial evidence |
| `commands_native_operation_test.go` | Component operation inspection CLI composition | Retain distinct component and workload authority selection evidence |
| `commands_acceptance_preview_test.go` | Read-only lifecycle plan and missing-authority contract | Retain root typed output and fail-closed integration evidence |
| `commands_gateway_assets_test.go` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_gateway_core_test.go` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_gateway_egress_test.go` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_gateway_registry_test.go` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_herdr_test.go` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_leaf_help_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_operator_test.go` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_output_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_plugin_show_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_root_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_scoped_workload_output_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_syntax_matrix_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_testenv_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_environment_reads_test.go` | Cross-family authenticated environment read entrypoint | Retain root Run/output and fail-closed authority evidence |
| `commands_logical_workloads_test.go` | Cross-family logical workload read entrypoint | Retain root Run/output and explicit scope evidence |
| `commands_workload_test.go` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_workload_aliases_test.go` | Cross-family class-filtered workload entrypoint | Retain root Run/output and class-mismatch evidence |
| `credential_storage_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `doctor_https_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `egress_dhcp_render_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_cli_test.go` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `gateway_credential_attach_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_credential_audit_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_declarations_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_model_render_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_registration_test.go` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `gateway_responses_verify_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_siwc_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_status_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `help_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `help_modes_test.go` | CLI composition / reviewed examples help mode | Retain; verify parsed modes and authored examples |
| `herdr_authorize_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_console_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_lifecycle_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_pin_host_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_register_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_owner_audit_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_replace_plan_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_import_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_lease_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_owned_import_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_test.go` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `instance_status_scoped_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `instance_status_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_egress_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_nixos_nft_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_policy_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_reconcile_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_status_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `opencode_go_plugin_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_preflight_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_project_confinement_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_project_ensure_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_project_plan_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_state_audit_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_units_audit_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `plugins_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `testenv_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `version_test.go` | CLI composition / global output | Retain; guard root-wide semantics |
| `workload_apply_scoped_dry_run_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_apply_scoped_execute_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_apply_scoped_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_gateway_provision_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_lifecycle_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_start_scoped_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_stop_scoped_test.go` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |

## Reconciliation

- The previous `cli-test-ownership.md` matrix omitted
  `architecture_guards_test.go`, `commands_output_test.go`,
  `commands_plugin_show_test.go` and
  `commands_scoped_workload_output_test.go` from its root snapshot.
- Its root-file mapping retained stale entries for
  `commands_aggregate_test.go`, `commands_images_test.go`,
  `commands_network_test.go` and `operator_package_audit_test.go`.
  Those names are **not** current root files and must not be used as
  justification for removing another root integration contract.
- Direct command registration tests now exist for images, network,
  aggregate, Herdr, operator, testenv, workload, gateway root,
  gateway egress, gateway registry and gateway assets. Preserve root
  negative error/side-effect evidence until equivalent full-path tests
  have been proven.

## Mutable hooks and final signoff

- Command package `cobra.Command` globals and `init()` registration
  are already forbidden by `architecture_guards_test.go`.
- Root package injection seams are not granted automatic exceptions by
  this file ledger. Any mutable production package-level variable used
  to alter behavior from a test must have a named owner and replacement
  plan before #903 closes; do not add exported test-only setters.
- Domain/store actions in `commands/gateway/assets` are tracked under
  #906 and must leave Cobra registration before final CLI closure.
- The full Go/Nix/hermetic/privileged manual acceptance gate remains
  separate from this accounting check.

The ledger deliberately records **75 files** and does not
set a numerical target for deleting cross-family root integration tests.
