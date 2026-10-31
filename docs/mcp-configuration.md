# MCP configuration

Status: the configuration contract is implemented and validated. The
behavior behind it is not. Nothing in this document describes a running MCP
service.

Aginctus has a normalized, typed and validated configuration model for MCP
servers, per-workload grants and model capabilities. This document describes
that model, the effective grant semantics it produces, and the limits of what
the model can guarantee.

The implementation lives in `internal/mcpschema` (schema, validation and
effective policy) and `internal/config` (precedence and source rejection).

## Scope

This contract covers:

- named MCP servers and their transports;
- remote Streamable HTTP endpoints and workload-local stdio commands;
- optional server-level tool restrictions;
- opaque credential references;
- explicit per-workload server and tool grants;
- the central gateway MCP intent;
- model and protocol capability requirements.

This contract does **not** cover, and no part of the current tree provides:

- a central MCP gateway, an MCP endpoint or a route (#163);
- authenticated workload identity or per-invocation authorization (#164);
- Responses API forwarding (#165);
- OpenCode MCP configuration generation (#166);
- segmented workload networking for MCP access (#167).

## Configuration shape

The declaration lives under the `mcp` key of the ordinary Aginctus
configuration, so it uses the ordinary precedence rules described in
[Configuration](configuration.md).

```json
{
  "mcp": {
    "servers": {
      "docs": {
        "transport": "streamable-http",
        "url": "https://docs.example.com/mcp",
        "credentialRef": "docs-token",
        "allowedTools": ["fetch", "search"]
      },
      "local": {
        "transport": "stdio",
        "command": "/usr/bin/mcp-local",
        "args": ["--verbose"]
      }
    },
    "capabilities": {
      "coding": {
        "protocol": "responses",
        "requires": ["mcp-tools"]
      }
    },
    "grants": {
      "dev": {
        "capability": "coding",
        "servers": {
          "docs": ["search"]
        }
      }
    },
    "gateway": {
      "enabled": true,
      "requireAuthenticatedWorkload": true,
      "upstreamTimeoutSeconds": 30
    }
  }
}
```

### Servers

A server has exactly one transport.

| Field | Meaning |
| --- | --- |
| `transport` | `streamable-http` for a remote server, `stdio` for a workload-local server. No other value is accepted. |
| `url` | The Streamable HTTP endpoint. Required for `streamable-http`, rejected for `stdio`. |
| `command` | The absolute executable path. Required for `stdio`, rejected for `streamable-http`. |
| `args` | Arguments of the workload-local process. Rejected for `streamable-http`. |
| `credentialRef` | An opaque reference to protected state that holds the upstream credential. |
| `allowedTools` | The server's declared tool surface. |

Server names are lowercase identifiers (`[a-z0-9][a-z0-9_-]*`, at most 63
characters). Workload identities are lowercase identifiers
(`[a-z0-9][a-z0-9_.-]*`, at most 64 characters). Capability profile names
follow the workload pattern. Tool names are
`[A-Za-z0-9][A-Za-z0-9_.-]*`, at most 128 characters, because a tool name
is defined by the server and is not an Aginctus identifier.

All of these are restricted to lowercase where Aginctus owns the name, so
that a name cannot be ambiguous under a case-insensitive comparison, and
none can contain a path separator.

`allowedTools` has two distinct meanings:

- **absent** — the server declares no server-level restriction;
- **present** — the server's tool surface is exactly the listed tools. An
  empty list permits no tool at all.

The distinction is preserved everywhere, including in the redacted policy
report.

### Grants

`mcp.grants.<workload>.servers.<server>` is a list of tool names. The
presence of the key is the server grant; the list names the tools.

- A workload without a grant has no MCP access.
- A grant that names no tool grants no tool.
- There is no wildcard. A server grant never authorizes a tool the workload
  did not name.

`mcp.grants.<workload>.capability` names the capability profile the workload
requires. It is required when the workload holds server grants, and the
profile must require `mcp-tools`.

### Capability profiles

A capability profile declares the model-facing protocol and the capability
tokens a workload requires. Profiles grant nothing by themselves.

| Token | Meaning |
| --- | --- |
| `mcp-tools` | The workload requires centrally enforced MCP tool calls. Required by any workload that holds server grants. |
| `provider-hosted-tools` | The workload requires tools that the model provider executes. |

`provider-hosted-tools` is a distinct capability and is never treated as MCP
access. Provider-hosted tool calls do not reach the Aginctus gateway, are
not authorized by a grant, and are not covered by any Aginctus network
control. A workload that requires only `provider-hosted-tools` holds no MCP
server grants.

Any other capability token — `resources`, `prompts`, `sampling`,
`elicitation`, `roots` and the like — is rejected explicitly. Those
features need policy that is not implemented, and silently ignoring them
would misrepresent what is enforced.

### Gateway intent

`mcp.gateway` declares the intent for the central gateway MCP endpoint:

| Field | Meaning |
| --- | --- |
| `enabled` | The endpoint is intended to be exposed and to route the declared remote servers. |
| `requireAuthenticatedWorkload` | The gateway must bind requests to an authenticated workload identity. This cannot be disabled. |
| `upstreamTimeoutSeconds` | The bound on one upstream MCP call, between 1 and 300. |

The declaration and the declared remote servers must agree: a remote server
requires the gateway intent, and the gateway intent requires a remote server
to route. Either inconsistency is rejected.

This option states an intent only. It does not create an endpoint, a
listener, a route or a workload, and it does not start a process.

## Effective access

For a workload, a server and a tool, effective access is the intersection of
the server's own tool surface and the workload's explicit tool grant.

```text
effective(workload, server, tool) =
    tool ∈ workload.grants.servers[server]  and  tool ∈ server.allowedTools
```

where `server.allowedTools` is unrestricted when the field is absent.

Validation rejects a configuration whose grant falls outside a declared
server allowlist, rather than narrowing it silently. Validation also rejects
a grant for an unknown server, a grant for a workload that the configuration
does not declare, a grant that names an unknown capability profile, and a
capability profile that does not require `mcp-tools` for a workload that
holds grants.

The resolved policy is deterministic: every collection is ordered, and every
accessor returns a defensive copy, so a caller cannot mutate shared state.

## Credential safety

No plaintext upstream credential can enter declarative configuration.

- A `credentialRef` is an opaque lowercase identifier. A value that
  resembles a well-known credential format, or that is not a plain
  identifier, is rejected.
- A remote endpoint rejects user information, fragments and
  credential-bearing query parameters. Plain HTTP is accepted only for
  loopback hosts.
- A workload-local command must be an absolute path without shell
  metacharacters. An argument that carries credential material inline is
  rejected; a path to a protected file is accepted.
- There is no `headers` field and no `env` field. Unknown fields are
  rejected rather than ignored, so a credential-bearing key can never reach
  the policy.
- Validation never reads protected credential contents and never echoes a
  rejected value.

The same rules are enforced twice. The NixOS and Home Manager modules reject
a credential-bearing field during evaluation, and the configuration loader
rejects it at the source boundary, before any merge. The source boundary
matters because the loader deep-merges objects: a later override that
replaces part of a server declaration does not remove the other fields of the
earlier one.

## Merge and precedence behavior

The MCP declaration uses the ordinary configuration precedence. The existing
deep-merge behavior interacts with named servers and grants in three ways
that matter:

| Declaration shape | Merge behavior | Consequence |
| --- | --- | --- |
| Object, for example `mcp.servers.docs` | Merged field by field | A higher-precedence source that changes one field keeps the other fields of the lower-precedence declaration. Changing `transport` leaves the previous transport's field behind and fails validation until the whole declaration is restated. |
| List, for example `mcp.grants.dev.servers.docs` | Replaced wholesale | A higher-precedence source narrows the granted tools; it cannot extend them by merging. An empty list revokes every tool of that server. |
| `null` | Replaces the value | The revocation marker for `mcp.servers.<name>`, `mcp.capabilities.<name>`, `mcp.grants.<workload>` and `mcp.grants.<workload>.servers.<server>`. |

An empty object cannot withdraw an inherited declaration, because the loader
merges objects. Use `null` to withdraw one.

## Declarative configuration

The NixOS module and the Home Manager module expose the same typed
declarations under `settings.mcp`:

```nix
services.aginctus.settings.mcp = {
  servers.docs = {
    transport = "streamable-http";
    url = "https://docs.example.com/mcp";
    credentialRef = "docs-token";
    allowedTools = [ "fetch" "search" ];
  };
  capabilities.coding = {
    protocol = "responses";
    requires = [ "mcp-tools" ];
  };
  grants.dev = {
    capability = "coding";
    servers.docs = [ "search" ];
  };
  gateway.enabled = true;
};
```

The `mcp` subtree is strict: an unknown field is an evaluation error, so a
credential-bearing key cannot reach the generated configuration file. The
rendered configuration contains only the fields that belong to the declared
transport.

Defaults do not create grants. A host that declares no MCP policy renders no
MCP section at all, so an existing v0.1 configuration keeps its exact shape.

## Policy inspection

`Policy.Report()` returns a deterministic, redacted view of the effective
policy. The report states which servers are declared, which servers each
workload may call, and with which tools. It never contains credential
material and never contains a credential reference; it reports only whether
a server declares one.

The report is the intended contract for the gateway and OpenCode adapters
that consume the policy. Its field names are pinned by a golden fixture and a
test, so a change is a reviewed contract change.

## Enforcement limitations

A correct policy document is not enforcement. The current tree resolves
declared policy and validates it. It does not:

- authenticate a caller or bind a request to a workload identity;
- check a grant at discovery time or at invocation time;
- resolve a credential reference or isolate a credential from a workload;
- filter a discovery response against a workload's grants;
- prevent a workload from reaching an MCP server by another path.

Those belong to the gateway and authorization work tracked in
[#163](https://github.com/blevinn/aginctus/issues/163) and
[#164](https://github.com/blevinn/aginctus/issues/164). Until that work
lands, a policy in the configuration is a statement of intent and nothing
more.

Two further boundaries follow from the design:

- A stdio server is executed inside the authorized workload. The central
  gateway never executes one, and the schema cannot constrain what such a
  process does once it runs.
- A provider-hosted tool call never reaches the Aginctus gateway, so it is
  outside both this policy and any Aginctus network control.

## Related work

- [#162](https://github.com/blevinn/aginctus/issues/162) — this contract.
- [#163](https://github.com/blevinn/aginctus/issues/163) — the central MCP
  gateway and remote server routing.
- [#164](https://github.com/blevinn/aginctus/issues/164) — workload
  identity, grants and secret isolation.
- [#165](https://github.com/blevinn/aginctus/issues/165) — Responses API
  tool passthrough.
- [#166](https://github.com/blevinn/aginctus/issues/166) — OpenCode MCP
  configuration generation.
- [#167](https://github.com/blevinn/aginctus/issues/167) — segmented
  networking for MCP access.
- [AI gateway design](ai-gateway.md) — the wider gateway design and the
  authority boundaries it assumes.
