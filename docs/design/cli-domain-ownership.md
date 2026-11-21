# Domain-owned CLI migration

**Status:** Design for review. Tracks [#902](https://github.com/blevinn/aginctus/issues/902),
[#725](https://github.com/blevinn/aginctus/issues/725),
[#755](https://github.com/blevinn/aginctus/issues/755), and the
[plugin-oriented core design](plugin-oriented-core.md) (#797).

**Baseline:** `main` at `61ddc39` (2026-11-15). The direct CLI root has 25
production and 70 test Go files; `internal/cli/operations` has 46 production
and 12 test Go files, and `internal/cli/commands` has 11 registration files
and **zero colocated test files**. These numbers describe remaining work, not
architecture acceptance thresholds.

## Decision

The existing `operations/<family>` packages are **migration staging areas**,
not permanent subsystem boundaries. Their functions must be classified by what
they do, rather than which CLI command originally called them. A typed resource
operation or authorization policy belongs to the component controlling that
resource. CLI-specific syntax and result presentation can remain in a thin
adapter. A caller outside the CLI must not need a Cobra command, CLI exit code,
`io.Writer`, or a CLI package import to invoke reusable policy.

A vertical slice moves implementation, tests, and authorization evidence
together. Do **not** consolidate unrelated files just to reduce file count.

## Layer and dependency contract

```text
cmd/aginctus
  -> internal/cli                # application assembly, global flags, status
      -> internal/cli/commands/* # per-family Cobra command trees
      -> domain use-case owners  # typed requests/results and side-effect seams
          -> internal/incus/...  # Incus identity, ownership, image primitives
          -> internal/host/...   # effective host firewall/OS evidence and mutation
          -> internal/orchestration/... # native plan, apply, convergence
          -> internal/gatewaycontrol/... # provider/model/MCP/catalog model & store
          -> plugin owners       # Herdr/OpenCode/gateway-specific behavior
```

`internal/cli/commands/*` must not import the CLI root, reusable use-case
implementations, or host I/O. The CLI root may supply capabilities to command
handlers. A domain API must not accept CLI `args []string`, emit a CLI exit
code, or print its primary result to a `stdout/stderr` pair; errors and typed
reports should be returned for CLI rendering. Do not introduce a new umbrella
`internal/services`, `internal/operations`, or `assets` catch-all.

A temporary `internal/cli/operations/<family>` adapter is acceptable
**only** for request decoding from legacy strings, CLI-only orchestration
sequencing and report/exit rendering. Its remaining code must be small and
comprehensible, with an explicit reason to remain.

## File-level disposition of current operations

Each row enumerates the current source files; splitting the *functions* is
expected. Destinations are proposed owners, not indiscriminate file renames.

| Current `operations/` family | Files | Domain owner / CLI remainder |
| --- | --- | --- |
| `aggregate` | `apply.go`, `remove.go`, `status.go` | Plan construction and cross-resource execution: `internal/orchestration`. CLI keeps plan file reading, requested render format, and exit mapping. Must not resurrect retired compose/apply drivers as the default runtime. |
| `config` | `config.go` | Retain CLI: effective-config `show/get` presentation is a command use case. Core config parsing stays `internal/config`. |
| `doctor` | `address.go`, `doctor.go` | Reusable host/Incus observation and safe HTTPS-address policy: host/Incus owners; CLI keeps diagnostics and optional `--fix` confirmation. |
| `gateway` | `credentials.go`, `egress_actions.go`, `lifecycle.go`, `models.go`, `query.go`, `registry.go`, `responses.go`, `siwc_actions.go`, `status.go` | Gateway lifecycle/credentials: `internal/gateway`; provider, model, MCP/catalog actions: `internal/gatewaycontrol` and registries; network egress: host network/gateway owner; SIWC: provider plugin; Responses wire validation: `internal/responsescontract`; typed gateway status: gateway domain. CLI keeps presentation and legacy error/exit compatibility. |
| `herdr` | `console.go`, `herdr.go`, `trust.go` | Herdr identity, key verification, pinning, machine registration: concrete Herdr capability/plugin per #803, building on `internal/herdrclient` without reimplementing trusted-host rules. CLI retains command argument parsing and TTY bridge only. Coordinate with active plugin work. |
| `images` | `helpers.go`, `legacy_update.go`, `owned_import.go`, `owner_claim.go`, `read_inventory.go`, `replace_plan.go`, `scoped_import.go`, `scoped_inventory.go`, `scoped_lease.go` | Image data, ownership-claim classification, fingerprint validation, project inventory, cooperative lease, create-only mutations: a cohesive `internal/incus/images` owner if review confirms no cycles. CLI retains legacy syntax, explicit user confirmation, and status rendering. The v0.1 ambient alias replacement workflow remains a distinct capability. |
| `network` | `egress_reconcile.go`, `firewall_policy.go`, `firewall_reconcile.go`, `network.go`, `status.go` | Firewall backend/zone evidence and mutation in `internal/host/net/firewall`; management-network resource behavior with its owning Incus/native component; status rendering stays CLI. Egress grant scope must not widen. |
| `operator` | `authority.go`, `confinement.go`, `package_audit.go`, `preflight.go`, `project_commands.go`, `project_policy.go`, `state_audit.go`, `units_audit.go` | Credential/enrollment proof: `internal/controllerauthority`; project policy: `internal/incus`; UID, package and systemd evidence: cohesive `internal/host` owners; state inspection: owning accepted-state/host-state component. CLI retains only root-mode request/exit/output. Do not combine root and user authorities. |
| `testenv` | `testenv.go` | Test VM execution and policy in `internal/testing/testenv`, with CLI dispatch limited to offline/VM selection. Dedicated operator-only worker commands stay separate. |
| `workload` | `gateway_provision.go`, `legacy_status.go`, `scoped_lifecycle.go`, `scoped_status.go`, `workload.go` | Generic workload identity/ownership in Incus and workload domain; planning/convergence in `internal/orchestration/native`; gateway-specific provisioning in gateway/plugin owner. CLI retains legacy command syntax and status rendering. Coordinate with #799 and accepted-state instead of creating a parallel planner. |

## Security invariants that move with behavior

1. **No adopted authority:** image labels and owner claims are evidence, not
   permission to overwrite unmarked/foreign resources. Preserve explicit
   project selection and create-only semantics.
2. **Two independent compatibility paths:** legacy ambient image alias
   update/rollback is not silently converted to an exact-project API, and
   scoped mutators never fall back to the Incus ambient context.
3. **Host authority:** firewall writes require independently verified root
   authority, effective backend ownership, DROP target, an explicitly owned
   bridge and narrow permitted chains. Failure clears only owned exceptions
   (where authorized) and fails closed.
4. **Control-plane separation:** root NixOS and Home Manager user authority
   must not be inferred from owner-marked state files. Revocation/enrollment
   and permission proofs remain independent of plugin inventory.
5. **Safety before parsing side effects:** malformed syntax, help, version,
   dry-run and read-only previews do not authorize mutation. Never invoke
   host/Incus capabilities from Cobra registration or `init()`.
6. **Race and recovery:** pinned fingerprint re-observation, cooperative
   lease contention, atomic create and post-write verification continue to
   be independently tested across extraction boundaries.
7. **No silent format drift:** public help, emitted JSON fields, exit codes,
   typed result data, error redaction and no-clobber guarantees must hold.

## Migration sequencing

- **Design and accounting:** review the 46-file mapping, command test
  ownership and import contract. File-level disposition may be revised by
  reviewers, but an owner is required for every moved function.
- **Parallel independent foundations:** move *pure* Incus image claim/fingerprint
  policy with its direct tests; move firewall backend/zone classification and
  its direct tests into the host owner; add direct Cobra `Register` tests in
  command packages without importing CLI root. These have no shared files.
- **Typed service extraction:** separately migrate image scoped leases and
  operations, firewall mutations and operator host authority; preserve direct
  typed results and failure semantics. Avoid simultaneous changes to active
  native, accepted-state or plugin implementation branches.
- **Plugin/native coordination:** Herdr, OpenCode and gateway topology work
  follows #797/#803/#804/#805, while workload native planning follows #799.
  A domain owner should not be moved twice in independent stacks.
- **Closeout:** only after every operation has a domain/adapter disposition,
  every root test has an owner, representative negative-security tests are
  colocated, and Go/Nix/hermetic CI plus manual NixOS acceptance are explicitly
  accounted for (manual acceptance is separate evidence).

## Review gates for each migration PR

A PR should state (1) what behavior changed owner, (2) its old and new
dependency direction, (3) test moves/additions and retained CLI integration
coverage, (4) security denials that must remain, (5) compatibility behavior,
and (6) CI/host validation evidence. A pure source relocation without an
auditable new owner does **not** close a migration issue.

Work from isolated Git worktrees where local checkout is available. In a
stack, retarget each immediate successor to `main` **before** merging
`main` forward, and do not rebase or force-push shared branches. All
repository writes are confined to `blevinn/aginctus`.
