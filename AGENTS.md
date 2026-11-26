# AGENTS.md

Repository-wide instructions for automated coding agents working on Aginctus.

These rules apply throughout the repository unless a more specific `AGENTS.md` adds stricter local instructions.

## Workflow

- Make substantive changes on a topic branch and propose them through a pull request.
- Do not push substantive changes directly to `main` or another protected branch.
- Never merge a pull request. A human performs every merge.
- Never force-push a PR branch (including with `--force` or `--force-with-lease`), rebase it, or otherwise rewrite its history unless the user explicitly requests that action for that branch.
- Prefer additive commits when addressing review feedback or correcting earlier work.
- Keep changes scoped to the requested task; avoid unrelated cleanup.
- For feature work, prefer bite-sized, reviewable PRs, generally changing fewer than 10 files. Treat 10 files as a ceiling to avoid where possible, not a target or absolute limit.
- Balance small PRs with functional completeness: each feature PR should strive to deliver a coherent, verifiable increment, not merely introduce orphaned code on `main`. Split larger features into independently useful vertical slices where practical.

## Engineering guardrails

Follow the [engineering contract](docs/development/engineering-contract.md) and accepted ADRs. For every substantive change:

- Use an isolated worktree for local work; do not disrupt parallel worktrees. Never write to repositories or external resources outside the authorized `blevinn/aginctus` repository.
- Identify the canonical contract, existing implementation and tracking issue before adding a new abstraction or parallel path. Prefer completing and replacing existing paths over adding scaffolding.
- Follow [ADR 0007](docs/adr/0007-executable-boundaries.md): one production executable (`aginctus`), one developer executable (`aginctus-dev`). Use subcommands for new roles; do not introduce another persistent executable without a reviewed architectural exception. Keep production dependencies and authority isolated from development tooling.
- Preserve independent host authority, explicit environment acceptance, verified scope and ownership, pinned revision and writer exclusion. Configuration origin, names and labels do not grant authority. Never activate a privileged write path merely because a fixture passes.
- Use typed JSON-first intent, native reconciliation and static plugin composition for new lifecycle work. Do not expand legacy drivers or introduce speculative compatibility layers.
- Keep credentials out of accepted state, Nix store, readable resource configuration and diagnostics. Require explicit approval for host mutations, privileged integration and live providers.
- Parse once, execute once, render once. Prefer explicit effect capabilities, typed outcomes and domain-owned contracts; avoid new mutable global transport hooks.
- Distinguish applied, running, ready and converged. Report unknown or partial outcomes accurately. Test denial and failure paths as well as success.
- Before removing code, verify indirect consumers, including Nix, generated/embedded assets, build tags and public APIs. Record unresolved reachability instead of guessing.
- Record actual validation and its limits. Source inspection, hermetic checks, disposable VM tests and operator acceptance are distinct evidence tiers.
- Run relevant disposable VM tests when they provide useful validation and the execution environment supports them. This policy supplies standing operator authorization for repository-defined, isolated VM tests (including NixOS/QEMU tests and VM-local Incus); no separate per-run approval is required. Follow the [testing guidance](docs/testing/README.md) and the applicable harness prerequisites and isolation requirements. Explicit test opt-ins remain required where the harness expects them. This permission does not authorize host Incus administration, privileged host mutations, persistent worker administration, pilot deployments, live credentials or provider calls, or bypassing sandbox or MCP permission controls. Report unavailable or failed VM validation accurately rather than substituting hermetic evidence.
- For stacked PRs, after a human merges a parent, retarget its child to the correct base before merging the new base down; propagate forward integrations through descendants without rewriting history. Revalidate affected changes.

## Pull requests

PR descriptions should cover:

- purpose;
- important design decisions;
- validation actually performed and current validation state;
- known limitations or follow-up work;
- security-relevant implications.

Every PR description must also include:

- **Full validation instructions** for non-documentation-only PRs in a fenced code block, with the commands and any prerequisites or manual checks needed to validate the entire PR. Update this block as the PR evolves; do not replace it with only incremental or partial checks. Documentation-only PRs are exempt. More granular, partial validation steps may be supplied in chat during multi-step investigations.
- **An extended squash-merge commit message** in a fenced code block, ready to copy and paste for the final merge. Keep it accurate as changes are added or revised.

When working interactively with a human, also provide the next actionable validation steps directly in chat, in addition to maintaining the complete validation instructions and current state in the PR description. Keep chat steps focused on what the human should run or inspect next; update them as validation proceeds.

Open PRs as ready for review by default, not as drafts. Use draft status only when the PR is genuinely not ready for review or consideration for merging; mark it ready promptly once that blocker is resolved.

If a user supplies validation output, update the PR description to reflect the **current known validation state**, distinguishing confirmed passes, outstanding failures, pending checks, and checks not personally run. Keep this as a current snapshot, not a running history of earlier validation attempts; remove stale results when superseded. Do not claim validation passed merely because instructions were provided.

Apply appropriate GitHub labels and milestones to issues and PRs where possible, following the repository's existing taxonomy. Use release milestones for version targets rather than recreating version labels; avoid assigning an unconfirmed target. Update metadata when scope or targeting changes.

Call out changes involving sandboxing, network controls, credentials, secrets, MCP permissions, model access, authentication, authorization, or host capabilities explicitly.

## Documentation maintenance

- Every current/operator-facing Markdown page must be **reachable** from
  `docs/README.md` through a purpose-specific section index, usually in two
  clicks. The offline Markdown checker follows local Markdown links recursively,
  detects broken targets/fragments, and rejects orphaned active pages. Do not
  defeat it with a flat dump of links in the root index.
- Keep `docs/adr/`, `docs/design/`, `docs/internal/`,
  `docs/milestones/`, `docs/releases/` and `docs/history/` explicitly
  separated as decisions, proposals, internal notes or historical evidence;
  these are exempt from active-operator discoverability, not from link safety.
- Update **the canonical topic owner** instead of copying a side-effecting
  workflow: `docs/operations/gateway/README.md` owns gateway stages,
  `docs/testing/README.md` chooses evidence tiers,
  `docs/testing/acceptance/README.md` owns OpenCode acceptance,
  `docs/reference/cli.md` owns command effects, and
  `docs/reference/configuration.md` owns config precedence and module effects.
- Keep `docs/getting-started.md`, `docs/reference/cli.md`, and the Herdr
  operator runbook's literal `aginctus` examples aligned with generated
  `aginctus help` usage. Run `just check docs-examples` (or the legacy
  `just check-docs-examples` alias); it checks syntax shapes without executing
  operations. Use CLI help as authoritative syntax; do not copy a flag table.
- When adding gateway or acceptance pages, keep the gateway workflow at
  `docs/operations/gateway/README.md` and acceptance path at
  `docs/testing/acceptance/README.md` in sync. Verify new `operator ...` or
  `images ...` subcommands in both `docs/reference/cli.md` and
  `docs/reference/configuration.md` with consistent opt-in/future status.
- Run `just check markdown` (or `just check-markdown-links`) after moves. Preserve
  host approval, secret handling, project scope, failure/rollback, and live-model
  opt-in gates. Proposed features, old pilot observations, and green hermetic
  checks are not proof of current production behavior.

## Pre-stability compatibility

- Follow the [pre-stability compatibility policy](docs/development/compatibility.md).
  Until a separately approved stability commitment lands, do not presume
  backward compatibility, historical state migration, or preservation of pilot
  deployments. Prefer clean reinstantiation and direct, coherent changes over
  speculative legacy shims or migration machinery.
- Do not treat disposable pilot deployments as permission to destroy resources,
  bypass authorization or ownership checks, or weaken security, correctness,
  current-state data integrity, reproducibility, or external API conformance.
- Document consequential breaks and reinstantiation requirements, but do not
  introduce historical-version compatibility tests or adapters without a
  concrete, approved need.

## Working rules

- Read the relevant repository documentation and nearby code before making non-trivial changes.
- Prefer small, reversible designs when implementation details are unsettled.
- Add or update tests when behavior changes.
- Do not claim tests or checks passed unless they were actually run.
- Do not commit credentials, tokens, private keys, or other secrets.
- Do not weaken security controls merely to make tests or demos easier without clearly documenting and justifying the tradeoff.
- If a requested change conflicts with these rules, surface the conflict instead of silently violating them.

Project vision, architecture, scope, and integration choices belong in `README.md` and other project documentation rather than being duplicated here.
