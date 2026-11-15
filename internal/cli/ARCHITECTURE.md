# CLI package architecture

The Cobra registration migration (#642) and subsequent application-operation
extraction (#725) established separate syntax, composition and execution
boundaries. This document describes the post-extraction layout, not an
implementation roadmap. Final audit/closeout is tracked in [#755](https://github.com/blevinn/aginctus/issues/755).

## Package ownership

| Package | Owns | Must not own |
| --- | --- | --- |
| `internal/cli` | Fresh CLI root construction, config loading, typed capability wiring, compatibility entry points, exit and output contracts | New reusable Incus resource algorithms or privileged policy implementations |
| `internal/cli/commands/<family>` | Cobra leaf trees, flags, help, syntax/confirmation validation, presentation | Direct OS/Incus access, import of root/operations, global Cobra command trees |
| `internal/cli/operations/<family>` | CLI application actions, bounded requests/results, injected side effects and diagnostics | Cobra commands, imports of root or registration packages, duplicate domain ownership rules |
| `internal/{incus,firewall,gateway,workload,...}` | Reusable resource ownership, reconciliation and domain behavior | Cobra flags or CLI exit-code rendering |

The intended dependency direction is:

```text
cmd/aginctus
    |
    v
internal/cli (composition + compatibility adapters)
    |                                |
    v                                v
internal/cli/commands/*       internal/cli/operations/*
(Cobra syntax)                 (CLI application behavior)
                                     |
                                     v
                                internal/<domain>/*
```

Commands receive typed callbacks and stateful store capabilities from root
composition. Registration must not import the application-operation packages
directly. Domain packages must not depend on the CLI.

## Verified inventory snapshot

At `main` commit `61ddc39` (2026-11-15, after #854–#860 merged)
`internal/cli` contained **25 direct production Go files** and
**70 direct Go test files**. The `operations/` subtree contained
46 production Go files and 12 test files. These figures describe the
snapshot; they are not immutable architectural acceptance thresholds.
Subsequent PRs under #755 add focused operation tests and architecture tests.

The 25 root production files have explicit composition/compatibility roles:

| Family | Files | Why they remain at root |
| --- | --- | --- |
| Cobra registration | `commands_aggregate.go`, `commands_basic.go`, `commands_gateway.go`, `commands_herdr.go`, `commands_images.go`, `commands_network.go`, `commands_operator.go`, `commands_testenv.go`, `commands_workload.go` | Assemble a fresh command tree from injected handlers |
| Gateway | `gateway_declarations.go`, `gateway_siwc.go`, `gateway_status.go`, `workload_gateway_provision.go` | Connect domain stores/guest transport and preserve test seams |
| Herdr | `herdr_wiring.go` | Bind guest execution, trust and transition operations |
| Images | `images_wiring.go`, `images_mutation_wiring.go` | Preserve separate ambient read/update and exact-project create-only mutations |
| Workload and instance observation | `instance_status_wiring.go`, `workload_lifecycle.go`, `workload_scoped_wiring.go` | Bind native mutation and separate scoped/ambient observation contracts |
| Network | `network_firewall_egress.go`, `network_status.go` | Inject host/Incus observations and privileged firewall capabilities |
| Operator | `operator_wiring.go` | Bind host/systemd/filesystem evidence to fail-closed operator operations |
| Entry and supporting adapters | `run.go`, `testenv.go`, `version.go` | Stable entry points, testenv runner seams and build provenance |

Most root test files exercise public CLI contracts or inject root-side
capabilities; moving them solely to reduce the file count would weaken
observability of syntax, exit status, I/O prevention and privilege behavior.
Operation-level pure tests have been colocated incrementally.

The original approximately **10–15 total root Go files** target was an
aspiration. It is not an appropriate closeout proxy while independently
readable command wiring and root integration tests are warranted. Evaluate
individual remaining adapters by ownership and auditability, not by the
count of files alone.

## Dependency, authorization and testing contracts

1. Command registration builds Cobra trees afresh per invocation; no
   package-level `cobra.Command` or `init()` registration is permitted.
   Commands may not import OS process/filesystem capabilities directly.
   File access and mutations must flow through injected application/domain
   operations.
2. `operations/*` cannot import `internal/cli`, `commands/*`, or Cobra.
   These rules are tested by `architecture_test.go` and
   `architecture_guards_test.go`.
3. Parsing `--help`, `version` and invalid syntax must not load
   configuration, inspect Incus, start a test VM or mutate host resources.
   Root command syntax/help matrix tests and no-I/O negative tests preserve
   this contract.
4. Never conflate legacy ambient/default-project CLI flows with explicit
   project-scoped operations. The latter require exact project identity,
   ownership and appropriate confirmation/lease/fingerprint checks.
5. Operator-only `aginctus-testenv worker` commands must remain separate
   from public `aginctus testenv` and MCP.
6. Metadata claims (image and state ownership, installed package identity,
   systemd observations) are evidence, not write authorization. Denied,
   malformed or incomplete evidence must fail closed.

## Remaining exceptions and closeout

The gateway asset command family still uses closures bound to an injected
`gatewaycontrol.Store` for provider/model/MCP/catalog actions. Registration
does not open this store; it is obtained only after a parsed command reaches
its leaf handler. However, these action closures still mix syntax and
domain-store orchestration; fully moving them into typed operations is
separate remaining architecture debt under #755. The gateway asset
configuration file reader moves into `operations/gateway` in the final
architecture gate stack, retaining no-follow, regular-file and size checks.

Some root-level mutable function seams are intentionally retained while
CLI negative and integration tests depend on them. Remove a seam only when
its test owner and invocation boundary are migrated together.

Before closing #755/#725, review the exceptions and the final root inventory,
validate CLI syntax/help and negative security cases, and run the prescribed
Go, static, Nix and hermetic gates. **Passing PR CI does not substitute for
the human-led NixOS host acceptance exercise**; report the latter separately
under the release acceptance plan. Never imply that an unperformed manual
host run has passed.

All refactor PRs should use isolated local worktrees whenever a local
checkout is available, avoid rebases/force pushes in shared stacks and write
only to `blevinn` repositories. PR descriptions include fenced extended
squash-merge commit messages containing change descriptions rather than
validation or risk notes.
