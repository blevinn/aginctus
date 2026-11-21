# Individual CLI root test function ownership inventory

Snapshot of `main` on 2026-11-21 for #903 / #755 / #725. Every
Go test function under `internal/cli/*_test.go` is enumerated, with
the owner and disposition inherited from the reviewed file-level ledger.
This inheritance is **provisional**: a mixed-responsibility function must
be split and given its actual domain owner before deleting any root
integration evidence. The AST guard in `architecture_guards_test.go`
checks the exact function inventory to prevent additions or removals
without an updated owner decision.

| Root file | Test function | File's assigned current owner | Owner disposition / review |
| --- | --- | --- | --- |
| `aggregate_remove_test.go` | `TestAggregateRemoveDependencyOrder` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_remove_test.go` | `TestAggregateRemoveStopsBeforeNetworkOnGatewayFailure` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_remove_test.go` | `TestAggregateRemoveRejectsFlags` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_remove_test.go` | `TestAggregateRemoveTwice` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_status_test.go` | `TestAggregateStatusReadOnlyAndComplete` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_status_test.go` | `TestAggregateStatusDoesNotPrintPartialResults` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_status_test.go` | `TestAggregateStatusRejectsArguments` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_test.go` | `TestAggregateApplyRenderDeterministicSharedNetwork` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_test.go` | `TestAggregateApplyRejectsInvalidArguments` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_test.go` | `TestAggregateApplyFromRejectsMalformedFileWithoutExecution` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_test.go` | `TestAggregateApplyFromRejectsNonRegularFile` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_test.go` | `TestAggregateApplyFromRejectsTrailingDocuments` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_test.go` | `TestAggregateReadMultiDocumentYAML` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `aggregate_test.go` | `TestAggregateRenderStreamRoundTrip` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `architecture_guards_test.go` | `TestCLIImportBoundaryRules` | CLI composition / global output | Retain; guard root-wide semantics |
| `architecture_guards_test.go` | `TestCLICommandPackagesConstructCobraLocally` | CLI composition / global output | Retain; guard root-wide semantics |
| `architecture_guards_test.go` | `TestCLICommandGlobalStateGuardRejectsUnsafeDeclarations` | CLI composition / global output | Retain; guard root-wide semantics |
| `architecture_guards_test.go` | `TestRootTestOwnerLedgerPreventsDrift` | CLI composition / global output | Retain; guard root-wide semantics |
| `architecture_test.go` | `TestCLICommandAndOperationImportBoundaries` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_basic_test.go` | `TestCobraBasicRejectsSyntaxBeforeConfiguration` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_basic_test.go` | `TestCobraBasicDoesNotRetainFlagStateAcrossRuns` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_basic_test.go` | `TestCobraConfigLoadsOnlyAfterValidArguments` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_gateway_assets_test.go` | `TestGatewayAssetCommandsAndSelectors` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_gateway_assets_test.go` | `TestGatewayAssetSyntaxAndHelpStayOffline` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_gateway_core_test.go` | `TestCobraGatewayCoreRejectsInvalidSyntaxBeforeConfig` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_gateway_egress_test.go` | `TestGatewayEgressCobraRequiresExplicitConfirmationsBeforeConfig` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_gateway_registry_test.go` | `TestGatewayRegistryLegacyMapping` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_gateway_registry_test.go` | `TestGatewayRegistryCobraSyntaxBeforeConfig` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_herdr_test.go` | `TestCobraHerdrRejectsMalformedInputBeforeConfig` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_leaf_help_test.go` | `TestCobraNestedHelpReflectsLeafFlagScope` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_logical_workloads_test.go` | `TestInjectedLogicalWorkloadCLIOutput` | Cross-family logical workload read entrypoint | Retain root Run/output and explicit scope evidence |
| `commands_logical_workloads_test.go` | `TestLogicalWorkloadCLIFailsClosed` | Cross-family logical workload read entrypoint | Retain root Run/output and explicit scope evidence |
| `commands_operator_test.go` | `TestCobraOperatorInvalidInputDoesNotReadConfigOrInspectIncus` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_output_test.go` | `TestPluginInventoryExplicitStructuredOutput` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_output_test.go` | `TestPluginInventoryLegacyDefaultAndExplicitHuman` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_output_test.go` | `TestExplicitUnsupportedOutputFailsBeforeConfig` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_plugin_show_test.go` | `TestPluginShowClientBackedOutput` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_plugin_show_test.go` | `TestPluginShowMissingAndUnsupportedOutput` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_root_test.go` | `TestCobraRootGlobalOptionsPreserveAliasesAndOrder` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_root_test.go` | `TestCobraRootHelpAndErrorsDoNotLoadConfiguration` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_root_test.go` | `TestCobraRootNormalizesLegacyWithGlobalFlags` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_scoped_workload_output_test.go` | `TestScopedWorkloadTypedOutputAndLegacyDefault` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_scoped_workload_output_test.go` | `TestScopedWorkloadTypedOutputFailsClosed` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_syntax_matrix_test.go` | `TestCobraSyntaxBeforeConfigurationMatrix` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_syntax_matrix_test.go` | `TestCobraHelpOfflineNestedMatrix` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_testenv_test.go` | `TestCobraTestenvSyntaxDoesNotCallConfig` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_testenv_test.go` | `TestPublicTestenvDoesNotRegisterPrivilegedWorkerLeaves` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_workload_aliases_test.go` | `TestWorkloadClassAliasesFilterVerifiedResources` | Cross-family class-filtered workload entrypoint | Retain root Run/output and class-mismatch evidence |
| `commands_workload_test.go` | `TestCobraWorkloadLegacyRewrite` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `commands_workload_test.go` | `TestCobraWorkloadInvalidSyntaxHasNoConfigAccess` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `credential_storage_test.go` | `TestGatewayCredentialStorageEnsure` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `credential_storage_test.go` | `TestCredentialStorageEnsureValidatesProjectBeforeMutation` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `doctor_https_test.go` | `TestDoctorDoesNotFixPreexistingUnsafeHTTPSListener` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `doctor_https_test.go` | `TestDoctorAcceptsConfiguredLoopbackWithoutMutation` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `egress_dhcp_render_test.go` | `TestApplyRenderEgressDHCP` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_credential_attach_test.go` | `TestGatewayCredentialAttachmentCommand` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_credential_audit_test.go` | `TestGatewayCredentialStorageAuditCLI` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
