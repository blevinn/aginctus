# v0.2 CLI architecture — operator acceptance and signoff

**Status: NOT EXECUTED.** This is a versioned, CLI-specific acceptance
checklist for #725/#755/#903 and root capability cleanup #1021. It is
not evidence of a completed live host test or of the wider #580
fresh-host deployment / coding / cleanup-and-repeat gate.

**Host:** the operator's actual NixOS development host. Destructive
Incus, image, firewall and egress checks belong inside a *dedicated
disposable NixOS VM with its own Incus daemon and virtual disk*.
Do not execute privileged mutation on the live development host merely
because a command is included in this runbook.

## Before starting: freeze and record evidence

- [ ] Check out reviewed `main` (or a precisely identified release
  candidate) and save `git rev-parse HEAD`, `git status --short`,
  `nixos-version`, `go version`, Incus version and the Nix closure
  revision. Record operator, UTC start time and host/VM identifiers.
- [ ] Verify the project and network names in the disposable VM.
  Do not infer an Incus project from the ambient/default context.
  Identify the VM's dedicated storage, bridged network and cleanup
  ownership before any write. Confirm backups/snapshots and an
  independent console are available.
- [ ] Review expected resource names, any commands that can mutate,
  and their authorization/confirmation gates. Never use real provider
  tokens or live model calls for CLI-only acceptance.
- [ ] Save evidence in an operator-owned redacted local directory.
  Record command, expected/actual exit status, expected/actual output
  fields, before/after resource inventory, and incident IDs. Do not
  paste secrets, sensitive paths, key material or host-specific
  identifiers into an issue or public CI log.

## Gate A — hermetic and static acceptance (development host)

From the pinned repo working tree:

```sh
git rev-parse HEAD
nix --quiet develop --no-update-lock-file -c just check-all
go test ./internal/cli/... ./cmd/... ./internal/testing/testenv/...
go vet ./internal/cli/... ./cmd/...
git status --short
```

- [ ] All applicable hermetic, architecture-boundary, root test
  function/ownership, output and help checks pass at the recorded SHA.
- [ ] Reconcile the current `cli-root-mutable-seams.md` exceptions
  against #1021. An unreviewed mutable authority hook is a blocker,
  not an implicit green status.
- [ ] Review CI for the same source revision. A green PR head alone
  is not a checked post-merge `main`.

## Gate B — nonmutating CLI experience (development host)

Use the built `aginctus` executable from the pinned Nix closure
or development environment. Capture exit codes **separately** from
human output for each command.

```sh
aginctus --help
aginctus help gateway
aginctus help --full gateway
aginctus version
aginctus gateway provider --help
aginctus gateway egress --help
aginctus operator --help
aginctus images --help
```

- [ ] Default root and family help each stay within the 35-line ceiling,
  include useful discovery hints, and never load configuration or
  contact Incus.
- [ ] Full help is discoverable without hiding necessary flags.
  Invalid arguments and missing confirmations yield documented
  nonzero status and usage, **without host/Incus I/O**.
- [ ] Supported structured read commands emit parseable JSON/YAML
  with typed scope/identifiers; `--output` on unsupported commands
  fails clearly. Compare representative output/exit codes with the
  pinned acceptance fixtures.
- [ ] Verify public `aginctus-testenv` CLI cannot invoke any
  private worker/admin command without its independent authorization.

## Gate C0 — programmatic testenv VM admission

Use the public `aginctus testenv vm` interface, built from the pinned
source, to discover, plan and run a fixed disposable scenario. **Do not
manually create a host Unix account, project, storage pool or bridge merely
to start CLI acceptance.** Do not run a host-local `incus` mutation in place
of a guest operation.

For the initial `native-instance` scenario, the runner calls
`nix build --no-link --no-update-lock-file --print-out-paths
.#vm-harness-native-instance`, which executes the fixture as a NixOS VM
test derivation. This is **not** one of the networked scenarios that run
a Python NixOS driver directly in the caller's host context. Before
accepting a run on a multipurpose development host:

- [ ] Confirm the repository and `flake.lock` are reviewed and pinned,
  the checkout is clean, and `nix config show` reports
  `true` (not `false` or `relaxed`). Confirm the Nix daemon's
  applicable sandbox setting if the client view differs.
- [ ] Use the current-source CLI: the installed system binary can be
  older than the accepted checkout. Do not launch from an agent-writable
  repository or delegate invocation to an untrusted coding agent.
- [ ] Review resource headroom and the fixture's guest-only Incus
  initialization. The Nix builder is a trusted build service; this
  does not establish an adversarial sandbox for unreviewed build scripts.
- [ ] Run the fixed `testenv vm test native-instance` command.
  Read its owner-only evidence via `just check vm-report-json <run-id>`.
  Distinguish derivation success from independent cleanup verification.
- [ ] Treat any run failure as FAIL and investigate before attempting a
  different execution mode. Do not use `vm-debug` or networked driver
  scenarios under a privileged pilot identity.

The scenario verifies a substantial native Incus instance contract, but
does not itself prove project-create, image-import collision/lease or
firewall mutation contracts. Those require subsequent appropriately
isolated scenarios. The fully untrusted-agent-facing supervisor is still
tracked separately under #428; don't claim the operator-driven batch
runner supplies it.

## Gate C — scoped Incus and image lifecycle (disposable VM only)

- [ ] In the VM, create a uniquely named, **explicitly operator-owned**
  restricted Incus project under the documented provision/ensure
  flow. Confirm the authenticated local daemon and the selected
  project from a separate readback; no ambient context switching.
- [ ] Run the read-only `aginctus operator project-plan --project
  <VM_TEST_PROJECT>` and the version-appropriate scoped image
  list/claim checks. Preserve exact status and redacted JSON.
- [ ] An invalid project name, `default`, foreign/unmarked alias,
  or missing project authorization must fail **before any write**.
- [ ] Run one allowed create-only image import with known locally
  built artifacts; verify alias/fingerprint/claim and independent
  re-read after import. Repeat the same alias and confirm no implicit
  reuse/adoption/replacement.
- [ ] Exercise lease contention across *separate processes*,
  release/retry and uncertain post-write response: no opportunistic
  rollback or trust in claim metadata as authorization.
- [ ] Document legacy v0.1 ambient image update separately; it
  must never silently become the selected-project create-only API.

## Gate D — host firewall and gateway egress (disposable VM only)

- [ ] Record firewall backend, bridge/project ownership evidence,
  active/drop zone, nftables owned child chain and routing state
  before any mutation.
- [ ] Root-denied, unexpected backend, wrong DROP target, ambiguous
  link, extra NIC, mixed routing table and foreign project/bridge
  cases fail closed without unauthorized host commands.
- [ ] With explicit operator authorization, reconcile the selected
  allowed runtime rules and independently inspect the actual ruleset.
  Do not flush base chains or execute global firewalld reload.
- [ ] Revoke/remediate only owned grants, verify unrelated firewall
  policy remains untouched, and test repeated reconciliation.
- [ ] Record that a successful planning/audit response is not proof
  of gateway connectivity or provider inference.

## Gate E — workload, gateway, credentials and testenv (VM/pilot as scoped)

- [ ] Verify the accepted/configured/observed state separation at
  the selected *project* scope, including multiple environments
  per Incus project and environment-owned gateways.
- [ ] Inspect gateway provider/model/MCP/catalog registration using
  local nonsecret fixtures. Confirm duplicate, invalid and
  credential-bearing declarations reject cleanly.
- [ ] Exercise gateway/workload lifecycle only for operator-owned,
  disposable resources, including restart/retry and negative
  authorization. Observe independently that image/project and
  credential isolation are preserved.
- [ ] Check private credential volume ownership, read-only
  observability and redaction without live provider login.
  Provider consent, real authentication and potentially billable
  inference remain separately authorized in the canonical staged
  acceptance documentation.
- [ ] Verify testenv worker isolation, cleanup and resource
  ownership. No reusable host-level worker privileges are inferred
  from a VM or hermetic test.

## Cleanup and replay

- [ ] Inventory resources created under this run ID; remove *only*
  those resources, including VM-owned Incus instances, networks,
  images, keys, volumes and firewall rules.
- [ ] Confirm foreign resources and unrelated host rules are intact.
  Re-run read-only observations to prove cleanup, then repeat the
  relevant disposable VM flow with a new run ID.
- [ ] Keep failed or skipped phases as **FAIL** or **NOT RUN**, not PASS.

## Results / handoff record

| Field | Operator-supplied value |
| --- | --- |
| Operator / review date | NOT RUN |
| Source revision and Nix closure | NOT RUN |
| Development host and VM identifiers (redacted) | NOT RUN |
| Hermetic and post-merge CI evidence | NOT RUN |
| Gate A: hermetic | NOT RUN |
| Gate B: CLI grammar/help/output | NOT RUN |
| Gate C: project/image scope | NOT RUN |
| Gate D: host firewall/egress | NOT RUN |
| Gate E: gateway/workload/testenv | NOT RUN |
| Owned cleanup and repeat | NOT RUN |
| Incidents / issue references | NONE RECORDED |
| Operator review and risk acceptance | NOT SIGNED |

## Closure policy

#903 can close after direct command tests, per-function owner records,
and unavoidable test-global exceptions have a reviewed disposition.
#902 can close when every residual `internal/cli/operations` function
is domain-owned, a legitimate CLI adapter, or explicitly deferred to
an appropriate native/plugin owner with an issue.

#755 and parent #725 require this CLI-specific acceptance record,
a green final merged `main`, and explicit acceptance of any remaining
#1021 exceptions. This does **not** require completing unrelated live
inference and full fresh-host repeatability under #580; failures
discovered by this CLI run must still be tracked and resolved.
