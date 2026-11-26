---
name: pr-stack-reconciliation
description: Reconcile stacked pull requests after parent merges, forward-integrate main, update PR bases and descriptions, diagnose CI, and prepare open stacks for review.
---

# PR stack reconciliation

Use this skill when asked to forward-integrate (FI) `main` through one or more
stacked pull requests, especially after a parent PR has merged. This skill
coordinates Git branches, worktrees, CI and PR metadata; it does not authorize
merging PRs, rewriting branch history, or bypassing validation prerequisites.

## Before acting

1. Read the repository's `AGENTS.md`, its engineering contract, the relevant
   testing guidance, and the current PR workflow rules. Treat repository
   instructions and explicit user constraints as authoritative.
2. Inspect `git status`, current branch/worktrees, `origin/main`, and GitHub PR
   state. Preserve all pre-existing user changes. Do not repurpose or remove
   unknown worktrees or scratch content.
3. Verify `.tmp/` is ignored by the repository's effective Git ignore rules
   (`git check-ignore`). Use `.tmp/aginctus-work/` for isolated worktrees and
   large temporary command output. Do not request broad `/tmp` access. If the
   directory is not safely ignored, stop before placing scratch data there and
   report the blocker; do not add ignore rules without authorization.
4. Confirm current branch heads, PR base branches, merge status, and stack
   dependencies from GitHub metadata and commit ancestry. Do not infer a stack
   solely from branch names or PR numbers. Record the intended sequence before
   modifying branches.

## Reconcile merged stacks

1. For each reported merged parent, identify the immediate open child whose
   base was that parent (or the equivalent verified dependency). Process each
   independent stack in its own isolated worktree. Use one subagent per stack
   only when subagents are available and explicitly permitted; handle at most
   three stacks concurrently. Keep each subagent scoped to its assigned stack
   and have it report branches, changes, validation and blockers.
2. Starting at the immediate child, retarget its PR to `main`, then forward-
   integrate the latest `main` into the child branch using the repository's
   established FI method (a merge of `main` into the topic branch, unless the
   project defines another non-rewriting method). Resolve conflicts only when
   the intended result is clear from the PR and surrounding code. Commit
   conflict resolutions as additive changes and push normally.
3. Continue in dependency order through descendants. For each descendant,
   preserve its correct unmerged parent as the PR base, forward-integrate the
   newly advanced parent using the project's stack procedure, resolve clear
   integration conflicts, and push without rewriting history. Do not retarget a
   descendant to `main` until its own parent has merged.
4. Never rebase, amend, force-push, force-with-lease, or otherwise rewrite PR
   history. Never merge a PR. If a requested operation requires rewriting
   history or conflicts with branch protection, stop and ask for direction.
5. Inspect relevant CI checks and logs for each affected PR. Fix failures
   demonstrably caused by FI when the correction is clear and within scope;
   otherwise preserve the failure and report the failing check, evidence and
   blocker. Do not hide, disable, or weaken checks to obtain a green result.
6. Ensure each affected PR description has an accurate **fenced extended
   squash-merge commit message** ready to copy for the eventual merge. Keep it
   aligned with the final diff, and include the repository-required full
   validation instructions and current validation snapshot.

## Review other open stacks

After the merged-parent stacks are complete, inspect the remaining open PRs for
additional stacks and identify their actual dependency order. Run local checks
appropriate to each PR using documented repository commands. Scope validation
to the affected changes, but do not omit required checks needed to prepare the
whole PR for review.

Respect each check's documented prerequisites and evidence tier. For isolated
VM coverage, use the appropriate repository [testenv setup](../../../docs/testing/testenv/README.md)
and follow the [VM harness procedure](../../../docs/testing/testenv/vm-harness.md)
to run the selected scenario in its intended isolated environment; do not
substitute direct execution on the coding host. Use a dedicated testenv worker
only under its separate operator authorization and worker contract. Never
bypass isolation controls, alter host security settings, use sudo to evade an
isolation failure, or report a skipped/blocked test as passed. If no suitable
testenv is available, report that check as blocked or not run, give the concrete
reason, and identify the appropriate testenv setup or operator follow-up. Do
not execute live providers, credentialed operations, destructive integration
tests, or host-mutating checks without the required explicit authorization and
disposable environment.

Update PR descriptions to reflect the **current known validation state**:
confirmed passes, failures, pending checks, blocked checks and checks not run.
Remove superseded validation claims. Include full validation instructions for
non-documentation PRs and an accurate fenced extended squash-merge commit
message on every PR being prepared, following `AGENTS.md`.

## Completion report

Report results per stack and PR, including:

- verified stack order and each PR's base before and after;
- FI performed and branches pushed;
- conflicts or CI failures resolved, with remaining failures and evidence;
- local checks actually run and their results, including skipped/blocked tests;
- PR-description updates, especially the extended commit message;
- merge-review readiness and any remaining owner action.

Do not claim a PR is ready when required checks, unresolved conflicts, or
material blockers remain. Do not merge any PR. Keep command output likely to be
large in files under `.tmp/aginctus-work/` and inspect only relevant excerpts.
