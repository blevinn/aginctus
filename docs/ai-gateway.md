# AI gateway design

Status: agreed design direction; initial configuration validation and Compose rendering are implemented, while lifecycle, authentication, credentials, MCP policy, and backup/restore remain follow-up work.

## Scope

The MVP serves one operator through one gateway deployment. Aginctus manages an Incus Compose project containing LiteLLM, PostgreSQL, an authenticated entry point, and MCP gateway functionality. The entry point may be provided by a gateway component rather than a separate service. A separate MCP service is needed only if the selected LiteLLM release cannot satisfy the requirements below.

Initial inference upstreams are OpenAI through ChatGPT/Codex OAuth and OpenCode Go through an API key and OpenAI-compatible endpoints. LiteLLM is the selected inference component. Its pinned release must be validated against the OAuth flow and required inference protocols before implementation is considered complete.

The MVP includes workload identities, explicit model and MCP permissions, inference usage accounting, audit records, health checks, and backup/restore. Spending limits are not enforced. Budget enforcement is a high-priority next milestone.

Multi-tenancy, multiple gateways, federation, a central control plane, high availability, local MCP subprocess execution, and an approval UI are outside this MVP. This gateway slice does not change the scope of the [first Herdr milestone](milestones/0001-herdr-console.md).

## Initial implementation slice

The first implementation exposes:

```text
aginctus gateway validate
aginctus gateway render
```

The effective configuration currently provides a stable gateway ID, Compose project name, management network, and pinned LiteLLM/PostgreSQL images. `gateway render` emits a deterministic Compose model for LiteLLM plus PostgreSQL and references runtime secrets through environment variables rather than embedding credentials.

This slice deliberately does not mutate Incus. Deployment will be wired through the declarative orchestration/Compose integration so the gateway does not introduce another bespoke reconciliation path.

## Deployment and ownership

```text
Incus host
  aginctus CLI -- Incus management transport --> gateway administration
  managed agent workloads -- TLS + workload credential --> gateway entry point
    inference --> LiteLLM --> OpenAI OAuth / OpenCode Go
    /mcp/<server> --> MCP gateway --> approved remote MCP servers
    gateway components --> PostgreSQL
```

Aginctus owns the gateway configuration and translates it into Compose and component configuration. Compose is a deployment implementation detail, not the public workload interface. The implementation must select and pin an Incus Compose tool and verify its resource ownership, network, volume, health, and teardown behavior; Docker Compose semantics must not be assumed.

Use the existing Aginctus management-network model for managed workloads. Expose TLS inference and MCP endpoints to authorized workloads on that private network. Do not publish PostgreSQL or component administration interfaces to workloads or the public internet. Host-side CLI administration uses Incus exec or an equivalent authenticated Incus operation, so the MVP needs no separate operator account system or network admin login.

Gateway resources carry Aginctus ownership metadata and a stable gateway ID. Audit records include that ID; credentials are scoped to it. Each gateway must remain independently operable if multiple deployments are added later.

PostgreSQL stores durable gateway state, usage, and audit records. Components own their schemas and migrations. Sharing a database instance does not authorize components to modify each other's tables. The orchestration CLI does not need a separate database. Secrets and OAuth state have durable protected storage outside declarative configuration; database backups alone may therefore be insufficient for recovery.

## Authentication domains

Keep three authorities distinct:

| Authority | Credential | Purpose |
| --- | --- | --- |
| Operator | Existing host/Incus management authorization | Configure the gateway and manage credentials |
| Workload | Revocable gateway-issued bearer credential | Invoke permitted models and MCP tools |
| Upstream provider or MCP server | API key or OAuth session | Authenticate gateway requests to upstreams |

Workloads receive gateway credentials only. Provider keys, OAuth access and refresh tokens, database credentials, and gateway master credentials never enter agent environments. Possession of a workload token does not grant administration access.

### Provider authentication in the CLI

Proposed commands:

```text
aginctus gateway auth login openai
aginctus gateway auth login opencode-go
aginctus gateway auth status
aginctus gateway auth logout openai
aginctus gateway auth logout opencode-go
```

For OpenAI, the CLI coordinates the supported OAuth mechanism of the pinned provider integration. Prefer a device flow if supported; otherwise use the supported browser authorization flow with state validation and PKCE where applicable. Do not invent an OAuth application, callback path, or reuse unrelated ambient CLI credentials. Headless operation must follow the integration's supported mechanism. Aginctus displays the authorization URL and instructions, and transfers resulting credentials over the Incus management channel into protected gateway storage. No OAuth session is represented as a static API key in the manifest.

The gateway integration owns token refresh during normal operation, including refresh-token rotation. The CLI does not need to remain running. Concurrent refresh must be serialized for a session, and updated credentials written atomically. Explicit authorization rejection or an invalid refresh token changes the provider state to `reauthentication-required`; transient network errors report degraded connectivity without discarding the session. Requests targeting an unavailable provider fail clearly. Other providers remain usable. Reauthentication replaces the session without changing workload tokens.

For OpenCode Go, prompt for the API key with terminal echo disabled. Allow a noninteractive secret input channel such as stdin or a protected file; do not accept secret values in command-line arguments. Store the key under the configured secret reference. The endpoint is configurable and must be validated against provider documentation rather than guessed. A credential check must be non-billable where supported; a successful login does not require generating a completion.

`auth status` reports configured, authenticated, reauthentication-required, or unconfigured state, separately from upstream connectivity. It never prints tokens. A successful local logout deletes gateway-held credentials and stops new requests using that credential. Upstream revocation is attempted only when supported, and the result is reported separately; local deletion does not imply remote revocation. In-flight requests may finish.

### Workload credentials

Proposed commands:

```text
aginctus gateway credentials issue coding-agent
aginctus gateway credentials list coding-agent
aginctus gateway credentials rotate coding-agent
aginctus gateway credentials revoke <credential-id>
```

Issue credentials only for workloads present in the applied configuration. Each credential has an identifier, gateway ID, workload association, creation time, and revocation state. Use the selected gateway's supported verification mechanism; never expose verifier or master-key material through list/status commands.

Managed-workload issuance provisions the credential directly into workload secret storage and prints only its identifier. Explicit export for an external client uses a protected destination file, not default console output. Define the output-file interface during implementation.

Rotation provisions a replacement, verifies delivery, and then revokes the previous credential. If delivery fails, preserve the old credential and report the incomplete rotation. Policy remains workload-scoped rather than copied into long-lived credentials. Revocation and removal of a workload block subsequent requests, including calls through an existing MCP session. Existing inference streams may finish. Reconciliation must disable removed workloads rather than leave usable orphan credentials.

## Proposed configuration

This gateway-focused manifest is illustrative; integration into the broader Aginctus schema remains to be finalized. No budget fields are included in the MVP.

```yaml
apiVersion: aginctus.dev/v1alpha1
kind: AIGateway

gateway:
  id: local
  network: management
  tls:
    certificateRef: gateway-cert
    privateKeyRef: gateway-key

storage:
  postgres:
    connectionSecretRef: gateway-db

providers:
  openai:
    type: openai-oauth
    sessionRef: openai-session
  opencode-go:
    type: openai-compatible
    baseURL: "<documented-opencode-go-endpoint>"
    credentialRef: opencode-go-key

models:
  coding:
    provider: openai
    model: "<validated-openai-model-id>"
  general:
    provider: opencode-go
    model: "<validated-opencode-go-model-id>"

mcpServers:
  docs:
    url: "https://docs.example.com/mcp"
    credentialRef: docs-token
    allowedTools: [search, fetch]

workloads:
  coding-agent:
    models: [coding, general]
    mcp:
      docs:
        tools: [search, fetch]

observability:
  audit: true
  inferenceUsage: true
  logRequestContent: false
```

The MCP endpoint and tool names are placeholders. A local test fixture exposing read-only tools is sufficient for MVP validation; no production MCP service is selected. Model aliases are public and stable; upstream model IDs remain configurable. Workloads have no access unless explicitly granted. Effective tool permission is the intersection of the server allowlist and the workload grant.

Validate unknown references, unsupported provider/protocol combinations, and tool grants outside server allowlists before applying configuration. Configuration is authoritative; component admin interfaces must not allow workload-driven changes. Apply validated policy coherently so requests cannot observe a partially applied grant set. Do not silently fall back to a different provider or model.

## Request contracts

### Inference

Expose OpenAI-compatible inference endpoints. During implementation, select and validate the endpoints needed by OpenCode and Hermes, including whether Chat Completions, Responses, or both are required. Compatibility includes streaming and tool-call payloads, not just a successful text completion.

Authenticate the workload, authorize the model alias, resolve the upstream, inject the provider credential, and forward through LiteLLM. Record workload, alias, actual provider/model, outcome, latency, and reported token usage. Preserve upstream usage where supplied; mark missing usage as unknown rather than zero. Record estimated monetary cost only where supported, with its basis; subscription usage must not be assigned fictitious per-token spending. Do not automatically retry or switch providers after streaming has started.

### MCP

Expose one Streamable HTTP endpoint per configured server at `/mcp/<server>`. Bind upstream sessions to the authenticated workload and server; clients cannot reuse another workload's session identifier. Check server access on connection, filter discovery, and independently check tool access on every invocation. Apply these checks to batched requests if the selected protocol version supports them.

MVP exposes tools only. Resources, prompts, upstream-initiated sampling, and other capabilities requiring additional policy are not advertised or forwarded. Tool annotations do not grant authority. Upstream URLs come from operator configuration; callers cannot supply arbitrary upstream destinations.

Forward authorized calls using gateway-held upstream credentials. Record workload, server, tool, outcome, and latency; omit arguments and results by default. Do not automatically retry tool invocations because a timeout may occur after a side effect has completed.

MCP implementation acceptance requires session handling, workload isolation, discovery filtering, per-invocation enforcement, upstream credential injection, revocation during existing sessions, and audit hooks. Evaluate the pinned LiteLLM release first; add a separate MCP component if required. Protocol proxy support alone is insufficient.

## Failures, health, and operations

Authentication or authorization evaluation failure blocks affected requests. Database outages must not silently disable policy enforcement or revocation checks. Reject new requests when the required durable identity/policy state cannot be evaluated. Audit and accounting storage failures must be visible through health; define bounded buffering and request admission behavior before implementation is accepted.

Health distinguishes service liveness, database readiness, provider authentication state, provider reachability, MCP reachability, and accounting/audit degradation. An unavailable upstream must not make unrelated providers unusable. Sanitize upstream errors and redact authorization headers and credential-bearing URLs from diagnostics.

Proposed lifecycle commands are `aginctus gateway validate`, `up`, `status`, `apply`, `down`, `backup`, and `restore`. Exact arguments remain to be designed. Stop/down preserves durable data and secrets. Destructive deletion must be a separate explicit action. Backups include configuration, database state, and protected secret/OAuth material needed for recovery; storage and transfer must protect those credentials. Restore retains the gateway identity, applies compatible migrations, and verifies policy, credentials, and OAuth usability before reporting ready. Expired or revoked OAuth sessions require reauthentication.

Managed workloads access approved upstreams through gateway credentials. Mandatory network-wide prevention of bypass is deferred to Aginctus egress policy. This design alone does not restrict an operator from supplying other credentials or creating alternate outbound paths.

## Acceptance and follow-up

Implementation is accepted when:

1. Aginctus provisions and discovers an owned gateway Compose deployment with persistent PostgreSQL and protected secret storage.
2. The CLI completes OpenAI OAuth and OpenCode Go API-key setup without exposing upstream credentials to workloads, logs, or declarative configuration.
3. OpenAI credentials survive restart and refresh without a running CLI; invalid refresh state is diagnosable and recoverable through login.
4. At least one initial agent runtime makes a streaming inference request through each upstream using a gateway credential.
5. Unauthorized models and tools are denied; MCP discovery matches permissions; sessions cannot cross workload boundaries.
6. Credential issuance, rotation, revocation, and workload removal behave as specified, including existing MCP sessions.
7. Usage and audit records are attributable and omit request content and secrets by default.
8. Database/upstream outages produce the documented admission and health behavior.
9. Backup/restore is exercised, including secret state and reauthentication where necessary.

Before implementation, resolve the Incus Compose implementation, pinned LiteLLM release and feature availability, OpenAI OAuth integration, OpenCode Go endpoint/model IDs, supported inference endpoints, MCP component selection, TLS bootstrap, and audit-failure admission behavior.

The next gateway milestone adds workload spending limits, periods and resets, alerts, and enforcement with a documented concurrent-request overshoot bound. Subscription quotas must be distinguished from monetary budgets. Multiple independently managed gateways can follow later without introducing shared databases or federation into this MVP.
