# CLI operations guide

The installed `aginctus help` and `aginctus help <command>` are the source of truth for supported syntax. Help is available without loading the host's configuration. This guide explains **effects and boundaries**, not a duplicated flag-by-flag manual. Check [configuration precedence](configuration.md) and [security limitations](security.md) before provisioning.

## Read-only inspection and planning

| Surface | What it establishes | What it does not establish |
| --- | --- | --- |
| `aginctus help`, `version` | Available syntax and build identity | Host readiness |
| `aginctus config get PATH`, `config show` | Effective local configuration | Whether deployments are safe; review output before sharing |
| `aginctus doctor` | Incus daemon and configured loopback HTTPS listener | Real service reachability or firewall isolation |
| `aginctus status`, `network status`, `herdr client status`, `workload ID status`, `gateway status` | Selected observed state | Successful model inference |
| `aginctus gateway validate`, `gateway render` | Gateway input and rendered Compose intent | Application health, egress or authenticated provider access |
| `aginctus apply --render` | Offline deterministic orchestration plan | Executed reconciliation |
| `aginctus network apply --dry-run`, `herdr client apply --dry-run`, `workload ID apply --dry-run` | Non-mutating planned lifecycle operations | Ownership of an uninspected remote environment |

Some gateway egress `plan`, `preflight`, `audit`, `model render` and `credential-storage audit` commands are also read-only. A proposal or ownership audit is **not** a network address reservation, an authenticated request or permission to activate host policy. Use `aginctus help gateway` for the currently supported forms.

## Commands that change state

| Surface | Side effect or prerequisite |
| --- | --- |
| `aginctus doctor --fix` | Configures the Incus HTTPS listener **only if absent**, using a loopback address |
| `aginctus network apply/remove` | Creates/reconciles or removes a named management bridge |
| `aginctus herdr client apply/remove/start/stop` | Changes the managed client container lifecycle |
| `aginctus workload ID apply/remove/start/stop` | Changes the selected managed workload lifecycle |
| `aginctus herdr authorize/pin-host/register ID` | Changes workload SSH authorization, client trust and/or saved Herdr machines; these are deliberate bootstrap steps |
| `aginctus images update NAME PATH` | Imports/updates a local Incus image alias; running instances are not automatically replaced |
| `aginctus apply`, `aginctus apply --from FILE` | Executes orchestration; `--from` validates a YAML/JSON stream then **executes**, not previews |
| `aginctus gateway apply` (`up` alias), `gateway remove` | Deploys or removes gateway instances; removal retains persistent volumes |
| `aginctus gateway credential-storage ensure/attach` | Changes owned storage/attachments; audit is a separate read-only command |
| `aginctus gateway provider/model ... register` | Writes **local** provider/model registry metadata, not upstream authentication or LiteLLM activation |
| `aginctus workload gateway provision ID --model=ALIAS --key-file=PATH` | Sends an already authorized workload key through protected guest-helper stdin; does not verify inference |
| `aginctus network firewall reconcile`, `gateway egress dhcp apply`, `gateway egress reconcile --confirm-stopped-instance` | Changes host filtering or guarded Incus network/NIC state; follow the dedicated operator procedures and required confirmations |

`--force` is **not a general safety bypass** and differs by command; it can relax specific ownership checks or permit a removal. Never apply it simply to fix an unknown ownership failure. `--dry-run` is supported only where help lists it: notably **gateway apply has no dry-run**, so use `gateway validate` and `gateway render` for offline review. Treat read-only diagnostics as potentially sensitive when their outputs include configuration or path metadata.

## Typical progressive checks

For a configured, approved host:

```sh
aginctus config get incus.management.network.name
aginctus doctor
aginctus status
aginctus gateway validate
aginctus gateway render
aginctus network apply --dry-run
```

On a separately approved deployment, perform the relevant operations from [getting started](getting-started.md), then inspect runtime status. For Herdr use [managed machine registration](herdr-machine-registration.md); for approved gateway keys and authenticated smoke tests use the [gateway key workflow](gateway-workload-key-pilot.md) and [inference smoke](gateway-inference-smoke.md). Passing the above checks does **not** close the [v0.1 release gate](https://github.com/blevinn/aginctus/issues/118).

## Development and errors

Run `nix develop` and `just check-all` for hermetic developer validation. Operation failures should be addressed at the failing ownership, configuration, network, credential or application-readiness stage; avoid widening host routing/NAT or disabling SSH host verification to make a test green.

For exact syntax:

```sh
aginctus help network
aginctus help gateway
aginctus help herdr
aginctus help workload
aginctus help apply
```
