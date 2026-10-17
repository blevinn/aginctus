# OpenCode v0.1 acceptance (#124)

## Status and trust boundaries

The deterministic test checks the acceptance **orchestrator**, not real inference, model-directed behavior, Incus isolation, or actual gateway traffic. Only a successful live pilot run may establish the full #118 vertical slice. Do not use synthetic mock evidence as release sign-off.

The harness lives in `scripts/acceptance/opencode.sh`. It provisions an isolated, randomly named temporary directory and a tiny Go module with an intentionally defective `Add` function and a failing unit test. It requests a source fix and test execution, verifies the fix and executes `go test ./...` itself, then requires adapter-provided gateway and credential assertions. It always attempts agent stop and workspace close before deleting its own temporary directory, including on errors. It does not delete Incus instances, user workspaces, volumes, or unrelated data.

## Deterministic validation

Requires Bash, Go (1.22+), GNU sed for the bundled mock and standard Unix tools. From the repo root:

```sh
bash scripts/acceptance/opencode-test.sh
```

The bundled adapter fakes model work and gateway evidence. Expected: a PASS for the mock run, a deliberately injected failure, a rejected unapproved live invocation, and a final `PASS: deterministic acceptance orchestration and cleanup`. Exit nonzero is failure.

This can run independently of #122, #123, OAuth, the pilot host, and #220. It can be incorporated in `just check` later without credentials; do not add a live invocation to default checks.

## Live pilot: deliberate opt-in

The live adapter, provided when #123 integrates OpenCode, must be an **executable** implementing this argv contract:

```text
ADAPTER preflight WORKSPACE RUN_ID
ADAPTER open WORKSPACE RUN_ID
ADAPTER start WORKSPACE RUN_ID
ADAPTER prompt WORKSPACE RUN_ID PROMPT
ADAPTER wait WORKSPACE RUN_ID
ADAPTER credential-check WORKSPACE RUN_ID
ADAPTER gateway-evidence WORKSPACE RUN_ID
ADAPTER stop WORKSPACE RUN_ID
ADAPTER close WORKSPACE RUN_ID
```

Every phase must return nonzero on an unmet assertion or timeout. `open` creates only a new Herdr workspace owned by RUN_ID, using the supplied directory. `start` launches its OpenCode agent through Herdr with gateway-only model configuration owned by #123. `prompt` submits the provided task verbatim. `wait` must observe completion and actual test execution via agent/tool traces, not infer it from modified files. `stop` and `close` target **only** that run; both should be idempotent. The harness never passes secrets as command arguments.

`preflight` must fail closed unless the NixOS deployment, managed Herdr client/control-plane connection, managed OpenCode workload, private Incus management network, and real Aginctus LiteLLM gateway are healthy. Explicitly verify IPv4 NAT and routing are disabled on the management bridge and that OpenCode cannot directly access an upstream provider. Do not alter firewall rules; #220 owns DNS/DHCP integration. Ensure the fixture's ephemeral workspace maps into the Herdr-managed workload; no unrelated mounts or persistent volumes.

`credential-check` must inspect workload-visible environment, process arguments, runtime configuration and mounts for **absence** of upstream provider keys/tokens; compare against trusted secret identifiers without logging secret contents. A successful grep of one hardcoded filename is insufficient for the live adapter. It should also verify that the workload has no usable direct-provider credentials.

`gateway-evidence` must consult gateway-side audit/metrics records using RUN_ID correlation, verify that an actual successful upstream model response for this run passed through the Aginctus gateway, and confirm that gateway egress rather than workload egress was used. It must output exactly these marker lines after verification:

```text
gateway-observed:<RUN_ID>
upstream-inference-confirmed:<RUN_ID>
```

These are **interface assertions**, not cryptographic proof: operator review of the gateway-side source and networking remains necessary. A marker printed by the workload itself does not qualify.

With a deployed #123-compatible adapter and explicit permission from the operator:

```sh
export AGINCTUS_ACCEPTANCE_ADAPTER=/absolute/path/to/reviewed-live-adapter
export AGINCTUS_ACCEPTANCE_LIVE=I_UNDERSTAND
bash scripts/acceptance/opencode.sh live
```

The live adapter is not included in this initial, #123-independent PR. Running live with the bundled mock adapter cannot pass (it provides no upstream inference marker). Confirm with `herdr ... --help` on the actual pilot that the implementation matches the installed Herdr CLI. Do not use guessed flags. Run on the NixOS/btrfs pilot after checking DNS/DHCP (#220), gateway inference (#122), and workload integration (#123).

## Evidence and troubleshooting

The harness deliberately outputs only fixed PASS/FAIL summaries and opaque run IDs; it retains no raw transcript or tokens after termination. For operator audit, the future live adapter should save **redacted**, access-controlled evidence in an explicitly configured location: timestamps, host and software versions, resource names, correlation ID, sanitized Herdr lifecycle results, workspace test result, gateway request/response IDs and upstream outcome, networking checks, and credential-exposure boolean. Never print secrets, raw request headers, OAuth material or provider responses. Failures in adapter preflight, lifecycle, task completion, expected diff, tests, credential checks, or gateway correlation cause a nonzero exit. A partially created workspace is closed even after a failed agent start. If stop/close fail, the harness warns with the opaque ID, and the operator must inspect and remove **only** the matching owned resources manually. The harness does not automatically retry potentially destructive cleanup.

## #118 acceptance mapping

| Release requirement | Deterministic layer | Live evidence | Status |
| --- | --- | --- | --- |
| Herdr authenticated, launch/observe/terminate | Mock open/start/wait/stop/close and failure cleanup | Herdr control-plane connectivity, agent lifecycle records | Live blocked by deployment/integration |
| OpenCode workspace and development resources | Temporary Go module, local source assertion and real Go tests; fake agent | Mounted workspace, agent trace of file read/write and test execution | Live blocked by #123 |
| OpenCode uses shared-network Aginctus gateway | Correlation marker contract only; **synthetic** | Gateway-side request correlation and denied direct egress | Live blocked by #123 and #220 readiness |
| Gateway performs valid upstream inference | No inference | Gateway response and upstream-provider success, without credentials in workload | Live blocked by #122 and OAuth integration when selected |
| Model-driven coding task, tests, verified traffic | Fake edit, actual fixture tests, lifecycle assertions | Agent tool trace + changed file + successful tests + gateway-side audit | Live blocked by #122/#123 |
| Reproducible NixOS smoke tests | Mock harness repeatable without host or network | Opt-in pilot run and redacted operator record | Harness provided; live execution pending |

No release checklist item is considered satisfied by the mock test alone. The operational pilot must be separately validated and recorded on #118/#124. Host firewall permissions and gateway configuration are out of scope here.
