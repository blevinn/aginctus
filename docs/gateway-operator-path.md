# Gateway and egress: operator decision path (#403)

This page is the **entry point** for operating the current Aginctus LiteLLM gateway. It orders the existing procedures without replacing their exact commands, ownership checks or fail-closed guards. The live pilot is **not signed off**: the [#122 authenticated inference gate](https://github.com/blevinn/aginctus/issues/122) and [#257 OCI egress acceptance](https://github.com/blevinn/aginctus/issues/257) remain open. Recheck those issues, current configuration and the actual pilot before a live change; historical addresses and observations in runbooks are **examples, not authoritative inventory**.

## Choose the next boundary

| Stage | Operator guide | Allowed effect and evidence | What a pass does **not** mean |
| --- | --- | --- | --- |
| 1. Inspect/read-only | [Gateway egress network](gateway-egress-network.md), [host firewall contract](firewall-host-backends.md), [OpenCode preflight](acceptance-opencode.md#2-read-only-pilot-inspection) | Local validation, redacted ownership/bridge and guest status, gateway health, plan/audit commands | Application health and a rendered topology do not prove routing isolation, key authorization or upstream success |
| 2. Exception-only network maintenance | [OCI bridge and NIC migration](litellm-oci-egress-pilot.md) and, **only for a firewalld NixOS host**, [firewalld activation and runtime grants](gateway-firewalld-egress.md) | Separately approved host activation, bridge/DHCP configuration, stopped LiteLLM NIC repair and tightly scoped HTTPS grants, with recorded rollback | A dry-run or completed migration is not permission to make a provider request or proof of deny controls |
| 3. Register an authorized upstream | [API-key provider/model registration](gateway-api-key-pilot.md) | Inert preview first; *only with approval*, one private-gateway registration using a protected upstream API-key file | A registered alias and `/v1/models` entry do not prove working Responses inference |
| 4. Issue a scoped workload key | [Workload virtual-key lifecycle](gateway-workload-key-pilot.md) | Inert preview first; *only with approval*, bounded model-specific LiteLLM key issuance into a private file | It is not a provider key; successful issuance does not establish proxy routing or provider acceptance |
| 5. Approved inference smoke and revocation | [Authenticated Responses smoke](gateway-inference-smoke.md), then [virtual-key revocation](gateway-workload-key-pilot.md) | One explicitly approved potentially billable `/v1/responses` probe, trusted upstream/gateway correlation, negative controls, verified key revocation | A synthetic SSE transcript or HTTP 200 alone does not prove upstream provenance, OpenCode session compatibility or agent-driven task completion |
| 6. Full agent acceptance (separate gate) | [OpenCode acceptance decision path](acceptance-opencode.md) | Independently reviewed Herdr/OpenCode tool traces, private gateway-side provider evidence and safe teardown | Passing the host-only smoke does not establish the full coding-agent vertical slice |

Stages are **not automatic dependencies or authorization grants**. An operator may perform read-only diagnostics without planning a migration, and a healthy topology may require no network change. The two maintenance runbooks are alternative/conditional procedures, **not** a mandatory pair of steps before every inference attempt.

## 1. Baseline before any mutation

Work on the **trusted pilot host** and confirm that Incus addresses the intended local daemon and project(s). Read the active resource names from Aginctus configuration and the current Incus inventory. Do not copy sample `10.x` addresses, instance names or DHCP ranges from a dated session into an apply command.

- Verify the management bridge has IPv4 NAT and routing **disabled**, IPv6 disabled, and that PostgreSQL, Herdr and OpenCode workloads have **no direct upstream route**. Bridge ownership, guest NICs and effective host filtering require independent inspection; a configured setting is not proof of packet denial.
- Verify LiteLLM has its approved outbound-only path, but do not assume a working default route from the Compose render or a planned NIC. Observe real guest routes and interface addresses and confirm the active firewall backend. Check gateway/PostgreSQL private connectivity separately.
- Check application liveness/readiness separately from authentication and actual provider success. A LiteLLM `/health/liveliness` HTTP 200 is only an application-health observation.
- Before any credentials or requests, review the current [#122](https://github.com/blevinn/aginctus/issues/122) provider/session-header conditions and [#257](https://github.com/blevinn/aginctus/issues/257) isolation evidence. No live test is authorized by the presence of these docs.

Use `just check markdown`, `just check docs-examples`, and `just check ci` for **offline repository checks**. They do not test real host security or invoke provider inference.

## 2. Maintenance is a separately authorized change

Only enter these paths if read-only assessment shows a specific deficiency and the operator approves **that exact mutation**:

- The [OCI egress migration](litellm-oci-egress-pilot.md) requires fresh all-project NIC/lease inventory, supported Incus OCI gateway semantics, a noncolliding bridge/DHCP and static LiteLLM address plan, a change window, and a safe rollback. Bridge DHCP changes and stopped-instance NIC reconciliation are separate explicit decisions. PostgreSQL and existing credential/storage volumes are not disposable.
- On hosts actually using firewalld, the [firewalld egress runbook](gateway-firewalld-egress.md) separates permanent NixOS definition and activation from *runtime* source/uplink grants and revocation. A NixOS activation can affect SSH/Tailscale access; verify recovery before approval. Do not grant broad egress, trust the management bridge, or treat firewall reconciliation as permission for model access.

After any change, positively test LiteLLM-to-provider TLS and gateway-to-PostgreSQL connectivity, and **negatively** test prohibited guest-to-public/private/host/Tailscale routes. No speculative firewall fix or `gateway apply` should be used as a repair shortcut. If route, ownership or denial evidence is unclear, **stop** and restore only the specifically reviewed prior state.

## 3. Register an authorized upstream

The [provider registration procedure](gateway-api-key-pilot.md) is independent of host networking. Obtain explicit approval for the **actual provider account, protocol and model**, validate the pinned LiteLLM and upstream contract, and put any upstream API key in an operator-owned 0600 regular file outside Git, Nix store and shell history. Its default dry-run has no live registration effect; live `--apply` is a distinct approval.

## 4. Issue a scoped workload key and plan revocation

The [workload-key procedure](gateway-workload-key-pilot.md) issues a **separate LiteLLM virtual key** for exactly one registered alias, with a one-hour lifetime, bounded budget and concurrency/rate restrictions. Confirm alias, scope, output-path owner/mode and permitted `/v1/models` visibility before a live inference request. Restrict host-side traffic to the observed *private numeric IPv4* LiteLLM HTTP listener on port 4000: that private HTTP transport is **not encrypted**, so never port-forward the admin endpoint or call it from an untrusted machine.

**Treat uncertain issuance, registration or revocation as unresolved remote state**: inspect safe metadata and reconcile by approved procedures. Do not dump expanded Incus configuration (it can contain master/salt/database credentials), print key contents, or delete the protected workload key file before remote revocation is confirmed.

## 5. Inference evidence and operator hold

The [authenticated smoke](gateway-inference-smoke.md) is **opt-in** (`--run`) and can incur provider charges. Before running, separately verify the permitted model/alias, a restricted unexpired virtual key, private listener ownership, gateway-only routing, negative direct-egress checks and cost authority. For OpenCode Go, inspect provider-specific coding traffic requirements (including stable `x-opencode-session` and the expected user-agent/forwarding path) under [#122](https://github.com/blevinn/aginctus/issues/122). **Do not disable authentication, add a direct-provider bypass, broadly forward headers or retry billable traffic merely to make the smoke pass.**

A valid provider acceptance record requires *more than* a successful local HTTP request: terminal Responses completion with expected challenge output; rejection of missing/invalid keys; independent, trusted gateway/provider-side attribution; validated LiteLLM TLS/egress; negative workload/PostgreSQL Internet access; and a successfully revoked temporary virtual key. For actual OpenCode acceptance, add the Herdr-launched coding agent's independently observed read/edit/tool/test execution and run-ID-correlated gateway evidence from the [OpenCode acceptance path](acceptance-opencode.md).

If the upstream returns a nonterminal/error response, session identity is missing, provenance cannot be proven, or revocation is uncertain, record a **failure or unknown** and stop. Neither mock traffic, model registration, `/v1/models`, a `/health` check, nor a potentially fabricated SSE response is a release-acceptance result.

## 6. Full agent acceptance, evidence and recovery

A successful host-only smoke must still be followed by the separately authorized [OpenCode agent acceptance](acceptance-opencode.md), including real Herdr tool/test traces and run-correlated upstream gateway evidence. Synthetic or scripted model protocol acceptance cannot replace this final gate.

For each approved operation, record the observed software versions, sanitized resource identities, change approval, before/after ownership/NIC/route state, safe firewall results, provider outcome and run/request correlation, negative controls, and the explicitly verified key revocation result. Track changing live pilot observations in [#257](https://github.com/blevinn/aginctus/issues/257) and [#122](https://github.com/blevinn/aginctus/issues/122), not as permanent sample addresses here. Refer full-release claims to [#118](https://github.com/blevinn/aginctus/issues/118).

Never publish secret-bearing command output, expanded instance configuration, upstream API or LiteLLM master/workload keys, OAuth state, raw request headers, or unredacted provider responses. For partial NIC/firewall updates, follow the **specific** rollback procedure in the runbook, inspect server state after uncertain timeouts, and never destroy unrelated Incus instances, PostgreSQL databases or credential volumes. An interrupted smoke must result in controlled key revocation/verification, not abandoned live credentials.
