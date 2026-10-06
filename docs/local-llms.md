# Locally hosted LLMs

Status: proposed design; no local-provider configuration, connectivity policy, or model-serving lifecycle is implemented by this document.

## Problem and scope

Aginctus's [AI gateway](ai-gateway.md) currently targets hosted OpenAI and OpenCode Go upstreams. Operators also need to run inference on hardware they control for privacy, offline operation, and predictable resource use. Local models must retain the same workload identity, model permissions, and audit path as hosted models.

The first slice connects the existing LiteLLM gateway to an operator-managed OpenAI-compatible server on the Incus host or a trusted LAN machine. Ollama, llama.cpp server, and vLLM are candidate servers; compatibility is verified per pinned server version and model, not inferred from the product name. Aginctus does not install, start, download models for, or delete these external servers.

Managed Incus model-serving workloads, native Ollama APIs, GPU passthrough, model downloads, distributed inference, embeddings, and automatic routing between local and hosted models are follow-up work. This proposal does not expand the first Herdr milestone or claim the gateway's planned authentication and policy controls already exist.

## Decisions

- Keep LiteLLM as the inference boundary. Add local upstreams through its OpenAI-compatible provider adapter rather than adding a second inference path to agents.
- Treat hosting location and wire protocol separately: `hosting: local` describes operator-controlled deployment, while `type: openai-compatible` selects the adapter.
- Expose stable model aliases. A workload receives a gateway URL and workload credential; it never receives the local server URL or upstream key.
- Require explicit endpoint, upstream model ID, capabilities, and workload grants. Model discovery may aid operator inspection but must never grant access or download a model.
- Fail the selected local alias when its server is unavailable. Never spill prompts to hosted providers or substitute another model automatically.
- Start with external servers. Their lifecycle and hardware requirements stay with the operator until the gateway path is proven.

## Request path and trust boundaries

```text
managed agent -- TLS + workload credential --> authenticated gateway entry point
  --> alias authorization --> LiteLLM --> operator-managed inference server
                                      --> usage/audit persistence
```

Gateway-only access to an unauthenticated local server is acceptable solely on an explicitly restricted private path. Workload authentication remains mandatory even if the upstream requires no key. The local server is a separate trust boundary: its operator can observe prompts, completions, and tool payloads, and its logs may retain them. `hosting: local` is a declaration, not proof of data residency or absence of server-side telemetry.

Local inference must depend on the gateway design's authenticated entry point, coherent alias authorization, credential revocation, and failure admission rules. A bare LiteLLM deployment from the current rendering is not an accepted secure implementation of this proposal.

## Proposed configuration

The following JSON is an illustrative extension of the existing effective configuration, not a supported CLI example. It deliberately uses JSON and the existing `gateway` namespace rather than introducing another configuration loader.

```json
{
  "gateway": {
    "providers": {
      "localcpu": {
        "type": "openai-compatible",
        "hosting": "local",
        "baseURL": "http://10.42.0.1:8080/v1",
        "auth": { "mode": "none" },
        "transport": { "allowPlaintext": true },
        "connectTimeoutSeconds": 5,
        "requestTimeoutSeconds": 300,
        "streamIdleTimeoutSeconds": 60,
        "maxConcurrentRequests": 1
      }
    },
    "models": {
      "localcoding": {
        "provider": "localcpu",
        "model": "operator-selected-model-id",
        "capabilities": {
          "chatCompletions": true,
          "streaming": true,
          "tools": true,
          "responses": false,
          "vision": false
        },
        "contextWindowTokens": 32768,
        "maxOutputTokens": 4096
      }
    },
    "workloads": {
      "codingagent": { "models": ["localcoding"] }
    }
  }
}
```

The address and model ID are placeholders. The context/output limits are operator-supplied bounds for the selected model and server configuration, not measured properties of all local models. Existing configuration precedence applies. Provider/model IDs must use a constrained identifier syntax compatible with dotted overrides; arrays replace lower-precedence arrays. Removing an entry from a higher-precedence object does not erase a recursively merged lower-precedence entry: operators must remove its original definition and inspect effective configuration. Workload grants remain explicit arrays.

For a protected endpoint, use `auth: { "mode": "bearer", "credentialRef": "local-server-key" }`. Resolve the reference only into gateway runtime secret storage; reject literal keys and credentials in URLs. `none` must be explicit. No dummy key is a credential: if the pinned adapter requires a placeholder for keyless operation, inject an internal nonsecret placeholder and verify it does not accidentally become an upstream Authorization header.

HTTPS is the default requirement. HTTP requires explicit `allowPlaintext: true` and a verified isolated host/private-network path. For LAN connections require HTTPS with normal certificate validation, including a protected operator-provisioned private CA when necessary; do not add an insecure TLS switch. Reject unknown references, invalid schemes, userinfo, query/fragment-bearing URLs, invalid positive limits, inconsistent capabilities, and output limits exceeding the context window before applying. Preserve `/v1` exactly once when generating LiteLLM `api_base`; do not guess or append endpoint paths twice.

## Connectivity and isolation

`localhost` and `127.0.0.1` inside LiteLLM identify its guest, not the Incus host. The initial slice rejects loopback upstream addresses and requires an explicit address reachable from the gateway guest. IPv6 loopback and equivalent resolved addresses receive the same treatment. Do not depend on Docker-specific host aliases or host networking.

For an Incus-host server, bind the inference listener to a deliberately selected private bridge-side address, not every host interface. Discover the bridge address through Incus or require it explicitly; do not guess an address from an automatically allocated subnet. For a LAN server, define a narrow gateway-to-server route and destination/port rule. The management bridge currently defaults to IPv4 NAT and routing disabled, so a LAN URL alone does not establish reachability. Do not globally enable NAT, routing, or workload internet access as a side effect of adding a provider.

Before enabling the alias, verify from the LiteLLM guest that the intended destination is reachable and from an agent guest that direct inference access is denied. Host firewall rules and Incus ACLs must cover IPv4 and IPv6, allowing gateway connections and established replies while denying agent connections to the server. An unauthenticated server on a flat management bridge is not acceptable. Endpoint changes require revalidation and policy reconciliation before routing changes become active. For DNS names, destination controls must cover resolved addresses and reject loopback, link-local metadata, and unrelated management/admin destinations; prevent redirects from escaping the configured destination. Private addresses themselves are legitimate for this feature.

These narrow server-access restrictions are part of accepting local support. They do not implement general network-wide prevention of gateway bypass, which remains deferred in the gateway design. If the platform cannot enforce the chosen path, report the unmet isolation requirement rather than claim readiness. Never modify unrelated host firewall rules or adopt an external server as an owned resource implicitly.

## Protocol and runtime compatibility

Use OpenAI Chat Completions as the first upstream contract. Preserve streaming chunks, finish reasons, tool-call IDs, argument fragments, and usage when reported. Reject unsupported modalities and protocols clearly. Do not advertise Responses support or emulate it silently. Enable a runtime/model pair only after verifying the actual OpenCode or Hermes request shape; a runtime requiring Responses is outside this slice until a separate adapter is validated.

Capabilities are configured per model, checked at admission, and confirmed by compatibility tests. Tool support depends on model weights, chat template, and server parser as well as the HTTP interface. A text-only completion is insufficient evidence for an agent-ready alias. Do not silently strip tool definitions or reduce an oversized context. Reject requests exceeding configured limits when the pinned adapter can measure them reliably; otherwise expose the limit and propagate a sanitized upstream context error without claiming exact local tokenization.

Select and pin at least one server/model/agent combination for acceptance. Record server version, model identifier and revision or checksum where available, quantization, context setting, chat template, and tool parser in the integration fixture. Other servers remain candidates until the same matrix passes. LiteLLM's currently pinned release must be checked for configuration, timeouts, streaming, and tool behavior before relying on adapter support.

## Admission, failures, and observability

Local hardware may serve only one request at a time. Apply the configured concurrency limit per provider across aliases and gateway workers; a per-process counter is insufficient. Initially reject excess requests with a clear overload response and bounded Retry-After rather than maintaining an unbounded queue. If the selected component cannot enforce the limit, implementation must supply shared admission control or constrain deployment to one worker with a documented bound.

Separate connect, total-request, and stream-idle deadlines. Propagate disconnect/cancellation upstream where supported and release admission slots on every completion/error/cancellation path. Do not automatically retry inference, including ambiguous pre-stream timeouts; a server may already be generating. Once output starts, failures terminate the stream visibly without restart or substitution.

Health distinguishes configured, reachable, model-ready, overloaded, and incompatible states. Probe from the gateway network namespace with bounded timeouts; `/v1/models` may be used when supported, but listing a model alone does not prove it is loaded or tool-capable. Periodic health checks must not trigger downloads or expensive generations. Operator-invoked compatibility checks may perform a small explicit inference. Server restarts and model unloading affect the relevant aliases, not hosted-provider health.

Reuse gateway audit attribution: workload, alias, configured provider, actual upstream model, outcome, latency, and reported token usage. Record unknown usage as unknown. Do not invent a cloud price or report total operating cost as zero for local inference. Hardware/energy accounting is deferred. Omit prompt/completion content, keys, and raw server errors by default. Bound server error bodies and sanitize them before returning diagnostics.

## Reconciliation and lifecycle

Validation is offline and makes no requests. Rendering produces deterministic redacted configuration and secret references. Deployment renders local routing into the same authoritative LiteLLM configuration as hosted models and applies network restrictions before enabling routes. Ensure the Go Compose renderer and Jsonnet deployment resource agree; do not implement local providers only in `gateway render`.

Apply must validate the entire effective provider/model/grant set, stage routing and policy together, and activate coherently through the pinned gateway's supported mechanism. On failure preserve the previous active set or disable affected routes; never leave a new unguarded alias live. Removing a provider disables its aliases for new requests; in-flight streams may finish under the existing gateway revocation contract. Gateway stop/down does not stop the external server or delete weights. Backup includes configuration and referenced upstream secrets, not external model files; document server recovery separately.

Proposed diagnostic surface: extend future `gateway status` with redacted per-provider readiness and add an operator-only `gateway providers check <id>` for connectivity and explicit compatibility checks. Exact arguments and implementation remain follow-up work; the existing validate/render/up commands do not support these fields yet.

## Implementation plan and acceptance

1. Add typed provider/model/grant parsing to `internal/gateway` using `internal/config` precedence. Test invalid references, endpoint/auth/transport validation, limit validation, unchanged hosted-only configurations, and secret-free rendering.
2. Extend both `RenderCompose` and `internal/orchestration/resources/gateway.jsonnet` to provision the same LiteLLM configuration and out-of-band credential references. Add deterministic parity tests. Implement network preflight and destination restrictions through the orchestration/adaptor boundary; fail before activation when isolation is unavailable.
3. Complete the prerequisite gateway authentication and authorization slice, then implement capability checks, shared admission, timeouts, cancellation, redacted diagnostics, and per-provider health. Preserve hosted-provider behavior without introducing fallback.
4. Run a mock upstream suite for SSE fragmentation, tool calls, missing usage, malformed responses, auth errors, overload, timeout, mid-stream disconnect, cancellation, and provider loss. Verify unauthorized aliases never reach the server and upstream secrets never reach agents or output.
5. On an Incus host, exercise one pinned real server/model with an actual agent: text, streaming, tool invocation with returned tool result, context failure, restart recovery, and concurrent requests. Verify gateway reachability and denied agent-to-server access on every enabled IP family. Repeat through a trusted LAN endpoint before claiming that topology supported.
6. Run with hosted providers removed and internet egress disabled, with weights already installed. A local request must succeed without cloud requests, provider login, model downloads, or telemetry dependencies; any required audit database and identity state must remain available. Failures must not contact a hosted provider even when one is configured.

This PR proposes the contract only. No runtime compatibility or security enforcement is claimed until these checks pass. The implementation should land in small slices, with local aliases unavailable to workloads until the prerequisites are satisfied.

## Alternatives and follow-up

Direct agent-to-server access is simpler but duplicates credentials and bypasses gateway authorization and accounting. Native server-specific adapters can expose additional features but multiply the initial compatibility surface. Both are deferred in favor of the existing gateway boundary.

Managing model servers inside Incus can later provide owned lifecycle, private service discovery, persistent model volumes, resource limits, and reproducible images. That needs a separate design for container versus VM GPU access, device ownership, drivers, scheduling, artifact provenance/licensing, offline import, disk quotas, readiness, and explicit destructive cleanup. It must not grant agent workloads GPU devices or privileged host access merely to serve inference.
