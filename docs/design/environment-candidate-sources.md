# Explicit environment candidate sources

**Status:** Additive configured-state contract; not a deployed project-state writer.
Parent: [ADR 0006](../adr/0006-environment-incus-authority.md), [project state storage](environment-state-storage.md). Authorization and mutation gates: [#695](https://github.com/blevinn/aginctus/issues/695), [#696](https://github.com/blevinn/aginctus/issues/696), [#704](https://github.com/blevinn/aginctus/issues/704).

## Decision: one candidate, one explicitly selected source

An environment declaration is a complete, typed `aginctus.dev/environment/v1alpha1` `EnvironmentCandidate`, independent of whether NixOS, Home Manager or a standalone file produced it. It includes an immutable environment UUID, renameable display name, **declared** Incus `serverId` and `project`, an explicit set of selected workloads, and pinned, nonsecret workload specifications. It can also carry typed native workload attachments and optional Herdr and gateway intent. There is no root/user partial candidate patch or recursive system→user→environment→flag merge.

Go callers must use `environment.ReadCandidateFile(absolutePath)` with a **single explicit path** and then `environment.SelectCandidate(candidate, enrolledDescriptor, observedBinding)`. These operations are non-mutating and do not authorize Incus writes. In particular, `observedBinding` must have been independently established through an authenticated Incus session; a caller-provided or Nix-declared server fingerprint is not identity evidence on its own.

Candidate parsing rejects unknown, duplicated, case-aliased, null, privileged and credential-like fields. Workloads must use immutable SHA256 image fingerprints and explicit profiles. Canonicalization sorts the selected workload set but preserves Incus profile order.

## Pure source-to-source drift inspection

`environment.CompareCandidates(enrolled, observed, previous, proposed)` is a
**read-only comparison of two fully typed configured declarations** for the
same enrolled immutable environment identity. Both must match the enrolled UUID
and exact server/project binding, and `observed` must be established
independently by an authenticated Incus client before it is passed to this
helper. Passing an arbitrary `observed` value does **not** authenticate it.

The result contains only environment ID, project, canonical candidate digests,
a display-name-change flag, and selected/unselected workload IDs and modified
**field names**. An unselected workload is not a deletion order. A display name
may change without changing the environment's immutable identity. No file is
written; no accepted-state service is contacted and no credential, CAS
promotion, fencing lease or Incus writer is introduced. The comparison does
**not** establish which declaration was accepted, who supplied it, or whether
its recorded project is authorized.

The comparison temporarily reuses the legacy workload-only field diff
algorithm, but **does not** convert, migrate, adopt, or publish any legacy
`DeploymentSpec` revision. Authentic configured→accepted→observed drift and
revision history require independently authenticated state-service reads and
project-scoped environment records under #695/#696/#704.

## Declaration producers

| Source | Candidate declaration | Output | Activation |
| --- | --- | --- | --- |
| NixOS | `services.aginctus.environmentCandidate` | `/etc/aginctus/environment-candidate.json` (Nix-managed source) | Renders data only; no acceptance or reconciliation triggered by the candidate |
| Home Manager | `programs.aginctus.environmentCandidate` | `$XDG_CONFIG_HOME/aginctus/environment-candidate.json` | Renders data only; no acceptance, Incus access or user service |
| Standalone | Explicit versioned JSON file | Caller-provided absolute path | None |

A module can render candidate data for a project different from the ordinary CLI preference `settings.incus.project`, because the candidate has its **own explicit binding**. This is not silent redirection: later selection must verify the immutable enrolled binding using real Incus connection evidence. Changing `AGINCTUS_INCUS_PROJECT`, root/user JSON settings or a CLI flag must never reinterpret the candidate as applying to another project or environment.

A NixOS and Home Manager candidate describing the same UUID must not be merged or arbitrated by Unix UID. The caller must choose exactly one candidate file. If a future discovery API presents both, ambiguous/conflicting declarations must be reported rather than last-writer-wins. No system/user candidate merge is available.

The modules share `nix/modules/environment-candidate.nix`. Optional lifecycle
fields are omitted when unset. The gateway producer accepts ordinary registry
hostnames with an optional numeric port; the Go parser remains authoritative
for standalone JSON. Both modules require pre-resolved immutable image
references and accept only nonsecret candidate data. The
`environment-candidate-parity` flake check compares both generated JSON files
and parses them through the Go contract.

## Legacy compatibility and migration

The old Home Manager `workloadCandidate` emits the experimental `aginctus.dev/accepted/v1alpha1` `WorkloadDeployment` schema into `workload-candidate.json`. It remains supported **only for the original explicitly selected same-UID local-state experiment**, and cannot silently become an environment candidate. The Home Manager module rejects enabling both candidate options together.

The current `internal/config.Loader.Load` continues to merge default/system/user/environment/CLI **ordinary CLI configuration** for compatibility. Existing legacy commands may still consume those preferences; they are not a source-neutral environment candidate, accepted revision, authenticated state authority, or a replacement for the explicitly selected candidate file. The new parser deliberately does not accept a generic merged config map.

The common schema describes environment-scoped intent and references to existing host capabilities. Project-wide resources, network/host policy, secrets, environment profiles and other domains require separate reviewed schemas and ownership. It is not a completed replacement for the legacy deployment state namespace. Any migration of existing accepted revisions, instance names, profiles, volumes, keys or resource labels needs a dry-run inventory, explicit old→new mapping, verified server/project binding and backup before promotion. Do not relabel or adopt existing resources automatically.

## Exit criteria for runtime adoption

1. Both module outputs parse to the same Go contract and reject unsafe/duplicate candidates.
2. Two environments sharing one Incus project never collide by display name or overwrite each other's accepted revision history.
3. Same UUID with differing server/project bindings is rejected; remote alias or environment variables cannot redirect it.
4. Project service authenticates the principal and verifies scope without an unrestricted root deputy, then performs per-environment CAS and writer fencing.
5. Read-only candidate selection and diff never mutate running resources; real Incus negative VM tests and migration acceptance precede privileged integration.

The separate root-owned host configuration may eventually use its own configured → accepted → observed lifecycle, but remains outside project candidate state. Ordinary NixOS switching and existing opt-in `aginctus-reconcile` behavior are not changed by this slice.
