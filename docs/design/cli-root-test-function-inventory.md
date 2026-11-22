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
| `commands_output_test.go` | `TestStructuredErrorUsesStderrAndDoesNotClaimSuccess` | CLI composition / machine error serialization | Retain root integration and non-disclosure evidence |
| `commands_output_test.go` | `TestUnexpectedStructuredErrorDoesNotLeakBackendDetails` | CLI composition / machine error serialization | Retain root integration and non-disclosure evidence |
| `commands_acceptance_preview_test.go` | `TestAcceptancePlanCLIUsesVerifiedStateRead` | State planning CLI composition | Retain read-only and fail-closed root integration evidence |
| `commands_acceptance_preview_test.go` | `TestAcceptancePlanCLIRejectsAbsentAuthority` | State planning CLI composition | Retain read-only and fail-closed root integration evidence |
| `read_capabilities_test.go` | `TestUnifiedReadCapabilitiesResolveEnvironmentDisplayName` | CLI read composition | Retain verified display-name selection and fail-closed regression evidence |
| `read_capabilities_test.go` | `TestUnifiedReadCapabilitiesRejectAmbiguousAndStaleEnvironmentNames` | CLI read composition | Retain verified display-name selection and fail-closed regression evidence |
| `read_capabilities_test.go` | `TestUnifiedReadCapabilitiesShareOneInvocation` | CLI read composition | Retain root cross-family and missing-authority evidence |
| `read_capabilities_test.go` | `TestUnifiedReadCapabilitiesFailClosedWhenMissing` | CLI read composition | Retain root cross-family and missing-authority evidence |
| `commands_state_reads_test.go` | `TestStateCLIReadOnlyTypedOutput` | State read-only CLI composition | Retain fail-closed root entrypoint and typed output evidence |
| `commands_state_reads_test.go` | `TestStateCLIDeniesAbsentAuthorityAndProject` | State read-only CLI composition | Retain fail-closed root entrypoint and typed output evidence |
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
| `architecture_guards_test.go` | `TestCLIRootFunctionInjectionSeamsAreAccountedFor` | CLI composition / global output | Retain; guard root-wide semantics |
| `architecture_guards_test.go` | `TestCLIRootTestFunctionsHaveOwnerRecords` | CLI composition / global output | Retain; guard root-wide semantics |
| `architecture_test.go` | `TestCLICommandAndOperationImportBoundaries` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_basic_test.go` | `TestCobraBasicRejectsSyntaxBeforeConfiguration` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_basic_test.go` | `TestCobraBasicDoesNotRetainFlagStateAcrossRuns` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_basic_test.go` | `TestCobraConfigLoadsOnlyAfterValidArguments` | CLI composition / global output | Retain; guard root-wide semantics |
| `commands_config_scope_test.go` | `TestConfigScopeStructuredOutputAndNoAuthorityClaim` | Cross-family configured project preference presentation | Retain root Run/format/authorization disclaimer evidence |
| `commands_config_scope_test.go` | `TestConfigScopeDoesNotExtendLegacyOutputGate` | Cross-family configured project preference presentation | Retain root Run/format/authorization disclaimer evidence |
| `commands_system_status_test.go` | `TestSystemStatusTypedOutput` | Cross-family Incus server observation presentation | Retain root Run/format and missing-backend evidence |
| `commands_system_status_test.go` | `TestSystemStatusRejectsUnavailableServerAndUnrelatedOutput` | Cross-family Incus server observation presentation | Retain root Run/format and missing-backend evidence |
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
| `commands_environment_reads_test.go` | `TestEnvironmentShowResolvesUniqueNameThroughVerifiedInventory` | Environment CLI composition | Retain authenticated name-selection and fail-closed regression evidence |
| `commands_environment_reads_test.go` | `TestEnvironmentShowRejectsAmbiguousAndStaleEnrollment` | Environment CLI composition | Retain authenticated name-selection and fail-closed regression evidence |
| `commands_environment_reads_test.go` | `TestInjectedAuthenticatedEnvironmentCLI` | Cross-family authenticated environment read entrypoint | Retain root Run/output and fail-closed authority evidence |
| `commands_environment_reads_test.go` | `TestEnvironmentCLIRequiresAuthenticatedBackend` | Cross-family authenticated environment read entrypoint | Retain root Run/output and fail-closed authority evidence |
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
| `gateway_declarations_test.go` | `TestConfiguredGatewayAssets` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_model_render_test.go` | `TestRenderRegisteredChatGPTModelIsReadOnly` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_model_render_test.go` | `TestRenderRegisteredChatGPTModelRejectsAmbiguousCredentials` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_model_render_test.go` | `TestRenderOpenCodeGoAndNativeChatGPTTogether` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_registration_test.go` | `TestGatewayRegistrationDryRun` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `gateway_registration_test.go` | `TestProviderListAndShow` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `gateway_registration_test.go` | `TestGatewayModelRegistryAndAliasPreflight` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `gateway_registration_test.go` | `TestOpenCodeGoRegistrationOffline` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `gateway_registration_test.go` | `TestOpenCodeGoHelpIsDiscoverable` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `gateway_responses_verify_test.go` | `TestGatewayResponsesVerifyFromStdin` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_siwc_test.go` | `TestSIWCProviderOperationsUseConfiguredGatewayWithoutLocalCredentials` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_siwc_test.go` | `TestSIWCLoginHelpIsOfflineAndShowsConsent` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_status_test.go` | `TestGatewayStatus` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `gateway_status_test.go` | `TestGatewayStatusDaemonFailure` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `help_test.go` | `TestHelpOfflineInventory` | CLI composition / global output | Retain; guard root-wide semantics |
| `help_test.go` | `TestHelpGeneratedFamilyTreeMatchesCanonicalSyntax` | CLI composition / global output | Retain; guard root-wide semantics |
| `help_test.go` | `TestHelpLeafFlagsAndSafetyClaims` | CLI composition / global output | Retain; guard root-wide semantics |
| `help_test.go` | `TestHelpAdvertisedAliasesDispatch` | CLI composition / global output | Retain; guard root-wide semantics |
| `help_test.go` | `TestHelpEgressPreviewAndConfirmationSyntax` | CLI composition / global output | Retain; guard root-wide semantics |
| `help_test.go` | `TestHelpApplyFromStreamSyntax` | CLI composition / global output | Retain; guard root-wide semantics |
| `help_budget_test.go` | `TestBasicHelpBudgetRealTree` | CLI composition / bounded help presentation | Retain; verify output limits across registered commands |
| `help_modes_test.go` | `TestHelpExamplesUsesReadOnlyAuthoredExamples` | CLI composition / reviewed examples help mode | Retain; verify parsed modes and authored examples |
| `help_modes_test.go` | `TestHelpExamplesRejectsUnknownModes` | CLI composition / reviewed examples help mode | Retain; verify parsed modes and authored examples |
| `herdr_authorize_test.go` | `TestParseHerdrPublicIdentity` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_authorize_test.go` | `TestHerdrAuthorizeAbsentKeyReconcilesAndVerifies` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_authorize_test.go` | `TestHerdrAuthorizeAlreadyInstalledDoesNotRewrite` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_authorize_test.go` | `TestHerdrAuthorizeRejectsUnexpectedExistingKey` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_authorize_test.go` | `TestHerdrAuthorizeRejectsForeignWorkloadBeforeGuestExec` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_console_test.go` | `TestHerdrConsoleRequiresOwnedRunningClient` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_console_test.go` | `TestHerdrConsolePropagatesExitFailure` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_console_test.go` | `TestHerdrConsoleCommandUsesManagedServerContext` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_lifecycle_test.go` | `TestHerdrLifecycleTransitions` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_lifecycle_test.go` | `TestHerdrLifecycleForeignInstanceCannotBeStopped` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_lifecycle_test.go` | `TestHerdrLifecycleStopFailureBlocksDelete` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_lifecycle_test.go` | `TestHerdrLifecycleCommands` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_pin_host_test.go` | `TestParseWorkloadHostKey` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_pin_host_test.go` | `TestParseManagementAddress` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_pin_host_test.go` | `TestHerdrPinHostReconcilesAndProbesStrictly` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_pin_host_test.go` | `TestHerdrPinHostRejectsMismatchedExistingTrust` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_pin_host_test.go` | `TestHerdrPinHostFailsWhenStrictSSHBlocked` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_register_test.go` | `TestParseSavedHerdrMachines` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_register_test.go` | `TestSavedHerdrMachineConflictsFailClosed` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_register_test.go` | `TestHerdrRegisterStrictPreflightAndIdempotence` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_register_test.go` | `TestHerdrRegisterSSHProbeMismatchReportsAccount` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `herdr_register_test.go` | `TestHerdrAuthorizationGuestExecSeparatesStdoutAndStderr` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_owner_audit_test.go` | `TestImageOwnerAuditRejectsInvalidArgumentsWithoutIncus` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_owner_audit_test.go` | `TestImageOwnerAuditScopedReadAndRedaction` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_replace_plan_test.go` | `TestImageReplacePlanRejectsInvalidArgumentsBeforeIncus` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_replace_plan_test.go` | `TestImageReplacementPlanClassifiesStateAndControllers` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_replace_plan_test.go` | `TestImageReplacementPlanOwnerSymmetry` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_import_test.go` | `TestScopedImageImportArgs` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_import_test.go` | `TestScopedImageImportRefusesConflictsAndUncertainState` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_import_test.go` | `TestScopedImageImportInvalidArgsNeverContactIncus` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_import_test.go` | `TestScopedImageImportTopLevelDispatch` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_lease_test.go` | `TestScopedImageImportLeaseContentionFailsBeforeIncus` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_lease_test.go` | `TestScopedImageWriterLeaseReleasedOnInspectionFailure` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_lease_test.go` | `TestScopedImageWriterLeaseHeldThroughPostcondition` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_owned_import_test.go` | `TestScopedOwnedImageImportArguments` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_owned_import_test.go` | `TestScopedOwnedImportSafetyAndPostconditions` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_owned_import_test.go` | `TestScopedOwnedImportCLIRejectsWrongProjectWithoutIncus` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_test.go` | `TestScopedImageInventoryFailsClosed` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_test.go` | `TestScopedImageInventoryRejectsImplicitOrMismatchedProject` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_test.go` | `TestScopedImageInventoryUsesCLIDispatch` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_scoped_test.go` | `TestScopedImageInventoryHonorsEffectiveConfigOverride` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `images_test.go` | `TestImagesListAndAlias` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `images_test.go` | `TestImageUpdatePreservesActiveAliasOnFailedImport` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `images_test.go` | `TestImageUpdateRejectsInvalidInputBeforeIncus` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `images_test.go` | `TestImageUpdateExistingFingerprintIsNoop` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `images_test.go` | `TestImageUpdateExistingFingerprintCreatesAlias` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `images_test.go` | `TestImageUpdateRestoresOldAliasWhenPromotionFails` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `images_test.go` | `TestImageUpdateReportsPromotionAndRollbackFailures` | Cross-family entrypoint + domain/store contracts | Retain root Run/output/legacy compatibility; direct Register tests colocated |
| `instance_status_scoped_test.go` | `TestScopedInstanceListArgumentsAndParsing` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `instance_status_scoped_test.go` | `TestScopedInstanceStatusCommands` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `instance_status_scoped_test.go` | `TestScopedInstanceStatusFailsClosed` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `instance_status_scoped_test.go` | `TestScopedWorkloadListNeverPrintsPartialResults` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `instance_status_test.go` | `TestWorkloadStatusUsesLiveIncusData` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `instance_status_test.go` | `TestHerdrClientStatusRejectsDaemonError` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `instance_status_test.go` | `TestWorkloadListRejectsForeignAndDaemonErrors` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_egress_test.go` | `TestEgressLockAndState` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_egress_test.go` | `TestEgressNoUnprivilegedMutationOrRetiredStaging` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_egress_test.go` | `TestDiscoverEgressIdentityAndRoutes` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_nixos_nft_test.go` | `TestNativeNftReconcileScopedToOwnedChildChain` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_nixos_nft_test.go` | `TestNativeNftDeletedBridgeClearsOnlyOwnedPermissions` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_nixos_nft_test.go` | `TestNativeNftMissingInputHookNeverMutates` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_nixos_nft_test.go` | `TestNativeNftBackendRequiresExplicitSelection` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_policy_test.go` | `TestFirewallPolicyRejectsUnownedBeforeDisplay` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_reconcile_test.go` | `TestFirewalldReconcileUsesOwnedZoneAndMinimalRules` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_reconcile_test.go` | `TestFirewalldDisownedNetworkRemovesRuntimeExceptions` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_reconcile_test.go` | `TestFirewallReconcileRejectsMixedHostBackendsBeforeMutation` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_firewall_reconcile_test.go` | `TestUnprivilegedFirewallReconcileDoesNotExecuteCommands` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_status_test.go` | `TestNetworkStatusLiveStateAndOwnership` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `network_status_test.go` | `TestNetworkStatusRejectsOptionsWithoutInspecting` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `opencode_go_plugin_test.go` | `TestOpenCodeGoPluginRendering` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_preflight_test.go` | `TestOperatorPreflightProjectReadOnly` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_preflight_test.go` | `TestOperatorPreflightNoProjectOrInvalidProject` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_preflight_test.go` | `TestOperatorPreflightOfflineOrDenied` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_preflight_test.go` | `TestOperatorPreflightProjectEnumerationIsOptional` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_preflight_test.go` | `TestOperatorPreflightIdentityEvidence` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_preflight_test.go` | `TestOperatorPreflightRejectsNullInstanceInventory` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_preflight_test.go` | `TestOperatorProbeFailureClassification` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_preflight_test.go` | `TestOperatorPreflightCancellationDuringOptionalRead` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_project_confinement_test.go` | `TestInspectProjectConfinementGetOnlyAndFailClosed` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_project_ensure_test.go` | `TestOperatorProjectEnsureFailClosed` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_project_ensure_test.go` | `TestOperatorProjectEnsureNeverCreatesOnUnsafeArgs` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_project_ensure_test.go` | `TestOperatorProjectEnsureRejectsConfigOverridesBeforeLoading` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_project_plan_test.go` | `TestOperatorProjectPlanConservativeReadOnly` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_project_plan_test.go` | `TestOperatorProjectPlanCannotLoadHostConfiguration` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_project_plan_test.go` | `TestOperatorProjectPlanRejectsInvalidNameBeforeIncus` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_state_audit_test.go` | `TestOperatorGatewayStateMetadata` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_state_audit_test.go` | `TestOperatorGatewayCredentialLocationIsNotAnOwnerHandoff` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_state_audit_test.go` | `TestOperatorGatewayCredentialAuditRejectsUnsafeLocation` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_units_audit_test.go` | `TestOperatorUnitsInventoryDoesNotInferControllerOwnership` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `operator_units_audit_test.go` | `TestOperatorUnitsInventoryRejectsOwnerSelection` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `plugins_test.go` | `TestCompiledPluginInventoryIsLocal` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestDoctorSuccess` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestConfigAlias` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestConfigurationOptions` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestNetworkEnsureUsesEffectiveConfiguration` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestNetworkEnsureRejectsWrongConfigurationType` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestConfigurationLoadFailure` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestUnknownCommand` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestNetworkEnsureDryRunOutput` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestNetworkTeardownUsesConfiguredNameAndOptions` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestNetworkTeardownAbsent` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestNetworkRejectsUnknownOption` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestHerdrClientEnsureUsesEffectiveConfiguration` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestHerdrClientTeardownUsesConfiguredNameAndForce` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestHerdrClientStopsWhenOrchestrationFails` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestGatewayRenderUsesEffectiveConfiguration` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestGatewayUpStopsBeforeNetworkMutationWhenSecretInitializationFails` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestWorkloadEnsureUsesGenericConfiguredWorkload` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestWorkloadTeardownMapsForceAndDelete` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestWorkloadUnknownNameFailsConfigurationLookup` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestDoctorDetectsMissingHTTPSListener` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestDoctorFixConfiguresLoopbackOnly` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestNetworkApplyAndRemoveCommands` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestHerdrClientApplyAndRemoveCommands` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestWorkloadListAndApplyAll` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestWorkloadRemoveNamed` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestGeneratedGatewayHelpIsDispatchable` | CLI composition / global output | Retain; guard root-wide semantics |
| `run_test.go` | `TestGeneratedGatewayHelpMatchesHelpCommand` | CLI composition / global output | Retain; guard root-wide semantics |
| `testenv_test.go` | `TestTestenvOfflineHelpListAndPlan` | CLI composition / global output | Retain; guard root-wide semantics |
| `testenv_test.go` | `TestTestenvRejectsUnexpectedCommandsWithoutExecution` | CLI composition / global output | Retain; guard root-wide semantics |
| `testenv_test.go` | `TestTestenvDispatchesOnlyKnownScenario` | CLI composition / global output | Retain; guard root-wide semantics |
| `testenv_test.go` | `TestTestenvMCPIsConfigFreeAndReadOnly` | CLI composition / global output | Retain; guard root-wide semantics |
| `testenv_test.go` | `TestTestenvBackendDiscoveryIsReadOnlyAndUninstalled` | CLI composition / global output | Retain; guard root-wide semantics |
| `testenv_test.go` | `TestTestenvIncusPolicyIsOfflineAndRestricted` | CLI composition / global output | Retain; guard root-wide semantics |
| `version_test.go` | `TestVersionVCSFallback` | CLI composition / global output | Retain; guard root-wide semantics |
| `version_test.go` | `TestVersionInjectedRevisionOffline` | CLI composition / global output | Retain; guard root-wide semantics |
| `version_test.go` | `TestVersionEmbeddedSource` | CLI composition / global output | Retain; guard root-wide semantics |
| `workload_apply_scoped_dry_run_test.go` | `TestScopedWorkloadApplyNativeDryRunSummarizesWithoutSecrets` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_apply_scoped_dry_run_test.go` | `TestScopedWorkloadApplyNativeDryRunFailsClosed` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_apply_scoped_dry_run_test.go` | `TestScopedWorkloadApplyNativeDryRunRejectsDriftAndUnownedProject` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_apply_scoped_dry_run_test.go` | `TestScopedWorkloadApplyNativeDryRunHelpOffline` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_apply_scoped_execute_test.go` | `TestConfirmedScopedApplyBoundaries` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_apply_scoped_execute_test.go` | `TestScopedApplyConfirmationSyntax` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_apply_scoped_test.go` | `TestScopedWorkloadApplyRenderOnlyOneProjectBoundInstance` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_apply_scoped_test.go` | `TestScopedWorkloadApplyRenderRefusesMutatingSyntax` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_apply_scoped_test.go` | `TestScopedWorkloadApplyRenderRequiresPresentOwnedInstance` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_apply_scoped_test.go` | `TestScopedWorkloadApplyRenderHelpOffline` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_gateway_provision_test.go` | `TestDiscoverGatewayIPv4` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_gateway_provision_test.go` | `TestReadWorkloadKey` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_gateway_provision_test.go` | `TestReadWorkloadKeyRejectsUnsafeParentOversizeAndSpecialFile` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_lifecycle_test.go` | `TestWorkloadLifecycleTransitions` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_lifecycle_test.go` | `TestWorkloadLifecycleRejectsForeignInstance` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_lifecycle_test.go` | `TestWorkloadCLIStartStop` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_lifecycle_test.go` | `TestWorkloadStopFailurePreventsDeletion` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_start_scoped_test.go` | `TestScopedWorkloadStartRequiresExplicitProjectAndConfirmation` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_start_scoped_test.go` | `TestScopedWorkloadStartSingleOwnedInstance` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_start_scoped_test.go` | `TestScopedWorkloadStartRejectsUnverifiedStateWithoutMutation` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_start_scoped_test.go` | `TestScopedWorkloadStartPostflightFailureNotReportedAsSuccess` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_stop_scoped_test.go` | `TestScopedWorkloadStopExactArgsAndSingleTarget` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_stop_scoped_test.go` | `TestScopedWorkloadStopRefusesUnconfirmedOrUnscopedAction` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_stop_scoped_test.go` | `TestScopedWorkloadStopFailsClosedBeforeMutation` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_stop_scoped_test.go` | `TestScopedWorkloadStopUnverifiedPostflightAndDaemonFailure` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
| `workload_stop_scoped_test.go` | `TestScopedWorkloadStopHelpAvailableOffline` | Root-integrated domain/privilege contract | Split domain policy into owning package where independent of Run; retain negative CLI entrypoint evidence |
