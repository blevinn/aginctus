# Opt-in live acceptance phase adapter (#124)

`scripts/acceptance/opencode-live-adapter.sh` is a **fail-closed dispatcher**
for the [disposable acceptance harness](acceptance-opencode.md). It requires
operator-supplied hooks and is not proof of a completed end-to-end run.

## Why explicit hooks

The [OpenCode guest launcher](workloads/opencode.md) reads private gateway
configuration and a workload key. The separate
[Herdr lifecycle implementation](acceptance-opencode-herdr.md) supports
`open`, `start`, `prompt`, `wait`, `stop` and `close`, but does not provide
complete fixture transport or real preflight, credential and gateway-audit
checks. Each installation must provide reviewed executable hooks for all
nine phases in one absolute directory. A workload-emitted correlation ID
alone is not gateway proof.

The dispatcher rejects absent or symlinked hooks, malformed arguments, non-owned run IDs and missing opt-in. It passes through the original harness arguments and preserves hook failure codes. The existing harness suppresses raw stdout/stderr of all phases (except that gateway evidence is validated from a private temporary file). Hooks must never print secrets, even on failure.

## Required hook responsibilities

- `preflight`: Check deployed Aginctus/Incus, NixOS management network with IPv4 NAT and routing disabled, host firewall DNS/DHCP, Herdr authentication, OpenCode guest readiness, gateway connectivity, available permitted model and gateway-only workload credentials. Fail without mutating host state.
- `open`: Create a *new* Herdr workspace with the passed randomly generated run ID, backed by the passed temporary fixture path. Never attach to or delete an existing workspace.
- `start`: Start only that run's OpenCode agent via Herdr. Use the guest's fail-closed gateway launcher (no alternate provider fallback).
- `prompt`: Submit the passed prompt. No shell evaluation of its contents.
- `wait`: Bounded poll for agent completion, and independently inspect tool-run traces proving the agent read and edited the fixture and executed `go test ./...`.
- `credential-check`: Verify guest environment, process arguments, configuration and reachable mounts do not expose **upstream** credentials; the approved scoped LiteLLM workload key is permitted. Also independently test that the guest has no direct provider egress.
- `gateway-evidence`: Query gateway-side (not guest-generated) records keyed to the nonce, verify actual upstream success and correlate request/response. Only then output exactly `gateway-observed:RUN_ID` and `upstream-inference-confirmed:RUN_ID`, one per line.
- `stop`/`close`: Idempotently terminate only resources associated with that run ID, including if startup partly failed. Do not remove user workspaces, volumes or Incus instances.

Scripts must supply their own timeouts and safe cleanup semantics. The dispatcher does not invent endpoint discovery, authentication, credential provisioning, logs or metadata. Operators should capture a redacted evidence report through reviewed hooks. The existing harness deletes its temporary local evidence after completion; failed remote stop/close operations require explicit owned-resource inspection.

## Deterministic validation

```sh
# Safe deterministic dispatch checks, no Herdr or provider required:
bash -n scripts/acceptance/opencode-live-adapter.sh scripts/acceptance/opencode-live-adapter-test.sh
bash scripts/acceptance/opencode-live-adapter-test.sh
bash scripts/acceptance/opencode-test.sh
```

These checks use test doubles and do not contact Herdr or a provider.

## Separately authorized live execution

Live execution can create remote workspaces, launch agents and incur
provider usage. First satisfy the deployment, networking, model and
credential prerequisites in the linked guides and the
[#122 inference gate](https://github.com/blevinn/aginctus/issues/122),
including its operator hold. Then obtain explicit operator authorization
and review all nine hooks, their timeouts and cleanup behavior.

```sh
export AGINCTUS_ACCEPTANCE_LIVE_HOOKS=/absolute/path/to/reviewed-hooks
export AGINCTUS_ACCEPTANCE_LIVE=I_UNDERSTAND
# Install an executable copy in an operator-owned directory outside the checkout:
install -m 0700 scripts/acceptance/opencode-live-adapter.sh /absolute/path/to/operator-bin/opencode-live-adapter
export AGINCTUS_ACCEPTANCE_ADAPTER=/absolute/path/to/operator-bin/opencode-live-adapter
bash scripts/acceptance/opencode.sh live
```

The destination directory must already exist. The bundled dispatcher has
non-executable repository mode; installing a copy satisfies the harness's
executable-adapter requirement without changing checkout permissions.

A missing hook intentionally prevents live acceptance. Merely adding no-op
hooks or markers is **not** a valid live result. Record reviewed live
evidence on [#124](https://github.com/blevinn/aginctus/issues/124) and the
[#118 release checklist](https://github.com/blevinn/aginctus/issues/118);
deterministic dispatch checks do not satisfy those gates.
