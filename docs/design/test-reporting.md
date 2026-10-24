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

## Migration and extension

- Add optional structured failed-case details through adapters for Go's JSON test
  events, Python `unittest`, and shell/Nix, without exposing arbitrary stdout.
- Represent `skipped` / `not-run` when future selectors or early aborts are
  introduced. Never count unexecuted checks as passes.
- Map existing VM harness synthetic `result.json` records and explicit live
  acceptance results into a common **presentation** model without changing their
  independently secured evidence stores, or quietly invoking those tests.
- Evolve `schema_version` for incompatible changes, and use stable suite IDs.

VM evidence semantics remain unchanged. Future schema adapters must not
interpret a green hermetic run as evidence of live inference.
