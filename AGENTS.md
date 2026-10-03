# AGENTS.md

Repository-wide instructions for automated coding agents working on Aginctus.

These rules apply throughout the repository unless a more specific `AGENTS.md` adds stricter local instructions.

## Workflow

- Make substantive changes on a topic branch and propose them through a pull request.
- Do not push substantive changes directly to `main` or another protected branch.
- Never merge a pull request. A human performs every merge.
- Do not force-push, use `--force`, use `--force-with-lease`, rebase, or otherwise rewrite topic-branch history unless a human explicitly instructs you to do so for that branch.
- Prefer additive commits when addressing review feedback or correcting earlier work.
- Keep changes scoped to the requested task; avoid unrelated cleanup.

## Pull requests

PR descriptions should cover:

- purpose;
- important design decisions;
- validation actually performed;
- known limitations or follow-up work;
- security-relevant implications.

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
