# Configuration reference: sources and host integration

Aginctus configuration is assembled from multiple sources with deterministic precedence. This page owns values, precedence, NixOS/Home Manager integration and their authority boundaries; [CLI operations](cli.md) owns command side effects, while installed `aginctus help` owns syntax.

From highest priority to lowest:

1. command-line overrides passed with `--config=path=value`;
2. an explicit command-line configuration file passed with `--configuration-file=path`;
3. environment variables prefixed with `AGINCTUS_`;
4. the user configuration file;
5. the system configuration file;
6. built-in defaults.

Higher-priority sources override lower-priority sources.

## Command-line overrides

Use dotted paths:

```sh
aginctus --config=z.b.c=xyz config get z.b.c
```

The `--config` option may be repeated:

```sh
aginctus \
  --config=incus.management.network.name=my-management \
  --config=incus.management.network.ipv4.nat=false \
  config show
```

Override values are parsed as JSON when possible. This means values such as `true`, `false`, `42`, arrays, and objects retain their JSON types. Values that are not valid JSON are treated as strings.

`--configure` remains accepted as an alias for `--config`.

## Command-line configuration file

Use:

```sh
aginctus --configuration-file=abc.json config show
```

The file must contain a JSON object. Its values override environment, user, system, and default configuration.

## Environment

Environment variables use the `AGINCTUS_` prefix. Underscores after the prefix map to dots in the configuration path, and names are normalized to lowercase.

For example:

```sh
export AGINCTUS_Z_B_C=xyz
```

sets:

```text
z.b.c = "xyz"
```

Similarly:

```sh
export AGINCTUS_INCUS_MANAGEMENT_NETWORK_NAME=lab-mgmt
```

sets `incus.management.network.name`.

As with `--config`, environment values are parsed as JSON when possible and otherwise treated as strings.

### Gateway providers and catalogs

`gateway.providers` declares named instances of compiled plugins, with nonsecret
`config` containing models, MCP endpoints and plugin-supported private state paths.
`gateway.catalogs` selects those assets and explicitly mapped inherited aliases.
The NixOS and Home Manager modules expose the same strict typed options under
`settings.gateway`. Each named provider/catalog is replaced atomically across
configuration sources. Inspection consumes declarations directly; it does not
copy them into local registries or publish runtime routes. See
[Gateway plugins, providers and catalogs](gateway-assets.md#declarative-nix-configuration)
for the schema, ownership rules and examples. MCP grants remain under `mcp` and
are not derived from catalog membership.

### Runtime gateway secrets

The gateway PostgreSQL password, LiteLLM master key, and LiteLLM salt key are **runtime-only** values, not Aginctus configuration settings. The paths `gateway.postgres.password`, `gateway.master.key`, and `gateway.salt.key` are rejected in all configuration files (system, user, and explicit CLI), `--config` overrides, and object-valued environment mappings, including nested JSON objects. This also applies when a prohibited key is set to `null`.

The standalone environment variables `AGINCTUS_GATEWAY_POSTGRES_PASSWORD`, `AGINCTUS_GATEWAY_MASTER_KEY`, and `AGINCTUS_GATEWAY_SALT_KEY` remain available to the gateway's existing runtime secret-initialization mechanism. The configuration loader deliberately excludes these direct variables from `config show` and `config get`. Do not place their values into configuration files, NixOS `settings`, object-valued overrides, CLI arguments, `systemd.services.*.environment`, inline unit `Environment=`, or generated Nix files: Nix store and generated unit content are not secret stores. For NixOS reconciliation, leave these variables unset and use generated persistent state as described below. External pre-seeding integration is deferred; the module provides no secret-input option.

## User configuration file

On Linux, the default user configuration file is:

```text
~/.config/aginctus/config.json
```

Aginctus uses the platform user configuration directory, so `XDG_CONFIG_HOME` is respected when set.

A missing user configuration file is not an error.

## System configuration file

The system configuration file is:

```text
/etc/aginctus/config.json
```

A missing system configuration file is not an error.

## JSON merging

Configuration files are merged recursively by object key. A higher-priority scalar, array, or object replaces the lower-priority value at the same path unless both values are JSON objects, in which case their keys are merged recursively.

For example, a system file may define:

```json
{
  "incus": {
    "management": {
      "network": {
        "name": "aginctus-mgmt",
        "ipv4": {
          "nat": false
        }
      }
    }
  }
}
```

and a user file may override only the name without repeating the rest of the object.

## MCP declarations

The `mcp` subtree holds the normalized MCP configuration: named servers and
their transports, opaque credential references, explicit per-workload server
and tool grants, model and protocol capability requirements, and the central
gateway MCP intent.

Each named server at `mcp.servers.<name>` is atomic: a source that supplies
it replaces that server's complete lower-priority definition. Adding a new
server preserves existing servers and grants. Other MCP objects follow the
ordinary recursive merge and source precedence.
Environment variables and repeated CLI overrides are grouped into one
source overlay, so one source can construct a complete server using several
paths.

An empty `mcp` object leaves inherited entries unchanged; `mcp: null` clears
the complete MCP policy. A partial server override does not inherit missing
fields and is rejected if incomplete. Existing grants remain bound to server
names. The generated NixOS configuration takes precedence over same-named
ambient servers, but does not clear other ambient policy. A privileged gateway
must select authoritative sources rather than trust a user-context merged
configuration.

The complete contract, including the credential rules, the effective grant
semantics and the limits of declared policy, is documented in
[MCP configuration](mcp-configuration.md). A configuration without an `mcp`
key declares no MCP policy and authorizes nothing.

## Selected Incus project (v0.2 foundation)

`incus.project` is the declarative, single-deployment **target** for
v0.2 Incus project isolation. It defaults to `aginctus` in the Go
config loader, the NixOS module (`services.aginctus.settings.incus.project`)
and Home Manager (`programs.aginctus.settings.incus.project`).

The selected project is visible in ordinary, read-only config inspection:

```sh
aginctus config get incus.project
aginctus --config=incus.project=aginctus-lab config get incus.project
```

The value must be a nonempty Incus project name containing 1–100 ASCII
letters, digits, dots, underscores or hyphens, beginning with a letter
or digit. The special Incus project `default` is **not** a valid
Aginctus deployment target. The Go loader validates the effective
value **after** system, user, environment, explicit file and CLI
override precedence is resolved. A mistyped, malformed, or
`default` selection fails before host-facing commands execute. The
NixOS/Home Manager options validate the same name constraints during
module evaluation. For example, `AGINCTUS_INCUS_PROJECT` selects
this setting through the regular environment override mechanism.

**Important transition boundary:** This setting is a **declared target**,
not a migration trigger. The opt-in, read-only `images list|ls --project
NAME` command consumes it after verifying the exact ownership and
feature policy of the **local** Incus project. Existing v0.1 `apply`,
`remove`, `status`, `images update`, gateway Compose and other Incus
callers have **not** yet been redirected to the selected project.
In particular, `config get incus.project` does **not** prove that a
running instance, alias, volume or gateway belongs to that project.
Do not use the setting to initiate live migration or grant additional
operator privileges.

For one existing workload in an approved disposable environment,
`aginctus workload apply --project aginctus dev --confirm-apply` explicitly invokes
native config/description reconciliation and ensures running. It requires the
configured project, verified ownership/image identity, the project writer lease,
and absent known lifecycle controllers. The command cannot create missing
instances or change devices/profiles/storage/workspaces. `--dry-run` remains
read-only and cannot be combined with confirmation. See
[confirmed scoped apply](cli.md#reconcile-one-existing-scoped-workload-explicit-mutation)
for partial-failure and authority limits. This does not enable NixOS/Home Manager
controllers or migrate the pilot.

For example, on an authorized local operator account:

```sh
aginctus images list --project aginctus
aginctus images ls --project=aginctus
```

These calls verify that `incus.project` equals the explicit flag, then
inspect the local project policy and list its image aliases through
`incus --force-local --project NAME image list --format json`. Foreign,
drifted, inaccessible or not-visible project state fails closed. The
legacy `images list` (no flag) and `images update NAME PATH` remain
ambient-project v0.1 operations.

The separate **explicit create-only** `images import` writes a **new**
image alias into the declared local project, if that alias does not
already exist:

```sh
aginctus images import --project aginctus my-test-image /path/to/nix-image --confirm-create
```

This is a real mutation. Use it only on an authorized disposable Incus
host until real project integration tests have passed. It requires an
existing, exactly owned/policy-compatible local project; before the
import it checks both that policy and target alias absence **twice**,
and uses `incus --force-local --project NAME image import ... --alias
ALIAS` **without** `--reuse`. Incus must reject any concurrent
same-name alias creation, not silently overwrite another writer. After
import, the command verifies the local project again and requires the
alias to point to the expected SHA-256 fingerprint derived from the
metadata and rootfs files. If the server refuses creation or the
post-import result is uncertain, it reports an error and **never deletes
or rolls back** potentially shared resources.

This initial writer cannot replace/adopt any existing alias, even one
that looks Aginctus-owned: image-alias provenance, concurrent updater
exclusion, mutation-time authority and scoped rollback remain open.
Existing `images update` and its systemd units are **not** redirected.
No active workloads, gateway instances, persistent volumes, Linux
bridges or NixOS/Home Manager owners are migrated by selecting a project.

The earlier `operator project-plan --project NAME` and
`operator project-ensure --project NAME --confirm-create` are referenced
here as future v0.2 project-isolation surfaces; they are **not yet
implemented** in the v0.1 CLI. `operator project-plan` is read-only;
`operator project-ensure --project NAME --confirm-create` may create a
fresh, explicitly selected **local** project, but does not move or
manage resources. Full explicit project propagation remains in
[#200](https://github.com/blevinn/aginctus/issues/200), gateway
co-location in [#201](https://github.com/blevinn/aginctus/issues/201)
and reversible v0.1 migration in
[#202](https://github.com/blevinn/aginctus/issues/202).
Until then, retain the existing project layout and avoid changing the
operator's ambient Incus selection as a workaround.

## Management network reconciliation backend

`incus.management.network.backend` selects how the Aginctus management
bridge is reconciled. It defaults to `legacy` in the Go config loader, the
NixOS module (`services.aginctus.settings.incus.management.network.backend`)
and Home Manager (`programs.aginctus.settings.incus.management.network.backend`).

- `legacy` drives the existing `incus-apply` backend.
- `native` drives the Aginctus-owned Incus SDK reconciler.

Exactly one backend reconciles the bridge. The setting is read once per
lifecycle operation and is shared by every plan that reconciles the bridge
(`network ensure`, `network remove`, the aggregate plan, the Herdr client,
the workload plan and the gateway deployment), so a single invocation never
mixes the legacy and the native reconciler.

That consistency is per invocation, not across processes. Aginctus holds no
shared lock or lease on the bridge, so two concurrent invocations with
different effective settings — for example one using the documented
`--config=incus.management.network.backend=native` override — can still
reconcile the same bridge at the same time. While the native backend is
being trialled, do not run a legacy and a native reconcile against the same
bridge concurrently.

```sh
aginctus config get incus.management.network.backend
aginctus --config=incus.management.network.backend=native network ensure
```

The native backend observes the bridge over the local Incus Unix socket,
refuses a bridge that does not carry the Aginctus ownership markers
unless `--force` explicitly authorizes adoption, preserves unmanaged Incus
configuration keys and the operator-supplied network description, and never
mutates anything during `--dry-run`.

The DHCP gateway announcement is immutable policy. Aginctus writes it only
when it creates the bridge; an existing bridge must already carry the
configured announcement, whether it is owned, foreign or being adopted.
`--force` authorizes the ownership markers only: it never rewrites the
announcement, and it never authorizes removing a network of another type.

The backend is an opt-in transition control for
[#153](https://github.com/blevinn/aginctus/issues/153) and
[#157](https://github.com/blevinn/aginctus/issues/157). It does not migrate
existing deployments, does not change the selected Incus project and does
not grant additional operator privileges.

## Gateway egress DHCP input

`gateway.egress.network.ipv4.dhcp` is optional. Omitting it preserves the
default enabled DHCP behavior without an explicit pool restriction. When set,
it accepts either a legacy boolean (`true` enables DHCP, `false` disables it)
or a nested object with a required boolean `enabled` and optional string
`ranges`. No other object members are accepted. Missing or mistyped `enabled`,
non-string `ranges` (including `null`), and unknown members are errors before
an orchestration plan is returned.

For example, this JSON restricts the egress bridge's DHCP pool:

```json
{
  "gateway": {
    "egress": {
      "ipv4": { "address": "10.42.40.25/24" },
      "network": {
        "ipv4": {
          "address": "10.42.40.1/24",
          "dhcp": {
            "enabled": true,
            "ranges": "10.42.40.100-10.42.40.199"
          }
        }
      }
    }
  }
}
```

Omitted or empty `ranges` means no explicit pool restriction. Nonempty ranges
require enabled DHCP, explicit bridge and LiteLLM IPv4 CIDRs, and a valid pool
that excludes the bridge, subnet and LiteLLM static addresses.
Disabled DHCP requires both explicit CIDRs; it cannot be combined with a
nonempty range.

JSON keys along the DHCP input path must be nested: literal dotted keys such
as `"dhcp.ranges"` beside `"dhcp"`, or `"ipv4.dhcp"`, are rejected even without
a colliding nested value. Dotted CLI override paths and environment mappings
still construct nested objects; set `dhcp.enabled` explicitly when setting
`dhcp.ranges`. Nix attribute paths without quotes likewise produce nested JSON.
This restriction is local to Aginctus DHCP input, not a global config-key rule.
Rendered Incus resource keys remain literal `ipv4.dhcp` (set to `"false"` when
disabled, otherwise omitted to retain Incus's enabled default) and
`ipv4.dhcp.ranges` (the exact nonempty pool string).

## Declarative client header forwarding

For OpenCode Go native Responses, use the default-off
[`gateway.litellm.selectiveGoForwarding` policy](../operations/gateway/selective-go-forwarding.md).
It requires an immutable LiteLLM digest and rejects simultaneous stock global
or nonempty model-group forwarding. The stock option below remains a
compatibility interface; actual pinned transport auditing rejects it for Go
because it forwards unrelated metadata and loses the original User-Agent.

`gateway.litellm.forwardClientHeaders` accepts a boolean or an array of
model-group strings in NixOS, Home Manager, JSON files, and CLI overrides.
It defaults to **false**:

| Value | LiteLLM policy |
| --- | --- |
| `false` or `[]` | No forwarding policy or config-file override is declared. |
| `true` | `general_settings.forward_client_headers_to_llm_api: true`, for all models. |
| A nonempty string array | `litellm_settings.model_group_settings.forward_client_headers_to_llm_api`, for those model groups only. |

The array selects **model groups, not header names**. Exact aliases and
LiteLLM model-group wildcard patterns are supported. An array replaces the
global mode; it cannot express per-model exclusions from global forwarding.
Provider-wide selection requires matching model-group names, not merely a
provider identifier. Empty/whitespace-only names, control characters, and
dollar signs are rejected; the latter prevents Compose environment expansion
inside model names. Other JSON types and mixed-type arrays are rejected.

The Go preview and Jsonnet deployment plan declare the same stock LiteLLM
YAML through inline Compose `configs`, attached only to LiteLLM at
`/app/aginctus-client-headers.yaml` with matching `CONFIG_FILE_PATH`.
No models or credentials are registered by this setting.

Forwarding admits eligible extra `x-*` headers except `x-stainless*`, and
`anthropic-beta`. It does not preserve client `User-Agent` or translate
session-header names. Provider-auth forwarding is a separate LiteLLM setting
and is not enabled here. Global mode expands eligible-header exposure to
every configured upstream, including models registered later. The #456
metadata-exposure and native transport acceptance gates remain separate.

In NixOS configuration:

```nix
services.aginctus.settings.gateway.litellm.forwardClientHeaders = true;
```

Alternatively, restrict forwarding to selected model groups:

```nix
services.aginctus.settings.gateway.litellm.forwardClientHeaders = [
  "team/*"
];
```

Without changing host or network state, inspect the effective setting and
the rendered Compose using:

```sh
aginctus config get gateway.litellm.forwardClientHeaders
aginctus gateway render
aginctus --config='gateway.litellm.forwardClientHeaders=["team/*"]' gateway render
```

A pilot that previously set `environment.CONFIG_FILE_PATH` directly in Incus
must follow the manual-to-declarative migration and rollback criteria in
[selective forwarding operations](../operations/gateway/selective-go-forwarding.md#migration-activation-and-rollback). Declaring this config is
not permission to reconcile a running gateway or make provider requests.

## Inspecting effective configuration

Print the complete merged configuration:

```sh
aginctus config show
```

Print one value:

```sh
aginctus config get incus.management.network.name
```

These commands apply the same precedence rules used by operational Aginctus commands.

## Initial defaults

The initial defaults reserve configuration for the management network that will be used by the first Incus provisioning slice:

```json
{
  "incus": {
    "management": {
      "network": {
        "name": "aginctus-mgmt",
        "ipv4": {
          "address": "auto",
          "nat": false,
          "routing": false
        },
        "ipv6": {
          "address": "none"
        }
      }
    }
  }
}
```

These are defaults rather than hard-coded orchestration constants. `aginctus network ensure` consumes these effective values when creating or reconciling the management bridge, and `aginctus network teardown` uses the effective network name when removing it, so higher-precedence sources apply consistently to both lifecycle operations.

Both network lifecycle commands support `--dry-run` and `--force`. Dry-run reports the intended mutation without applying it. Force only relaxes the Aginctus ownership check for a same-named Incus-managed bridge; it does not bypass network type checks or Incus API errors.

### Where a default comes from

Two registries describe the same effective configuration: the CLI's `defaults` map in `internal/config/config.go` and the module's typed options in `nix/nixos-module.nix`. The module renders its defaults into `/etc/aginctus/config.json`, which overrides the CLI fallbacks, so a value that differs between the two registries changes behavior depending on which layer supplies it.

`TestDefaultsMatchNixOSModule` (`internal/config/nix_defaults_parity_test.go`) fails when the registries disagree in either direction. The `gateway-config-parity` flake check runs it against the module's actual rendered output; plain `go test ./internal/config` runs it against the committed snapshot in `internal/config/testdata`. Adding a default therefore requires adding it to both registries, or recording it as a CLI-only fallback.

Two keys are CLI-only fallbacks today, listed explicitly in that test:

| Key | Value | Why |
| --- | --- | --- |
| `gateway.credentials.storage.pool` | `default` | Incus pool for the gateway credential volume; the module has no option for it |
| `gateway.egress.network.name` | `aginctus-egress` | Operators set this through the module's freeform JSON layer; the CLI value is the fallback when it is absent |

`firewall.backend` is not a registry default. The module pins it per backend in the generated reconcile unit's command line (`firewalld`, `nixos-nft`, or `iptables-nft`), and the CLI reads it only to refuse a backend the host does not actually provide. Setting it by hand changes which reconciliation path is attempted and fails closed on any mismatch, so prefer the module-generated unit over a hand-written override.


## Herdr client container defaults

The Herdr client uses a locally imported Incus image built by the repository flake:

```json
{
  "infrastructure": {
    "herdr": {
      "name": "aginctus-herdr",
      "image": {
        "alias": "aginctus-herdr-client"
      },
      "storage": {
        "pool": "default"
      }
    }
  }
}
```

`aginctus herdr client ensure` combines these values with
`incus.management.network.name`. The container is created without inheriting
Incus profiles: it receives an explicit root disk on the configured storage pool
and an explicit NIC on the configured management network.

The default image alias is produced by `nix build .#herdr-client` and imported
locally with `just update image herdr-client`. The image is NixOS 26.05 and
consumes Herdr from the published Herdr flake, with the exact revision pinned by
Aginctus's `flake.lock`. A systemd service starts `herdr server` in headless mode
when the container boots.

The image alias remains configurable through the normal Aginctus precedence
rules, allowing a separately built or promoted image to be selected without
changing orchestration code.


## NixOS module

The complete, effectful NixOS host integration and its activation examples
live in the [NixOS module reference](nixos-module.md). In particular,
`services.aginctus.firewall.enable` defaults on, while instance reconciliation
must be separately authorized. Installing or evaluating a module is not evidence
of a complete live deployment. Historical links to this section and the former
subsections are maintained below as navigation stubs rather than duplicated
configuration examples.

### Host firewall reconciliation (default-on)

See the [NixOS integration reference](nixos-module.md#host-firewall-reconciliation-default-on).

### Module evaluation checks

See the [NixOS integration reference](nixos-module.md#module-evaluation-checks).

### Image lifecycle validation

See the [NixOS integration reference](nixos-module.md#image-lifecycle-validation).

### Reconciliation on NixOS generation changes

See the [NixOS integration reference](nixos-module.md#reconciliation-on-nixos-generation-changes).

### Image-import service activation on CLI updates

See the [NixOS integration reference](nixos-module.md#image-import-service-activation-on-cli-updates).

### NixOS integration acceptance checklist (issue #67)

See the [NixOS integration reference](nixos-module.md#nixos-integration-acceptance-checklist-issue-67).
