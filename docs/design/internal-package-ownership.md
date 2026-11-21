# Internal package ownership inventory

Baseline: `main` at `5039da74273d812cca85fd55aa69609e38abe2ba` (2026-11-15). Tracking: [#799](https://github.com/blevinn/aginctus/issues/799); target architecture: [plugin-oriented core](plugin-oriented-core.md).

This is a **structural inventory and migration decision log**, not an instruction to move every package. File counts are tracked Git tree blobs directly beneath each immediate `internal/` subtree (including fixtures and documentation); Go/test counts include nested packages. Recompute against a fresh baseline before making closure claims. The objective is understandable ownership and dependency direction, not a directory-count target.

| Existing subtree | Files | Go files | Go tests | Intended owner / disposition |
| --- | ---: | ---: | ---: | --- |
| `acceptedstate` | 22 | 22 | 12 | `orchestration/state`; preserve revision/pin/storage authority and schema |
| `cli` | 172 | 168 | 74 | `cli`; retain commands vs operations vs presentation split |
| `config` | 12 | 9 | 6 | `config`; configured intent and precedence, not accepted-state authority |
| `filestate` | 8 | 8 | 4 | Retain private file protocol pending consumer and secret-owner audit |
| `firewall` | 17 | 16 | 8 | `host/net/firewall`; no host-policy behavior change during move |
| `gateway` | 72 | 71 | 37 | Split reusable host network logic from concrete gateway plugin behavior |
| `gatewaycontrol` | 6 | 6 | 1 | Split public provider hooks from concrete gateway/provider adapters |
| `herdrclient` | 2 | 2 | 1 | `plugins/herdr`, coordinated with plugin vertical slice |
| `incus` | 7 | 7 | 3 | `incus`; reusable Incus access and lifecycle operations |
| `managementnetwork` | 2 | 2 | 1 | Split policy/host capability from orchestrated Incus network plans |
| `mcpschema` | 9 | 7 | 4 | Audit contract vs execution/grant policy before migration |
| `modelregistry` | 2 | 2 | 1 | Keep distinct model registry semantics; audit public declarations |
| `oauthgateway` | 2 | 2 | 1 | Retain until protocol ownership/callers are audited |
| `observability` | 10 | 10 | 5 | `observability`; status/reporting adapters |
| `orchestration` | 70 | 65 | 40 | `orchestration`; planning/drivers/resources and state coordination |
| `plugins` | 17 | 17 | 8 | `plugins`; internal composition/registry only; concrete plugins at root |
| `providerregistry` | 2 | 2 | 1 | Distinct provider declaration/registry semantics |
| `responsescontract` | 2 | 2 | 1 | Retain protocol boundary unless consumer audit supports merge |
| `statevolume` | 6 | 6 | 3 | `incus` volume primitives, **not** accepted-state persistence |
| `testenv` | 39 | 39 | 19 | Retain focused execution subsystem; consider `testing/testenv` |
| `validation` | 11 | 11 | 5 | Retain evidence semantics; consider `testing/validation` |
| `workload` | 4 | 4 | 2 | Generic assembly in orchestration; product behavior in plugins |

**Baseline:** 22 immediate subtrees, 494 tracked blobs, 478 Go files, 237 Go test files. These counts include nested subpackages; fixtures are not Go files.

## Direction and migration sequencing

1. `pkg/` publishes intentionally small contracts only; concrete plugins cannot import `internal/` and generic core cannot import concrete plugins. Application composition owns registration.
2. `cli/` coordinates typed requests and formats results, without privileged infrastructure algorithms. Preserve existing CLI extraction and import rules in [plugin-oriented core](plugin-oriented-core.md) and `internal/cli/ARCHITECTURE.md`.
3. `host/net` owns host firewall/routing policy. `incus` owns reusable Incus access, resources and volume primitives. `orchestration` coordinates their effects. Do not move management-network planning into host policy just because it describes a network.
4. `orchestration/state` coordinates configured/accepted/current state with explicit authority. A new package path cannot grant privilege or change persistence, journal, fencing, or accepted-revision behavior.
5. `testing` evidence and runtime `observability` are separate. Keep private-file, provider, MCP and response protocols distinct until their consumer graphs justify consolidation.

### Candidate stacks

- **A1: host network ownership.** Move `firewall` to `host/net/firewall` with its tests and fixtures, update imports atomically, preserve policy and fail-closed behavior. Do not silently rewrite gateway domain logic.
- **A2: reusable Incus ownership.** Audit `statevolume` consumers and move volume lifecycle primitives only when a coherent owner and absence of cycles are established.
- **A3: management network boundary.** Separate policy/configured network identity from orchestrated plan execution only after dependencies are measured; no new authority grants.
- **B: state/supporting domains.** Independently inventory state write paths, persistence contracts and reverse imports before `acceptedstate` moves.
- **C: product integration.** Coordinate Herdr/gateway/workload migration with #803–#806; avoid duplicate plugin extraction.

### Per-PR verification

- Record changed packages and their direct/reverse imports (e.g. `go list -deps -json ./internal/...` or equivalent). Reject cycles, and explain any retained specialist package.
- Move colocated unit tests and fixtures with implementation; run affected package tests, `go test ./...`, relevant Nix/test gates, and documentation/import checks in an isolated local worktree.
- Preserve CLI output/flags, configuration, accepted-state storage, Incus project and ownership checks, host network security policy, credentials and evidence classifications.
- Treat move-only PRs as semantic no-ops; review any behavior changes separately and explicitly. No effects or validation results are claimed by this inventory document.

## Open audits

The inventory is complete at the subtree/count level, **not** at the per-package import-graph level. Before each move, enumerate all production and test importers, transitive dependencies, security-sensitive interfaces, and potential cycles. Update this decision log when the move is implemented or rejected. No package should be moved solely to make the table shorter.


## Follow-up boundary disposition (2026-11-21)

Snapshot: `main` `025d3de411a6f0504fdb7297909c5d51bc8b3c44` before the accompanying #799 host
firewalld query-executor extraction. This section supersedes the original
2026-11-15 layout table as a **current structural status**, not a new
runtime or Incus security acceptance statement.

| Reusable responsibility | Current owner on this snapshot | Evidence / continued owner |
| --- | --- | --- |
| Host firewall policy, backend evidence and privileged mutations | `internal/host/net/firewall` | #873; #905 and #964 complete the separately privileged CLI egress boundary |
| Incus state-volume and claimed-image lifecycle | `internal/incus/statevolume`, `internal/incus/images` | #874, #943, #955; remaining #904 review is independent |
| Accepted-state revisions, same-UID offline store and journal | `internal/orchestration/state` | #886; project service authentication, CAS, recovery and writer fencing remain #695/#696/#704 |
| Management-network orchestration and plans | `internal/orchestration/managementnetwork` | #875/#884, not host firewall ownership |
| Disposable VM worker and validation evidence | `internal/testing/testenv`, `internal/testing/validation` | #948/#949 and #973's recovery of changes missing from `main` |
| Plugin generator and application composition | `internal/plugins/build`, `internal/plugins/composition` | #974/#983; #975's merge alone did not put its composition relocation on `main` |

The only shared **host command transport** used by the gateway's legacy
read-only firewalld query helper now belongs in
`host/net/firewall.RunFirewalld`; `gateway.SystemFirewalld` is a
compatibility delegate preserving its injected call signature and
`--get-zone-of-interface` exit-code-2 normalization. The executor
does not choose a zone, grant privileges or approve any activation.
The owning domain's tests exercise the same error/refusal cases.
`TestHostFirewallImportDirection` guards against new imports of CLI,
gateway/gatewaycontrol or concrete product plugin packages into the
host firewall domain.

**Explicitly retained product-owned / follow-up work:**
- `internal/gateway` retains *gateway-specific* Incus egress bridge
  plans, DHCP transition, deployment credentials and related firewalld
  **read-only audit policy**. These concern a concrete gateway; do not
  copy them into generic host firewall merely because they mention a
  firewall. The broader gateway container/plugin split belongs to
  #805/#806.
- `internal/cli/operations/network` retains Cobra-independent
  formatting, request-to-domain wiring and gateway-specific egress
  discovery. #905/#964 moved the privileged transaction to host/net;
  remaining adapter cleanup and command-test ownership are #725/#902/#903,
  not an unresolved host policy authority grant.
- CLI gateway asset-store action ownership remains #906.
  Product lifecycle and accepted-state service activation retain #704
  and resource-owner/permission gates; a package path is not an
  authorization principal.

**Final validation still required before marking #799 completed:** fresh
`go test ./...`, the host firewall/domain import checks, applicable
`just check all`/Nix regressions and required PR CI on the integrated
head. This document is a source and dependency-boundary review; it does
not assert new manual NixOS, Incus or live-provider acceptance.
