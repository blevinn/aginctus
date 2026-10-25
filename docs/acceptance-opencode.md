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
