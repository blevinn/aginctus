# CI performance baseline and rollout policy

Related tracking: [CI modernization #705](https://github.com/blevinn/aginctus/issues/705).
This file is implementation and operator guidance, not evidence of a successful
host test or production acceptance.

## Measured baseline (2026-11-08)

GitHub Actions workflow `hermetic` currently serializes all eight safe
hermetic suites, a gateway parity build, a release packaging preview and
whitespace validation in one 45-minute job. Among the most recent 100
workflow runs examined: 44 successes, 10 failures, 42 cancellations, 4 in
progress. Successful workflow elapsed time had an approximately 18.2-minute
median and a 21.3-minute 90th percentile. Elapsed time includes scheduling;
it is not equal to billable runner execution.

Representative [run 42509516107](https://github.com/blevinn/aginctus/actions/runs/42509516107)
spent 449 seconds populating the Nix dev shell, 676 seconds executing the
hermetic suite aggregate, 10 seconds building the additional gateway parity
check, and 35 seconds packaging. Suite JSON reported Go 24 seconds, lint
50 seconds, Python 26 seconds, and Nix 573 seconds. Other successful runs
showed Nix suite times of 401 and 540 seconds; one showed 1170 seconds.
The Go module cache did not eliminate cold Nix shell setup.

[Run 42503881989](https://github.com/blevinn/aginctus/actions/runs/42503881989)
was terminated by a signal 15 / runner shutdown after about 35 minutes;
that evidence does *not* establish that the configured 45-minute timeout
fired. The old `cancel-in-progress` policy also superseded `main` pushes.

## CI trust and release requirements

- PR checks are read-only, unprivileged, and never opt in to Incus, KVM,
  live provider calls, billable acceptance, host mutation or cache publishing.
- NixOS/Home Manager authority boundaries and configuration security assertions
  must be built for relevant source changes. A classifier failure is not proof
  of non-applicability.
- Full flake checks and release/package checks remain independently observable
  for protected `main`, explicit dispatch and tagged releases.
- A skipped, cancelled, missing or invalid report must not be called a pass.
- The active [main ruleset](https://github.com/blevinn/aginctus/rules/24431697)
  requires the check name `hermetic validation (ubuntu)`. Preserve that
  check until an administrator intentionally migrates requirements.
- PR supersession may cancel only obsolete PR runs. Separate pushes to
  `main` and explicit dispatches must not cancel each other's evidence.

## Rollout and administrator actions

Stage 1: preserve the existing required check and baseline instrumentation.
Stage 2: introduce parallel Go and static checks and compare independent
outcomes against the legacy hermetic aggregate.
Stage 3: make the required check a fail-closed aggregation of fast and
targeted Nix checks; run comprehensive regression on `main` and on demand.
Stage 4: measure p50/p90, validate release packaging, and consider a
trusted binary cache only after owner approval of credentials and cost.

No ruleset changes, external cache credentials, hosted service, or new
privileged runner are required for stages 1-3. If GitHub ruleset status names
ever need changing, a repository administrator must first confirm the new
status reports successfully on representative PRs, update the ruleset,
and verify the next test PR cannot merge when that status fails. Revert
the ruleset change immediately if checks become missing or bypassable.

Review release signoff separately: post-merge checks cannot replace a
release tag validation and must not silently authorize publishing.

## Measurements for acceptance

Measure PR update to required-status completion (including queue time),
job execution time, setup/bootstrap time, each suite duration, cache
hit/miss, cancellation reason and number of runner-minutes consumed.
Target <=5-minute median and <=8-minute p90 for ordinary PRs, allowing
security-sensitive Nix changes a longer documented path. Report both
cold and warm runs; never use a single fastest run as a performance claim.

## Parallel baseline stage (#707)

The `go-fast` and `static-fast` jobs run independently of the existing
required `hermetic validation (ubuntu)` job during migration. Both use
fixed tools (SHA-pinned setup actions and pinned Python/linter versions).
Go modules are downloaded only during an explicit preparation step;
subsequent Go tests, vet and docs examples run with GOPROXY/GOSUMDB off.
These jobs do not use the Nix development shell and must not execute host,
Incus, KVM or provider acceptance paths. The old Nix-backed aggregate
continues to run and remains authoritative until #708/#709 migration.

The new jobs intentionally use regular GitHub step logs, not the machine JSON
schema. The existing `just check report-json` contract remains untouched.
Evaluate native-vs-pinned-tool diagnostic differences before removing the
legacy aggregate. Their failure must be treated as an actionable regression
rather than overriding the legacy check's successful result.

## Targeted Nix PR applicability (#708)

`scripts/checks/ci_select.py` compares the PR base commit to the full PR
merge checkout using a NUL-terminated Git diff with rename detection
disabled. Only Markdown below `docs/` and the root `README.md` can exempt
focused Nix security contract builds. Any other file, an empty diff,
invalid SHA, failed Git command or invalid UTF-8 path requires Nix (and an
invalid selector invocation independently fails). `changes` is always
included in the eventual required gate. The dedicated Nix job is skipped
only when the selector explicitly outputs `false`; 'skipped' is not a
security attestation or a successful Nix run. Complete flake checks remain
a separate post-merge/release obligation.

## Required status migration and comprehensive evidence (#709)

The existing `hermetic validation (ubuntu)` **job name** is retained for the
active ruleset. On PRs, it now runs as a fail-closed summary of the independent
Go, static, change-selection and (if required) targeted Nix jobs. It is never
skipped just because an upstream job fails, and an invalid selector output
fails the required gate. A docs-only PR must have a passing classifier and
an explicitly skipped Nix job. Changes to code/configuration/CI files must
show Nix PASS. Whitespace validation remains mandatory.

On pushes to `main`, weekly scheduled runs and explicit dispatch, the
`comprehensive hermetic regression (ubuntu)` job continues to run all eight
hermetic suites, gateway parity, a release packaging preview, and whitespace
validation. The full flake check itself already performs evaluation, so a
separate identical `--no-build` prepass was removed. Tag-triggered
`release.yml` remains unchanged and independently validates release sources,
packaged binaries and the draft release. Do not mistake passing PR checks
for release signoff, or main post-merge failures for harmless skips.

### Required operator review and rollback

1. Confirm #712, #713 and #714 have merged/validated in order, and compare
   the fast jobs against the previous eight-suite hermetic report.
2. On a real PR targeting `main`, confirm the required
   `hermetic validation (ubuntu)` context appears exactly once and fails if
   either Go/static job or required Nix job fails. Confirm a docs-only PR
   explicitly records the Nix skip and still runs all safe fast checks.
3. Confirm the existing [main ruleset](https://github.com/blevinn/aginctus/rules/24431697)
   still requires `hermetic validation (ubuntu)` with no missing check. It
   should not require a new job name until that job has reported reliably.
4. Confirm two `main` pushes produce two comprehensive jobs, neither of
   which cancels the other. Dispatch one comprehensive run manually if needed.
5. On CI regression, revert the workflow PR rather than simply removing the
   required status. Restore the original full hermetic PR gate while retaining
   the ruleset context. If a ruleset edit is later approved, revert it too
   before making code changes that remove a required check.

The repository administrator, not this code change, owns any eventual
ruleset edits and optional cache credentials. This implementation does not
write repository settings or inject secrets into untrusted PR jobs.


## Operational hardening and required checks (#711)

The core CI migration (#712–#715) has merged. As verified after #715,
the protected \`main\` branch's active
[ruleset 24431697](https://github.com/blevinn/aginctus/rules/24431697)
requires the **single** GitHub Actions check
\`hermetic validation (ubuntu)\`. The additive administrator change below
does not modify workflow code, credentials, bypass policy, or branch targets.

### Three required checks, with unchanged fail-closed Nix policy

A repository administrator should edit the existing ruleset under
**Settings → Rules → Rulesets → main**, keeping the existing check and
adding these two independently observed GitHub Actions **job** contexts:

| Exact check name | Role |
| --- | --- |
| \`hermetic validation (ubuntu)\` | Keep; fail-closed PR evidence aggregator |
| \`go tests and staticcheck (ubuntu)\` | Add; always-run PR Go verification |
| \`python shell and docs (ubuntu)\` | Add; always-run PR static and regression verification |

When available, pin the source of each required check to **GitHub Actions**
(the existing check uses integration ID \`15368\`). Preserve the current
target (default branch), active enforcement, loose/strict freshness setting,
review and squash-merge policy, non-fast-forward/deletion protections and
bypass permissions. Save the changes atomically. Do **not** replace or
disable the aggregate, or create a parallel ruleset.

Do **not** directly require \`targeted Nix security contracts (ubuntu)\`:
the job is conditionally skipped for genuinely docs-only PRs, and GitHub
can regard conditionally skipped jobs as satisfying requirements. The
required \`hermetic validation (ubuntu)\` aggregator checks the path
classifier's result and insists on targeted Nix **success** whenever the
classifier says Nix is required; invalid/missing classifier data fails.

All three required names must appear on ordinary PRs targeting \`main\`.
Their absence, failure, or cancellation must prevent merging. They are
PR-only jobs and are correctly skipped on a protected \`main\` **push**;
the comprehensive job, not the PR aggregate, runs after merge.

### Verification immediately after the ruleset edit

1. Read back the active ruleset; verify precisely the three names above,
   GitHub Actions expected source, unchanged target, enforcement, bypass
   and review settings. Record editor, UTC time and ruleset snapshot in
   [#711](https://github.com/blevinn/aginctus/issues/711).
2. On a current PR targeting \`main\`, inspect the checks and merge box.
   All three required contexts must be reported. A failure in either fast
   suite must block merge independently, and a required Nix failure must
   make the aggregate fail.
3. For a docs-only PR, require successful classification, both fast suites,
   and aggregate; Nix may be explicitly skipped. For source/config/CI
   changes, targeted Nix must be successful. Do not treat an unobserved
   security contract as a pass.
4. If any newly required check is missing, left \`Expected\`, or sourced
   from the wrong integration, **remove only the two added names**.
   Retain \`hermetic validation (ubuntu)\` and all other protections.
   Correct the workflow/status-name mismatch before trying again.
5. After merging a representative PR, confirm the independent
   \`comprehensive hermetic regression (ubuntu)\` finishes with full
   eight-suite Go/Python/Nix coverage, parity, packaging preview and
   whitespace checks. An unverified or failing comprehensive run blocks
   release signoff; PR gate success is not a substitute.

### Run failure and recovery policy

Every push to \`main\`, weekly schedule and explicit dispatch must produce
an independently addressable comprehensive run. A failed, cancelled or
timed-out comprehensive run requires an owner to inspect the failing step,
record its commit and run URL, and either fix the source, rerun an identified
transient infrastructure failure, or revert the offending merge. An abnormal
post-merge run is **not** ordinary PR cancellation and must not be silently
ignored. Escalate any unresolved failure before release tagging or approval.

Maintain ongoing failure notifications (at least one monitored channel
with an accountable owner and acknowledgement). Monitor the age of the
latest \`main\` comprehensive run, not just its green PR predecessor. The
GitHub Actions UI and notification subscriptions remain the operational
source of truth. Do not expand untrusted PR workflow permissions or add
external cache-publishing credentials to improve alerting.

Tag-triggered \`release.yml\` still performs its own strict source and
packaging validation, and will not publish on a failed validation job.
No new cache infrastructure is authorized as part of #711. Performance
baseline and cost sampling are separately tracked in
[#710](https://github.com/blevinn/aginctus/issues/710).
