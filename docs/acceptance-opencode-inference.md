# Read-only OpenCode gateway runtime readiness (#124)

This is the **second read-only check in stage 2** of the [OpenCode acceptance decision path](acceptance-opencode.md#2-read-only-pilot-inspection), after the host/Incus topology preflight. Running it does not start an agent, validate a scoped LiteLLM key remotely or grant permission for billable inference.

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
`AGINCTUS_ACCEPTANCE_WORKLOAD` name). As a narrow #521 credential
inventory check, it requires the provisioned
`/var/lib/aginctus/opencode` runtime directory to be a real, owner-private
`0700 agent` directory. A fixed shell program executed as `agent`
requires exactly the two expected entries (`gateway.json` and
`workload-key`) and rejects unexpected names or symlinks. The checks
emit **no directory contents or filenames**. The two provisioned files must
be agent-owned regular `0600` files; the script checks readability of
the workload key by `agent` **without opening or emitting the key**.
It reads only the nonsecret `gateway.json` as `agent`, validating the
Responses protocol, model alias syntax, and managed private IPv4 endpoint
shape. Command failures have static redacted labels; subprocess output
and runtime values are not included in reports.

This is a **point-in-time allowlist of the managed runtime directory only**,
not a scan of all guest mounts, user homes, agent process environments or
provider API keys. It cannot establish the absence of upstream credentials
elsewhere, nor prevent changes after inspection. A newly deployed guest does
not require an image change for these checks: they use standard existing
`stat`, `find`, `runuser` and POSIX shell commands from the trusted host.
The operator must run the separate [managed-workload ownership and NIC
preflight](acceptance-opencode-preflight.md) first. This read-only runtime
metadata check alone does not revalidate Incus ownership.

**Not verified:** managed gateway ownership, network isolation, key
authorization, actual LiteLLM reachability, streaming Responses tool
compatibility, upstream inference, Herdr agent execution, workspace changes,
or gateway-side correlation. Those require separately authorized live checks and independent
gateway/network evidence. Closure of [#124](https://github.com/blevinn/aginctus/issues/124)
is **not proof** of provider success. Do **not** authorize inference or change
Incus, firewall, or credential state based on this check alone; consult the
active [#122 inference gate](https://github.com/blevinn/aginctus/issues/122).
