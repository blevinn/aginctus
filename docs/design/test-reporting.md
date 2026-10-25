# Test reporting contract (proposal and incremental implementation)

Tracking: [#491](https://github.com/blevinn/aginctus/issues/491). Related: [#347](https://github.com/blevinn/aginctus/issues/347).

## Objective

Present reproducible validation outcomes consistently to a human at a terminal,
to CI, and to an agent receiving pasted output. Preserve underlying test tools
and keep expensive/host-mutating checks explicitly opt-in.

## First increment: hermetic suite-level adapter

Run within `nix develop` at the repository root:

```sh
just check report
just check report-json
```

The Nix development-shell startup banner is sent to stderr, so
`nix develop -c just check report-json` retains JSON-only stdout.

Both commands execute the same explicitly allowlisted safe `just check` suites:
`go`, `lint`, `python`, `shell-tests`, `shell`, `markdown`, `docs-examples`, and
`nix`. The adapter runs all eight even when an earlier suite fails, so it reports
all observed failures rather than masking later checks. Each child runs with the
existing `just check ci` safety overrides: real Incus and live acceptance opt-ins
disabled, caller-supplied Go test flags cleared, offline Go module resolution,
and no automatic Go toolchain selection. Nix may still fetch pinned inputs.

Interactive output is one PASS/FAIL/ERROR line with a duration for each suite,
followed by counts. It does **not** print raw child output by default. A failing
suite prints a fixed diagnostic rerun command; run `just check <suite>` for native
diagnostics. `python3 scripts/checks/report.py --verbose` explicitly streams raw
local output, which may be sensitive and must not be pasted indiscriminately.

`just check report-json` writes **one JSON object on stdout**, even when a
suite fails; failure is also indicated by process exit status 1. The schema is:

```json
{"schema_version":1,"profile":"hermetic","status":"fail","duration_ms":1000,"summary":{"passed":7,"failed":1,"errors":0},"results":[{"id":"go","status":"pass","exit_code":0,"duration_ms":100,"rerun":"just check go"}]}
```

This example is schematic: it shows one result from a complete run; real
reports include every suite. Suite statuses are `pass`, `fail`, or `error`
(launch failure). Exit code is an integer for launched children and `null` for
a launcher error. Durations are wall-clock milliseconds, not performance
benchmarks. `status=pass` requires every allowlisted suite to pass. The report
does not claim VM, host acceptance, or upstream provider inference coverage.

The machine document intentionally contains **no subprocess output, exception
messages, environment variable values, provider traffic, or credentials**.
A failed test's exact native diagnostics require explicit rerun. Machine output
is suitable for pasting as a compact status snapshot, not as a substitute for
collecting approved diagnostic evidence. The adapter does not store raw logs.

## Standard validation and CI adoption

The second increment makes `just check ci`, `just check all`, and the legacy
`just check-all` alias show the same concise suite-level terminal report.
These commands report every hermetic suite outcome; they no longer stop at
the first failing *suite*. Commands within a failing suite still follow Just's
normal fail-fast semantics. To see detailed native diagnostics, explicitly run
`just check <suite>`. To copy a compact, versioned, agent-readable report, use
`just check report-json` and preserve its process exit status.

GitHub Actions executes `just check report-json` **once** and saves the output
in the runner's temporary directory as `check-ci.json`. The CI step summary
validates the schema, suite inventory, counts, and statuses before rendering
bounded, synthetic-only Markdown. Missing, invalid or partial JSON is labeled
unavailable, never reported as passed, and additionally fails CI if the
aggregate step had claimed success. No successful or failing child stdout or
stderr is stored as a CI artifact by this integration.

This still does not cover disposable VM, live Incus or provider acceptance
runs. The source checks and their safety/environment overrides are unchanged.

## Structured failed-test identifiers (Go and unittest)

For normal non-verbose aggregation, the reporter drains native output in
fixed-size chunks without retaining a raw subprocess log. The Go test recipe
emits native JSON test events only while the reporter sets the dedicated
AGINCTUS_CHECK_STRUCTURED opt-in; invoking the ordinary Go test recipe directly
still uses original Go output. The Python adapter recognizes the standard
FAIL and ERROR headers from unittest verbose mode.

When a failing suite has recognizable test events, an optional additive
`failed_cases` array contains at most **eight** objects per suite, each with
a restricted `id` and `kind` (fail or error). The optional
`cases_truncated: true` signals more distinct failures. The schema remains
version 1. No raw output or exception/traceback text enters JSON or the CI
summary. The summary validates identifiers before writing Markdown.
Case names are **best-effort, diagnostic hints** extracted from child output;
the suite exit code remains authoritative. Cases not recognized, including
Go vet/module errors and some Python import failures, still cause the suite
to fail without invented case details.

Go identifiers are restricted to Aginctus-module package paths and safe
test-name characters. The Python adapter accepts only simple unittest
class/method identifiers. Overlong lines, binary/non-UTF-8 content, unrecognized
formats and characters that might inject Markdown or shell syntax are discarded.
The parser bounds its line buffer and retained case list; no raw logs are
saved to disk or emitted in JSON. Operators needing a traceback or actual
assertion text must rerun the reported suite intentionally on a trusted machine.
This prevents automatically pasting possibly sensitive error messages to
agents or CI. Shell, lint, docs and Nix checks remain suite-level pending
purpose-built structured adapters.

## Read-only presentation of existing VM evidence (phase D1)

The explicitly selected `just check vm-report <run-id>` and
`just check vm-report-json <run-id>` commands inspect *only* the
already-written VM harness `.vm-harness-results/<run-id>/result.json`.
They never launch a VM or implicitly opt in to Nix builds, host Incus,
live provider access, or agent-host authority. They require an exact
24-character run ID rather than accepting arbitrary filesystem paths,
and check symlink/owner-only permissions before reading the private
16-KiB-bounded source. Errors emit generic, bounded output; raw records,
filesystem exceptions and unrecognized fields are never copied into
shared text/JSON.

The read-only adapter validates the VM evidence schema-version-1 fields,
scenario allowlist, run ID, status/exit code, UTC timestamp, limited
provenance and the independently owned `not_verified` claims. It maps
those records to an independently versioned reporting **profile** of
`disposable_vm`, with the familiar `schema_version`, `status`, `summary`
and `results` shape. It retains `verification_tier`,
`upstream_inference: not_verified` and `cleanup: not_verified` explicitly,
rather than conflating a successful Nix derivation with verified upstream
model inference or cleanup. No duration is invented because the source
does not record it. The original private evidence is left untouched.

This view is not ingested by the hermetic CI reporter, and v0.1 live
acceptance or cryptographic approval/attestation still require separate
work. Only the known VM harness synthetic evidence format is supported;
there is no generic free-form log importer.

## Explicit read-only pilot preflight presentation (phase D2)

An operator can intentionally invoke either of the existing stage-2
read-only pilot scripts with `just check preflight-report <check>` or
`just check preflight-report-json <check>`. The explicit check selector
must be `pilot-topology` or `opencode-runtime`; no custom executable
or free-form shell command is accepted. There is no default selector.
The commands never run through `just check ci`, start a VM, run
Herdr agents, authorize a live response or trigger a billable provider.

Unlike the VM evidence viewer, this command runs the **chosen preflight**
on the caller's explicitly selected and accessible pilot. It may read
Incus network/instance metadata or guest key-file **metadata**, not key
contents, using existing read-only scripts. It suppresses child output,
clears inherited live/Incus/Herdr smoke opt-ins, imposes a timeout and
never includes raw errors in the report. A missing launcher, timeout, or
invalid runtime workload identifier is an `error`, not a pass.

The report contains the conceptual v1 `status`, `summary`, and
`results` shape with `profile: pilot_preflight` and
`verification_tier: read_only_pilot_preflight`. It records only bounded
local elapsed time, the known check name, the validated target name (or
`configured_pilot`) and the child exit code. `upstream_inference`,
`gateway_authorization` and `egress_policy` always remain
`not_verified`, irrespective of the preflight result. These reports
are **not** independent gateway/egress evidence and are not release
acceptance attestations. No unapproved live acceptance format is
imported or inferred.

## Migration and extension

- Extend bounded structured failure detail to shell/Nix only when a trustworthy
  structured event source exists, without scraping arbitrary diagnostic text.
- Represent `skipped` / `not-run` when future selectors or early aborts are
  introduced. Never count unexecuted checks as passes.
- Keep operator-authorized live acceptance and upstream inference evidence
  outside the routine reporters. Consider a separately secured, versioned
  receipt format only after the live gate defines trustworthy provenance,
  sign-off and credential-redaction requirements.
- Evolve `schema_version` for incompatible changes, and use stable suite IDs.

VM evidence semantics remain unchanged. Future schema adapters must not
interpret a green hermetic run as evidence of live inference.
