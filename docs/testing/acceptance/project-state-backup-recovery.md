# Project-state backup and recovery acceptance

Status: **not yet accepted**. This runbook describes validation requirements for a future administrator-authorized disposable test environment, not commands to operate on a production host.

The existing `LocalProjectStateStore` and `InspectLocalRecoveryInventory` provide local same-UID, read-only evidence for a **caller-enumerated** set of environment UUIDs. They do not authenticate an Incus project, enumerate all project enrollments, stop writers or create a backup.

## Required disposable acceptance

1. Bootstrap a dedicated project, one service instance and one external custom volume using the testenv's own Incus daemon. Verify project identity and exclusive attachment directly from the administrator-owned test setup.
2. Accept at least two environments with distinct UUIDs and independent generations. Record their published revision IDs and nonsecret inventory; prove a stale CAS cannot advance either environment.
3. Stop or fence **every** cooperating writer using a future production-grade service mechanism, independently verify that no writer remains, and snapshot the custom volume. A pair of matching read passes is only a best-effort race detector.
4. Recreate the service instance on the original volume and verify the published heads, per-environment lineage, metadata integrity and absence of unexpected activation.
5. Restore the snapshot to a **new isolated disposable volume and instance**, with the same verified server/project scope; refuse any foreign/mismatched project or pre-existing conflicting resource. Audit both histories before allowing a reader.
6. Inject corrupted parent records, incomplete pointers and unpublished immutable revision orphans. Restoration and reading must fail closed or explicitly classify the orphan as unaccepted. Never auto-promote or delete an orphan.
7. Test backup taken during attempted concurrent promotion, cancellation, and service restart. If quiescence cannot be proven, backup acceptance fails and no restoration claim is recorded.
8. Capture nonsecret VM/Incus versions, testenv profile, outcome and failure evidence. Do not copy runtime secrets into evidence or change the development host's Incus resources.

The same-UID file lock cannot fence all remote project principals or external Incus administrators. Real project writer fencing, physical snapshot and restoration evidence are required before closing #695/#701. Do not turn the fixture recovery inventory into a production backup command.
