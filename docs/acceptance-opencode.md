# OpenCode acceptance: operator decision path (#124)

This is the **entry point** for validating Aginctus → Herdr → OpenCode → private LiteLLM. Choose the least privileged check that answers your question; progress to the next stage only after reviewing its separate prerequisites and authorization. The original #124 tracking issue is closed, but that administrative state, a green mock, a gateway health response, and a read-only preflight **do not establish successful authenticated provider inference**. The [#122 provider-inference gate](https://github.com/blevinn/aginctus/issues/122), [#257 egress gate](https://github.com/blevinn/aginctus/issues/257), and [#118 release sign-off](https://github.com/blevinn/aginctus/issues/118) remain authoritative.

## Choose the evidence you need

| Stage | Start here | Permitted effects | What a pass establishes / does not establish |
| --- | --- | --- | --- |
| 1. Offline orchestration | [Deterministic fixture and tests](#1-offline-mocks-and-orchestrator) | Local temporary fixture and mocked adapter only | Validates the harness and its failure/cleanup paths; **not** Herdr, Incus, model behavior, or real gateway traffic |
| 2. Read-only pilot inspection | [Incus topology](acceptance-opencode-preflight.md), then [guest gateway readiness](acceptance-opencode-inference.md) | Reads selected Incus and guest metadata/configuration; no model calls or infrastructure changes | Checks network settings, running containers, workload key-file metadata and Responses endpoint syntax; **not** authentication, reachability, egress denial or provider success |
| 3. Explicitly opted-in Herdr lifecycle | [Owned workspace smoke](acceptance-opencode-herdr.md) | Creates and closes run-owned remote workspaces; separate opt-ins for agent startup and prompting | Tests specific lifecycle calls in the managed Herdr context; **not** fixture editing, tool execution, gateway correlation or upstream inference |
| 4. Separately authorized live end-to-end | [Nine-phase live dispatcher](acceptance-opencode-live.md) | May start an agent, mutate a run-owned fixture and incur provider charges; reviewed hooks required | Only independently verified agent/tool activity, isolation and gateway-side upstream response evidence can support live sign-off |

The disposable, synthetic-provider [VM harness workstream](https://github.com/blevinn/aginctus/issues/425) is a **separate, in-progress** validation layer. Even a future successful real-VM run against a synthetic provider must not be reported as a real billable provider or autonomous model-coding success.

## 1. Offline mocks and orchestrator

From the repository root, with Bash, Go 1.22+ and the fixture's Unix tools available:

```sh
bash scripts/acceptance/opencode-test.sh
bash scripts/acceptance/opencode-live-adapter-test.sh
```

The [harness](../scripts/acceptance/opencode.sh) creates a uniquely named temporary Go module with a deliberately wrong `Add` implementation and a failing test. Its mock adapter *simulates* an edit and gateway evidence. The harness checks the resulting source and independently runs `go test ./...`; it also verifies injected failures, unauthorized live rejection and owned cleanup. Expect `PASS: deterministic acceptance orchestration and cleanup` for the first test; an intentionally printed failure inside a negative test is not itself a failed test suite. Nonzero exit means the invoked check failed.

These tests need neither deployed Aginctus/Herdr/Incus nor an upstream model. They are safe for hermetic CI; they prove **no live inference**. The broader offline repository suite is `nix develop -c just check ci`, which keeps live and disposable acceptance opt-ins disabled.

## 2. Read-only pilot inspection

Use a separately provisioned pilot **only with read-only permissions** and the required tooling. Perform these checks in order, without interpreting one pass as proof of the next:

1. Run the [host/Incus topology preflight](acceptance-opencode-preflight.md) after confirming that `aginctus` and `incus` are available. It checks the configured default-project bridge and managed running containers, **not** the complete NIC attachments, firewall effectiveness or authenticated Herdr connection.
2. Run the [OpenCode inference-runtime readiness preflight](acceptance-opencode-inference.md) with a reviewed workload name, if overridden. It reads nonsecret `gateway.json`, agent-owned key **metadata** and key readability; it never reads key bytes or sends a model request.
3. Compare the declared network and model topology with the [gateway/workload integration contract](workloads/opencode.md). Verify gateway ownership, direct-provider egress denial, scoped LiteLLM authorization and upstream access **independently** before any higher-impact action.

A private `http://<IPv4>:4000/v1` syntax check, or `/health/liveliness` returning HTTP 200, does not prove that the LiteLLM address is owned by the managed gateway, that an OpenCode request is authorized, or that Responses/provider inference succeeded. A failed preflight is a hold, not permission to fix the host firewall, inspect secrets or reissue credentials.

### Concise, machine-readable stage 2 preflights

The following are **operator-initiated**, read-only alternatives to the
individual stage 2 preflight scripts. They use the same underlying checks,
discard subprocess output by default, and report exactly one pass/fail/error
result in the same general envelope as the hermetic and VM reporters:

```sh
nix develop -c just check preflight-report pilot-topology
nix develop -c just check preflight-report opencode-runtime
nix develop -c just check preflight-report-json pilot-topology
nix develop -c just check preflight-report-json opencode-runtime
```

These commands **contact the configured pilot's Incus daemon** to inspect
read-only resource and guest metadata. Use them only after explicitly
selecting the target pilot and reviewing the access available to
`aginctus` and `incus`. They are **not** run by `just check ci`,
a bare `just check`, or any VM test. The runtime preflight uses
`AGINCTUS_ACCEPTANCE_WORKLOAD` if supplied (otherwise `aginctus-dev`),
and records only a syntactically valid workload identifier. Inherited
integration, live and Herdr smoke opt-ins are disabled for both checks.

JSON contains a `pilot_preflight` profile, selected check ID, observed
exit status, bounded duration and explicit
`upstream_inference: not_verified`, `gateway_authorization: not_verified`,
and `egress_policy: not_verified` statements. Launcher/timeout failures
become `error`; failed checks exit nonzero. Neither result establishes
pilot gateway reachability, model authorization, actual upstream inference,
live-provider acceptance or host/network isolation. No logs, secrets,
endpoint values or exception messages are copied into shared JSON; to diagnose
a failure, deliberately rerun the native script on an authorized pilot.

## 3. Opt-in real Herdr lifecycle, not yet the full agent task

Read the [owned Herdr lifecycle runbook](acceptance-opencode-herdr.md) and validate the **saved machine profile within the managed Herdr client**, run-owned guest directory, guest staging helper and scoped cleanup before invoking its smoke. The baseline `AGINCTUS_ACCEPTANCE_HERDR_SMOKE=I_UNDERSTAND` creates, inspects and closes two owned remote workspaces. It does not start an agent or infer.

Separate `AGINCTUS_ACCEPTANCE_HERDR_AGENT_SMOKE=I_UNDERSTAND` and `AGINCTUS_ACCEPTANCE_HERDR_AGENT_PROMPT=I_UNDERSTAND` authorizations are needed for startup and prompting respectively. Do not enable these merely because workspace-only smoke passes: first verify gateway model selection, workload credential restrictions, private routing, and that inference may be used. Workspace/agent operations are not a substitute for a tool trace or gateway-side evidence.

## 4. Operator-authorized live nine-phase acceptance

Only after the independent [#122 operator hold](https://github.com/blevinn/aginctus/issues/122) has been satisfied, use the [live adapter runbook](acceptance-opencode-live.md). The live harness requires `AGINCTUS_ACCEPTANCE_LIVE=I_UNDERSTAND`, an executable `AGINCTUS_ACCEPTANCE_ADAPTER`, and nine reviewed executable hooks under the absolute `AGINCTUS_ACCEPTANCE_LIVE_HOOKS` directory:

```text
preflight → open → start → prompt → wait → credential-check → gateway-evidence
                                             ↘ stop → close (scoped cleanup)
```

The run's prompt asks the **actual OpenCode agent** to fix the temporary `Add` function and execute tests. A satisfactory operator record must independently corroborate all of the following: owned Herdr workspace and agent lifecycle; actual agent read/edit/test tool trace and expected fixture diff; local `go test ./...` success; absence of upstream provider credentials and direct-provider egress in the guest; and **gateway-side**, run-ID-correlated successful upstream model response with the expected Responses behavior. Do not accept guest-printed markers, a synthetic provider response, only an HTTP health check, or a changed file without a tool trace as substitutes.

The live dispatcher itself supplies **no** gateway audit, private credential discovery, validated fixture transport or missing hooks. Incomplete/no-op hooks must fail closed. It passes the same opaque `RUN_ID` to each hook; it never passes secrets as command arguments. The harness suppresses hook stdout/stderr, except for exact gateway marker verification from its private temporary output file. These markers are interface assertions, not cryptographic proof.

## Failure, cleanup and evidence handoff

For any stage, stop on nonzero results. The local harness creates and removes **only its own temporary directory**; after a successful `open`, it attempts `close`, and after attempting `start` it also attempts `stop`, even following a later failure. An `open` that partially created remote resources but failed before acknowledgment needs hook-side cleanup or operator inspection. Failed stop/close operations require inspecting the **matching owned run ID** before manually removing anything. Never delete unrelated Herdr workspaces, Incus guests, volumes or existing host paths.

Store any operator evidence in a separate, access-controlled, **redacted** record: run ID, UTC time, software versions, selected owned resource IDs, observed phase outcomes, independent tool/test traces, gateway-side request/response correlation and safe pass/fail statements for direct-egress and credential isolation. Do **not** collect raw request headers, provider replies, OAuth materials, keys or unredacted Incus/config dumps in logs or PR comments.

Record real pilot evidence on the [#118 release checklist](https://github.com/blevinn/aginctus/issues/118) and its live gateway/egress gates (#122/#257). A closed documentation issue or a passing hermetic check does not authorize live deployment, billable inference or release sign-off.

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
