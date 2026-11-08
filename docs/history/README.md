# Historical documentation and provenance

These materials are retained to understand prior decisions and **what was or was not verified at the time**. They are not the current operator API, authoritative resource inventory, or permission to act on an existing host. Use [getting started](../getting-started.md), [current architecture](../architecture.md), [testing tiers](../testing/README.md), and installed `aginctus help` before acting.

| Artifact | Why retained | Current source of truth |
| --- | --- | --- |
| [v0.1 deployment/acceptance handoff](../v0.1-deployment-acceptance.md) | Detailed unexecuted fresh-host acceptance and cleanup procedure; **not** a PASS | [v0.1 release notes](../releases/v0.1.0.md) and [deferred gate #580](https://github.com/blevinn/aginctus/issues/580) |
| [Herdr first milestone](../milestones/0001-herdr-console.md) | Historical placeholder with links to implemented behavior | [Current architecture](../architecture.md) and [Herdr operations](../herdr-machine-registration.md) |
| [Early v0.2 milestone](../milestones/0002-v0.2.md) | Superseded scope sketch | [Release roll-up #173](https://github.com/blevinn/aginctus/issues/173) |

## Retention policy

Preserve distinct security rationale, failure evidence, release sign-off constraints and rollback mechanics. Remove stale pilot addresses, dated status snapshots and issue-by-issue progress narratives from **active** runbooks, not from their immutable Git/release evidence sources. Existing `docs/design/` proposals and `docs/adr/` decisions remain separate: proposal is not shipped functionality, and an ADR is not a runbook. Documentation reorganizations should preserve source history with explicit mapping in the [baseline inventory](../internal/document-inventory.md).
