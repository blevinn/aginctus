# Image and firewall CLI ownership closeout audit

Status: code-boundary audit; **not** manual NixOS/Incus acceptance.
Tracks #902, #904, #905, #725 and #755. Review on current `main`
after the related domain PR is merged.

## Incus images

| CLI adapter file | Disposition | Owner / deferred proof |
| --- | --- | --- |
| `helpers.go` | CLI re-export/presentation | Artifact, fingerprint, name and image inventory structures are in `internal/incus/images`; delete delegates only after root compatibility tests move |
| `legacy_update.go` | Compatibility | Distinct v0.1 ambient staging, alias promotion and rollback live in `internal/incus/images`; do not reinterpret as an exact-project operation |
| `owned_import.go` | CLI parsing, result text | `internal/incus/images.CreateClaimedImage` holds project lease, verifies pre/post state and does not adopt or roll back uncertain resources |
| `owner_claim.go` | Audit argument parsing/JSON | Claims and fingerprint classification are diagnostic `internal/incus/images` evidence, never authority |
| `read_inventory.go` | CLI inventory presentation | Domain inventory and selected project evidence in `internal/incus/images`; review residual rendering tests at root |
| `replace_plan.go` | CLI plan arguments/output | Read-only proposal, not an authorization to replace. Keep without a claimed-image writer until a reviewed native owner takes replacement |
| `scoped_import.go` | CLI parsing/output | Exact-project create-only lifecycle owned by `internal/incus/images`; no `--reuse` or implicit remote |
| `scoped_inventory.go` | CLI listing/report | Only independently verified local project inventory is rendered; preserve v0.1 ambient list separately |
| `scoped_lease.go` | Compatibility delegate | Local cooperative lease implementation and cross-process contention in `internal/incus/images` |

**Audit finding:** direct domain callers of `CreateClaimedImage` must reject
malformed project scope, alias, owner and empty artifact path before taking a
writer lease. This tranche adds that check with domain-local negative tests.
Do not interpret a cooperative lease or `user.aginctus.*` properties as
independent authorization. Before declaring acceptance, exercise actual
Incus duplicate alias/fingerprint races, cross-process leases and uncertain
post-write responses on the operator's disposable NixOS acceptance VM.

## Host networking and firewall

| CLI adapter file | Disposition | Owner / deferred proof |
| --- | --- | --- |
| `firewall_reconcile.go` | Config/backend dispatch + CLI output | Backend evidence, verified policy, nft child-chain, iptables-nft and firewalld mutations live in `internal/host/net/firewall` |
| `egress_reconcile.go` | Gateway-specific grant discovery + CLI presentation | Root/lock/mutation and RPDB/effective-zone verification in host firewall owner; Incus gateway NIC/network identity remains in gateway owner |
| `firewall_policy.go` | Config-selected read-only report | Bridge/network policy derivation and host link checks in firewall owner |
| `network.go` | CLI requests and dispatch | Preserve typed Incus network authority and manual host validation as a separate gate |
| `status.go` | CLI status/report | Do not convert effective host evidence into mutable configuration authority |

**Audit finding:** host lock acquisition failures must be redacted; raw OS
error strings can reveal sensitive local paths or ownership information.
This tranche returns a stable refusal and tests that bridge inspection and
grant discovery cannot run following a failed lock acquisition.

## Release / closure boundary

Hermetic Go, staticcheck and targeted Nix security contracts are required
for the PR; they do not establish real firewall backend ownership or Incus
project isolation on the operator host. Run the existing manual acceptance
runbook with a disposable VM and the actual NixOS development host before
releasing the final architecture gate. Preserve the separation between
native orchestration, environment-owned gateway topology, and CLI reporting.

After a green current-head CI, #904 and #905 may close as **code migration**
issues only if residual compatibility adapters have justified owners; retain
outstanding real-host validation under #755 and the manual acceptance
tracking issue. Do not close #725/#902/#755 based on this audit alone.
