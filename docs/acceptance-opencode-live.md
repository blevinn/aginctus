# Opt-in live acceptance phase adapter (#124)

This is a **fail-closed dispatcher**, not a built-in Herdr automation implementation and not proof of a completed end-to-end run. It consumes the disposable acceptance harness from PR #227 without modifying #123, host firewall or gateway inference.

## Why explicit hooks

As of this PR, #123 supplies a guest launcher at `guest/opencode/launch.py` that reads `/var/lib/aginctus/opencode/gateway.json` and `workload-key`, but its branch does not define a stable host-side, fully automated Herdr workspace/pane lifecycle API or gateway audit interface. Guessing Herdr subcommands or treating a workload-emitted correlation ID as gateway proof would be unsafe. Each installation must provide reviewed and runnable scripts for `preflight`, `open`, `start`, `prompt`, `wait`, `credential-check`, `gateway-evidence`, `stop` and `close` in one absolute directory.

The dispatcher rejects absent or symlinked hooks, malformed arguments, non-owned run IDs and missing opt-in. It passes through the original harness arguments and preserves hook failure codes. The existing harness suppresses raw stdout/stderr of all phases (except that gateway evidence is validated from a private temporary file). Hooks must never print secrets, even on failure.

## Required hook responsibilities

- `preflight`: Check deployed Aginctus/Incus, NixOS management network with IPv4 NAT and routing disabled, host firewall DNS/DHCP, Herdr authentication, OpenCode guest readiness, gateway connectivity, available permitted model and gateway-only workload credentials. Fail without mutating host state.
- `open`: Create a *new* Herdr workspace with the passed randomly generated run ID, backed by the passed temporary fixture path. Never attach to or delete an existing workspace.
- `start`: Start only that run's OpenCode agent via Herdr. Use #123's fail-closed gateway launcher (no alternate provider fallback).
- `prompt`: Submit the passed prompt. No shell evaluation of its contents.
- `wait`: Bounded poll for agent completion, and independently inspect tool-run traces proving the agent read and edited the fixture and executed `go test ./...`.
- `credential-check`: Verify guest environment, process arguments, configuration and reachable mounts do not expose **upstream** credentials; the approved scoped LiteLLM workload key is permitted. Also independently test that the guest has no direct provider egress.
- `gateway-evidence`: Query gateway-side (not guest-generated) records keyed to the nonce, verify actual upstream success and correlate request/response. Only then output exactly `gateway-observed:RUN_ID` and `upstream-inference-confirmed:RUN_ID`, one per line.
- `stop`/`close`: Idempotently terminate only resources associated with that run ID, including if startup partly failed. Do not remove user workspaces, volumes or Incus instances.

Scripts must supply their own timeouts and safe cleanup semantics. The dispatcher does not invent endpoint discovery, authentication, credential provisioning, logs or metadata. Operators should capture a redacted evidence report through reviewed hooks. The existing harness deletes its temporary local evidence after completion; failed remote stop/close operations require explicit owned-resource inspection.

## Execution

```sh
# Safe deterministic dispatch checks, no Herdr or provider required:
bash -n scripts/acceptance/opencode-live-adapter.sh scripts/acceptance/opencode-live-adapter-test.sh
bash scripts/acceptance/opencode-live-adapter-test.sh
bash scripts/acceptance/opencode-test.sh

# Later, on the configured pilot, after independently reviewing all nine hooks:
export AGINCTUS_ACCEPTANCE_LIVE_HOOKS=/absolute/path/to/reviewed-hooks
export AGINCTUS_ACCEPTANCE_LIVE=I_UNDERSTAND
# Files created through the GitHub API are initially not executable. Mark the adapter
# executable in your checkout (do not commit this mode change on the PR branch):
chmod +x scripts/acceptance/opencode-live-adapter.sh
export AGINCTUS_ACCEPTANCE_ADAPTER="$PWD/scripts/acceptance/opencode-live-adapter.sh"
bash scripts/acceptance/opencode.sh live
```

A missing hook intentionally prevents live acceptance. Merely adding no-op hooks or markers is **not** a valid live acceptance result. Until real reviewed hooks are implemented and observed on the pilot, #118's live checklist remains unchecked.
