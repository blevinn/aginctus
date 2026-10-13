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

If a user supplies validation output, update the PR description to reflect the **current known validation state**, distinguishing confirmed passes, outstanding failures, pending checks, and checks not personally run. Keep this as a current snapshot, not a running history of earlier validation attempts; remove stale results when superseded. Do not claim validation passed merely because instructions were provided.

Apply appropriate GitHub labels and milestones to issues and PRs where possible, following the repository's existing taxonomy. Use release milestones for version targets rather than recreating version labels; avoid assigning an unconfirmed target. Update metadata when scope or targeting changes.

Call out changes involving sandboxing, network controls, credentials, secrets, MCP permissions, model access, authentication, authorization, or host capabilities explicitly.

## Working rules

- Read the relevant repository documentation and nearby code before making non-trivial changes.
- Prefer small, reversible designs when implementation details are unsettled.
- Add or update tests when behavior changes.
- Do not claim tests or checks passed unless they were actually run.
- Do not commit credentials, tokens, private keys, or other secrets.
- Do not weaken security controls merely to make tests or demos easier without clearly documenting and justifying the tradeoff.
- If a requested change conflicts with these rules, surface the conflict instead of silently violating them.

Project vision, architecture, scope, and integration choices belong in `README.md` and other project documentation rather than being duplicated here.
