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
