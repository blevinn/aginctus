# Legacy workload-to-environment migration preview

Status: **non-mutating library preview** (#1046), not a migration executable or resource adoption process.

The pure `environment.PreviewLegacyCandidate` API requires a valid legacy `DeploymentSpec`, its explicitly pinned canonical source digest, an enrolled environment UUID with explicit server/project binding, and exactly one reviewed workload mapping for every selected workload. It refuses changed Incus projects, ambiguous target IDs and implicit instance renames. It preserves pinned image fingerprints, ordered profiles and boolean running intent, and emits a complete typed environment candidate with nonsecret source provenance.

The mapping is **a proposal, not evidence of resource ownership**. Matching a name, label, image fingerprint, UUID or digest cannot authorize adoption. The preview does not read or write either state service, install credentials, mutate Incus, copy volumes, change running instances or activate a reconciler. Existing accepted revision history and operation journal entries remain separate.

This preview is a narrow explicitly requested projection, not a compatibility or preservation promise. Before-stability deployments should prefer clean reinstantiation from current authoritative declarations where practical. The preview does not make old state authoritative, guarantee that all runtime state is represented, or authorize deleting/replacing any existing resource. See the [pre-stability compatibility policy](../development/compatibility.md).

## Prerequisites for eventual migration execution

- Independently inventory the current legacy state roots, each relevant Incus project, enrolled environment identities, live resource ownership markers, profiles, external volumes and secret owners
- Freeze the expected legacy source digest and produce a full mapping with explicit physical instance-name decisions; refuse conflicting or foreign resources
- Back up the old source and new project-state volume under verified writer quiescence and record a rollback path
- Establish restricted project credentials, authenticate the selected server/project and test two environments inside the same project for isolation
- Perform a dry-run comparison of the mapped candidate against actual observed resources; require explicit operator approval before acceptance, writer handoff or reconciliation
- Retain application-owned secrets, token refresh owners and volumes in place unless separately migrated by their owning components; never treat this preview as a secret-transfer plan
- Validate disposal/recovery under real Incus testenv before attempting the operator's actual NixOS host acceptance

Do not run a bulk automatic migration simply because `PreviewLegacyCandidate` returns successfully. See #700/#701/#704 for actual acceptance gates.
