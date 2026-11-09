# OpenCode acceptance: private evidence and attribution

This is the **operator-only evidence companion** to the [staged acceptance decision path](README.md). Select the lowest authorized stage before reading or executing any example below. Reports, preflights and spending-log correlation do not authorize host changes or paid inference; genuine provider calls require separate explicit permission. Preserve run ownership and keep credentials, gateway bearer keys and private reports outside Git or agent-writable directories.

## Optional private, runner-owned acceptance report

The existing `scripts/acceptance/opencode.sh` can now write a small **local**
report if the operator explicitly supplies `AGINCTUS_ACCEPTANCE_REPORT_ROOT`.
Create that directory beforehand, **outside the repository and any
agent-writable workspace**, with the owner of the trusted runner and mode 0700:

```sh
install -d -m 0700 "$HOME/.local/state/aginctus/acceptance-results"
export AGINCTUS_ACCEPTANCE_REPORT_ROOT="$HOME/.local/state/aginctus/acceptance-results"
# Then invoke the EXISTING operator-reviewed mock or live acceptance workflow.
# Example: with the already reviewed adapter and live opt-in selected:
# bash scripts/acceptance/opencode.sh live
```

This opt-in **does not** launch acceptance on its own. Live execution still
requires `AGINCTUS_ACCEPTANCE_LIVE=I_UNDERSTAND` and all existing reviewed
adapter hooks and approvals; `just check ci` does not execute this path.
An unsafe or absent evidence directory fails before any acceptance phase.

Each run writes a new private
`<report-root>/aginctus-acceptance.<run-identity>/result.json` (0700 run
directory, 0600 record), with fixed IDs, run identity, UTC time and exact
host-runner outcomes for the fixture edit and `go test`. The report also
records whether the runner's **stop/close commands** returned successfully.
A failing stop or close makes the harness fail, even if the fixture
test passed; command success is **not** independent proof of cleanup.

On every reached harness exit path, the host runner removes its own disposable
local root, checks that the root path is absent (including dangling symlinks),
and records `local_workspace_removed: pass|fail`. Failed removal returns a
nonzero exit and cannot become a successful local check in the optional
read-only viewer or aggregate coverage report. Older local records lacking
this observation remain readable, with the removal check marked `not_run`.

This observation covers **only the host's own disposable directory**. It does
not inspect or prove deletion of remote Herdr workspaces, Incus instances,
storage volumes, active agents or any other resources; those remain
`cleanup: not_verified`. Diagnostics from `rm` are suppressed to avoid
leaking guest-writable paths.

Reports always label native agent/tool evidence, real gateway-side Responses
completion, credential/egress enforcement, and owned-resource cleanup as
`not_run` / `not_verified`. Thus a `mock` or even a `live` harness
success results in an **incomplete** acceptance report, never
`complete_acceptance: true`. A future, separately reviewed runner
observation under [#521](https://github.com/blevinn/aginctus/issues/521)
must provide genuine independent evidence for those dimensions before
[#522](https://github.com/blevinn/aginctus/issues/522) may present them as
verified. Do not ingest agent-written JSON or gateway nonce markers as proof.
This local workflow requires no cryptographic signing service.

### Inspect one existing private run (read-only)

After a separately authorized acceptance execution writes a record, select its
exact `aginctus-acceptance.<8-character-suffix>` run ID. These commands only
read an owner-private record from the configured directory and **never**
launch an agent, invoke Incus or contact a model provider:

```sh
# AGINCTUS_ACCEPTANCE_REPORT_ROOT remains set as above.
nix develop -c just check acceptance-report <run-id>
nix develop -c just check acceptance-report-json <run-id>
```

The viewer validates the run ID, the root/run/file ownership and permissions,
no-follow path resolution, a maximum 16 KiB record, the fixed local-result
schema, and consistency between observed exit codes and reported outcomes.
Text mode prints host-observed fixture/test and stop/close **command** statuses;
JSON mode emits one compact object with the same bounded information. No raw
child output, absolute filesystem paths, credentials or provider payloads are
included. A missing, unsafe or malformed record produces a bounded error
instead of reinterpreting the file as a pass.

Both viewer commands intentionally return nonzero, even if all local checks
passed: this record **cannot** by itself prove OpenCode tool execution, real
Responses inference, credential/egress enforcement or independent resource
removal. Those remain `not_verified`, and
`complete_acceptance` stays `false`. An untrusted agent's ability to
write the operator's report directory would invalidate this trust boundary;
keep the directory restricted to a separate trusted operator principal.

### Operator-only, non-inference gateway authorization checks (#521)

The existing `scripts/gateway/authenticated-inference-smoke.py` now has a
**separate `--auth-only` mode**, requiring explicit operator selection.
It reuses the existing private gateway endpoint and owner-only secret-file
checks and performs only:

- `GET /health/liveliness` against the reviewed private LiteLLM endpoint.
- `GET /v1/models` **without** a key and with a randomly generated invalid
  key, both required to return 401/403.
- `GET /v1/models` using the operator-selected *workload* key, required to
  return exactly the selected model alias and no other visible aliases.
  The workload key must differ from the protected gateway master key.

Run a dry run before explicitly choosing the non-inference authorization
probe. These values come from a separately reviewed pilot configuration,
not from defaults guessed by this document:

```sh
nix develop -c python3 scripts/gateway/authenticated-inference-smoke.py \
  --endpoint "$GATEWAY_PRIVATE_ENDPOINT" \
  --gateway-id "$GATEWAY_ID" \
  --alias "$GATEWAY_MODEL_ALIAS" \
  --workload-key-file "$PRIVATE_WORKLOAD_KEY_FILE"

# Same options with --auth-only for an explicitly authorized, non-billable
# host-to-gateway HTTP check. Do NOT add --run (billable inference).
```

The mode makes **no** `/v1/responses` request, launches no workload or agent,
does not allocate or revoke keys, and neither probes guest network routes nor
changes firewall policy. Successful output is bounded, versioned JSON
(`profile: gateway_authorization`, `gateway_authorization: pass`);
errors return nonzero and fixed `status: error` without secrets or raw
gateway contents. It keeps `upstream_inference`, `agent_tools`, and
`credential_egress` explicitly `not_verified` and
`complete_acceptance: false`.

This proves only the **observed model-list authorization controls** at the
private gateway, from the operator's host. It does not establish credential
absence inside a guest, negative guest egress enforcement, real upstream
inference, or completion of the native OpenCode acceptance run. The original
`--run` paid Responses smoke remains separately gated and still performs
these same checks before making any model request. Normal CI uses only
mocked HTTP fixtures and never invokes `--auth-only` or `--run` against
a live gateway.

### Operator-only gateway spend-log correlation probe (preparatory #521)

`scripts/gateway/inspect_responses_spend.py` can inspect **one previously
generated** LiteLLM spend-log record through the private gateway's
administrative read-only `GET /spend/logs?request_id=` endpoint. The command
does not submit a prompt, launch an agent, create a key or cause a new model
request. It is **not** part of the default acceptance harness or CI.

The operator must independently obtain the exact request ID, native OpenCode
session ID, expected acceptance run ID, model alias and already-provisioned
workload-key file. The tool requires a protected gateway administrative key
in the existing `XDG_STATE_HOME/aginctus/gateway/<gateway-id>/secrets.json`
state file and a reviewed private IPv4 gateway endpoint. Supply all selectors
explicitly; no gateway traffic occurs unless `--inspect` or
`--diagnose` is present:

```sh
# Set exact values after inspecting the reviewed pilot; no defaults inferred.
# Dry run first: reads no secret files and makes no HTTP request.
nix develop -c python3 scripts/gateway/inspect_responses_spend.py \
  --endpoint "$GATEWAY_PRIVATE_ENDPOINT" --gateway-id "$GATEWAY_ID" \
  --request-id "$GATEWAY_REQUEST_ID" \
  --run-id "$ACCEPTANCE_RUN_ID" \
  --session-id "$NATIVE_OPENCODE_SESSION_ID" \
  --model-alias "$GATEWAY_MODEL_ALIAS" \
  --workload-key-file "$PRIVATE_WORKLOAD_KEY_FILE"

# Append --diagnose ONLY for an operator-approved redacted field check.
# Append --inspect ONLY when exact correlation is expected.
# The two options are mutually exclusive.
```

#### Diagnosing missing spend-log correlation (no acceptance promotion)

The `--diagnose` option performs the same read-only, administrator-authenticated
single-request lookup as `--inspect`, but **never issues a correlated-pass
verdict**. Its fixed `gateway_spend_diagnostic` JSON labels each of six
expected database columns as `match`, `missing` or `mismatch`:

`request_id`, `session_id`, `end_user`, `model_group`,
`api_key` (checked against the selected workload-key hash), and
`call_type`. A missing/ambiguous record, malformed response, unsafe key
or transport failure yields bounded `error` with all checks `not_run`.
The output never contains a request value, session ID, run ID, key hash,
key path, response body, prompt, provider header or raw exception.

The separate `spend_log_status` may report `success_label`,
`failure_label` or `unrecognized`, derived from the log's own
status column. **Neither status label is independent proof** of provider
terminal completion. Even when every column matches, the diagnostic
remains `status: incomplete`, `gateway_log_correlated: not_verified`,
`upstream_inference: not_verified`, `agent_tool_execution: not_verified`
and `complete_acceptance: false`.

Source review of pinned LiteLLM v1.103.0 establishes that its
`LiteLLM_SpendLogs.session_id` may come from a trace-ID fallback rather
than the native OpenCode session, and `end_user` can be attributed from
key/customer metadata. The acceptance prompt's run nonce alone is **not**
a demonstrated mechanism for populating those columns. These field-level
hints help identify the missing instrumentation but do not authenticate
the native OpenCode session or bind a request to the acceptance run.
Normal CI calls only mocked HTTP transports; the operator alone chooses
whether to query the deployed private gateway.

The inspector requires **exactly one** bounded spend-log entry matching
request ID, `session_id`, `end_user` (the acceptance run ID),
`model_group`, the SHA-256 of the selected workload key, and the native
`aresponses` call type. Missing columns, wrong identity, ambiguous rows,
malformed JSON, unsafe keys or unavailable administrative logging fail closed.
This deliberately requires gateway-side values rather than trusting a
`gateway-observed:<run-id>` string from the agent or adapter. It does **not**
infer success from an unvalidated `status` field.

**Important prerequisite:** the current acceptance harness does not yet
guarantee that its native OpenCode traffic emits all of these fields in
the pinned LiteLLM spend-log schema. The probe may therefore return
`error` for legitimate but insufficiently instrumented runs. Review actual
pinned-version fields with an authorized pilot before integrating it as
automated evidence. Even a fully matching entry proves only that the gateway
logged a correlatable request. It does **not** independently establish
Responses terminal completion, upstream/provider success, agent tool
execution, network isolation or cleanup. Output is limited to fixed labels
(`gateway_log_correlated`, `upstream_inference: not_verified` and related
unverified claims); no response body, prompts, tokens, headers, file paths,
raw logs or exception text are printed. The main acceptance report and all
four `live/*` coverage rows remain unverified/not-run.

### Native OpenCode session attribution at the pinned gateway (#521)

The Go pilot's Nix-packaged OpenCode `chat.headers` hook now copies the
**same native `input.sessionID`** into three headers on the approved
`aginctus-go-pilot` route only:

- `x-opencode-session`: consumed by the existing strict gateway forwarding
  policy and selectively passed upstream.
- `x-opencode-session-id`: recognized by the pinned LiteLLM v1.103.0
  vendor-session parser.
- `x-litellm-session-id`: an explicit pinned LiteLLM v1.103.0
  trace/session input, which takes precedence over generic session headers.

The header hook creates **no** session ID. A missing/invalid native ID
fails before sending. The gateway's selective Go callback still validates
the native session headers and client User-Agent, rejects disagreements,
and now rejects a conflicting `x-litellm-trace-id` or
`x-litellm-session-id` that would override spend-log attribution.
The existing upstream allowlist remains only `x-opencode-session` and
`User-Agent`; LiteLLM session/trace headers are **not forwarded** to the
provider. This requires deploying the refreshed NixOS OpenCode workload
image *and* the callback-only pinned gateway policy.

This change provides a **syntactically consistent, client-supplied
session attribution path** through the pinned gateway. It does **not**
authenticate that client as the live OpenCode agent or bind any traffic to
the disposable acceptance `run_id`: that run identifier is currently
included only in the task prompt, and must be mapped through an
independently trusted host/gateway source before release acceptance.
The pinned spend-log writer may still need `missing_session_id` policy
review and real readback to establish whether `session_id` is correctly
persisted for this specific route. The operator may use the previously
introduced redacted `--diagnose` command on an *authorized, existing*
request to inspect that field, without counting matching values or log
success as provider completion evidence.

No live deployment or provider request occurs during ordinary CI. The
synthetic native-hook behavior can be validated locally, separately
from the pinned Python policy tests:

```sh
nix shell nixpkgs#nodejs --command node --test guest/opencode/test_go_session_plugin.mjs
nix develop -c python3 -m unittest discover -s scripts/gateway -p 'test_opencode_go_forwarding.py' -v
```


## Evidence does not replace acceptance

The [acceptance decision path](README.md) remains authoritative for the exact stage, approval, expected side effects, cleanup and verification limitations. A green synthetic test or a correlated usage record does not alone establish the model-directed coding and egress-denial properties claimed by a release gate.
