# Read-only OpenCode gateway runtime readiness (#124)

This is a **necessary, not sufficient** gate for the separately opted-in
[OpenCode live acceptance harness](acceptance-opencode.md). It complements the
[Incus topology preflight](acceptance-opencode-preflight.md), the
[Herdr lifecycle checks](acceptance-opencode-herdr.md), and the
[managed gateway provisioning contract](workloads/opencode.md). It does
not execute any model request, change a guest, or inspect secret contents.

The v0.1 managed CLI's `aginctus workload gateway provision dev` command
discovers **one private IPv4 on LiteLLM's management NIC** and writes
`http://<discovered-ip>:4000/v1` to the agent's nonsecret
`gateway.json`. Therefore this gate checks the configured IPv4 address
against RFC 1918 ranges and the expected port/path, **not a fictitious
LiteLLM hostname or a DNS lookup**. These syntax checks do not prove that
the address still belongs to LiteLLM, is reachable, or cannot be spoofed.

From a host-independent checkout (no Incus execution):

```sh
nix develop -c python3 -m unittest discover -s scripts/acceptance -p 'test_opencode_inference_preflight.py' -v
nix develop -c python3 -m py_compile scripts/acceptance/opencode-inference-preflight.py
```

For a separately authorized, **read-only** pilot inspection only:

```sh
nix develop -c python3 scripts/acceptance/opencode-inference-preflight.py
```

The script runs `incus --project default exec` against the managed
`aginctus-dev` guest (override only with a reviewed
`AGINCTUS_ACCEPTANCE_WORKLOAD` name). It checks that the workload
credential is an agent-owned regular 0600 file readable by `agent`,
**without opening or emitting the key**. It reads only the nonsecret
`gateway.json` as `agent`, validating the Responses protocol, model
alias syntax, and managed private IPv4 endpoint shape. Command failures
have static redacted labels; subprocess output and runtime values are not
included in reports.

**Not verified:** managed gateway ownership, network isolation, key
authorization, actual LiteLLM reachability, streaming Responses tool
compatibility, upstream inference, Herdr agent execution, workspace changes,
or gateway-side correlation. Those require the full live harness and
independent gateway/network evidence. Do **not** close #124, authorize
inference, or change Incus/firewall/key state based on this check alone.
