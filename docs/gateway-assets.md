# Gateway plugins, providers and catalogs

The Go CLI has a local, nonsecret gateway asset control plane. Cobra owns the
commands, flags, selector validation and nested help. Run
`aginctus gateway plugin --help`, `provider --help`, `model --help`, `mcp --help`
or `catalog --help` under the same `aginctus gateway` prefix for syntax.

## Objects and ownership

- A **plugin** is compiled Go behavior with an identity, naming default,
  configuration validation and optional model-discovery/login hooks. It is not
  a downloadable module or command supplied through configuration. The built-in
  `chatgpt` and `opencode-go` plugins describe the existing LiteLLM registrations;
  they do not implement interactive login.
- A **provider** is one named plugin instance, with its own configuration and,
  when supported by its plugin, an explicit private credential-state directory.
  Multiple instances may use the same plugin. Applying an existing name cannot
  change its plugin or credential-state identity.
- A **model** belongs to a provider. The same model name may occur at multiple
  providers; a show operation must identify its provider when ambiguous.
- An **MCP** is a local declaration or a provider-offered server declaration.
  Both use the existing [MCP schema](../internal/mcpschema/schema.go), including credential
  references, transport validation and tool restrictions. Applying a local MCP
  never starts its command, connects to its URL or grants tools to a workload.
- A **catalog** selects provider models and local/provider MCPs under exposed
  aliases. It can inherit explicitly selected assets from other catalogs and
  rename them, including namespaced aliases such as `dev.coding`.

Catalogs are declarations suitable for reviewing a workload's intended surface.
They do not publish LiteLLM routes, issue keys, install workload configuration or
replace the existing authenticated workload/MCP grant policy. Deployment and
authorization remain separate operations under the
[gateway operator path](gateway-operator-path.md).

## Instantiate and inspect a provider

Commands accept either a positional name or its explicit selector flag; supplying
both is an error. Configuration is a strict JSON object, supplied with `--config`
or `--config-file`, never both. Files must be regular non-symlink files, read with
a 1 MiB bound. They carry public declarations and opaque credential references,
not credentials. Global host configuration overrides do not apply to these local
registry commands.

```sh
aginctus gateway plugin list
aginctus gateway plugin show --plugin chatgpt
aginctus gateway provider apply --plugin chatgpt --provider personal --config-file /private/provider.json
aginctus gateway provider show personal
aginctus gateway model list --provider personal
aginctus gateway model show --provider personal --model coding
```

For the existing `chatgpt` registration plugin, `provider.json` has this shape:

```json
{
  "models": {
    "coding": {"source": "chatgpt/OPERATOR_VERIFIED_MODEL"}
  },
  "mcps": {
    "search": {
      "transport": "streamable-http",
      "url": "https://tools.example.invalid/mcp",
      "credentialRef": "search-session",
      "allowedTools": ["search"]
    }
  }
}
```

The source is an operator declaration, not evidence that the upstream model is
available. Use an independently verified source identifier. `opencode-go` applies
its existing supported-source validation. The native `chatgpt` plugin is not SIWC.

`provider login --provider NAME` dispatches only that plugin's compiled login
hook. Without a selector it requires exactly one registered instance. An absent
hook is an error; it never guesses credentials or falls back to another plugin.
`model list --provider NAME` may invoke that plugin's model discovery hook;
unfiltered listing shows local declarations without upstream authentication.

## MCP declarations and catalog inheritance

```sh
aginctus gateway mcp apply --local --mcp search --config-file /private/search.json
aginctus gateway mcp list
aginctus gateway mcp show --local --mcp search
aginctus gateway mcp show --provider personal --mcp search
aginctus gateway catalog apply --catalog base --config-file /private/base.json
aginctus gateway catalog apply --catalog workstation --config-file /private/workstation.json
aginctus gateway catalog show workstation
aginctus gateway catalog show workstation --model
aginctus gateway catalog show workstation --model=dev.coding
aginctus gateway catalog show workstation --mcp=tools.search
```

`search.json` contains one server object using the schema shown above. Without
`--provider`, MCP show selects a local declaration. Local and provider-offered
servers retain distinct identities even when their names are identical.

`base.json` explicitly selects assets:

```json
{
  "models": {"coding": {"provider": "personal", "name": "coding"}},
  "mcps": {"search": {"local": true, "name": "search"}}
}
```

For a provider-offered MCP use `{"provider":"personal","name":"search"}`
instead of the local reference. `workstation.json` imports and renames selections:

```json
{
  "inherits": [{
    "catalog": "base",
    "models": {"coding": "dev.coding"},
    "mcps": {"search": "tools.search"}
  }]
}
```

Import mappings are **source alias to exposed alias**. An omitted mapping imports
nothing, so adding assets to a parent cannot silently expand a child's surface.
Cycles, missing references and same-kind exposed-name collisions are rejected.
Renaming preserves the original provider/server identity and tool restrictions.
Provider updates cannot remove models/MCPs still referenced by catalogs. Catalog
updates validate the complete inheritance graph before writing.

### Relationship to MCP policy

The `mcp.servers`, `mcp.grants`, `mcp.capabilities` and `mcp.gateway` configuration
contract owns declared MCP authorization policy, as documented in
[the MCP configuration contract PR](https://github.com/blevinn/aginctus/pull/612).
Asset declarations reuse its server schema, not its grants. A catalog alias is
neither a policy server identity nor an authenticated workload identity. Renaming
or inheriting an MCP does not copy grants, resolve credentials, enable routing or
authorize invocation. A runtime adapter must retain the original server identity
and enforce the workload's explicit tool grants and the server restriction.

Provider-offered MCP servers are declarations of MCP endpoints. They are distinct
from `provider-hosted-tools`, which the model provider executes outside the MCP
gateway. Catalogs implement neither of those runtime paths. In particular,
schema validation here does not imply that a remote server has the gateway intent
required for inclusion in effective `mcp` policy.

## Persistence and compatibility

State lives under `${XDG_STATE_HOME:-$HOME/.local/state}/aginctus/gateway/`.
Provider instances extend `providers.json` with typed plugin configuration.
Local MCPs and catalogs live in `assets.json`. Updates use persistent advisory
locks and the existing owned-private-file, atomic-replacement and fsync helpers.
Reads and help do not initialize state. Configuration and provider errors do not
cause automatic login, remote execution or network-policy changes.

Existing `provider register` and `model register/render` forms remain available.
Models in the legacy `models.json` registry are included in the provider view
without copying state; a conflicting new source declaration is rejected. Applying
provider configuration does not remove a legacy model registration. Existing
deployment and workload-key commands retain their activation boundaries.
