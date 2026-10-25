# CLI operations guide

The installed `aginctus help` and `aginctus help <command>` are the source of truth for supported syntax. Help is available without loading the host's configuration. This guide explains **effects and boundaries**, not a duplicated flag-by-flag manual. Check [configuration precedence](configuration.md) and [security limitations](security.md) before provisioning.

## Read-only inspection and planning

| Surface | What it establishes | What it does not establish |
| --- | --- | --- |
| `aginctus help`, `version` | Available syntax and build identity | Host readiness |
| `aginctus config get PATH`, `config show` | Effective local configuration | Whether deployments are safe; review output before sharing |
| `aginctus doctor` | Incus daemon and configured loopback HTTPS listener | Real service reachability or firewall isolation |
| `aginctus status`, `network status`, `herdr client status`, `workload status ID`, `gateway status` | Selected observed state | Successful model inference |
| `aginctus gateway validate`, `gateway render` | Gateway input and rendered Compose intent | Application health, egress or authenticated provider access |
| `aginctus apply --render` | Offline deterministic orchestration plan | Executed reconciliation |
| `aginctus network apply --dry-run`, `herdr client apply --dry-run`, `workload apply ID --dry-run` | Non-mutating planned lifecycle operations | Ownership of an uninspected remote environment |

Some gateway egress `plan`, `preflight`, `audit`, `model render` and `credential-storage audit` commands are also read-only. A proposal or ownership audit is **not** a network address reservation, an authenticated request or permission to activate host policy. Use `aginctus help gateway` for the currently supported forms.

The older `workload <name> ensure|teardown` forms remain supported for compatibility; prefer `workload apply|remove [name]` in new examples.

## Explicit local Incus image inventory (read-only)

```sh
aginctus images list --project aginctus
```

The opt-in `images list|ls --project NAME` uses the local Incus Unix
socket, never the selected remote or ambient project. The name must
match the effective `incus.project` config and the local project must
have matching Aginctus ownership markers and required feature policy.
Failing or restricted project inspection is an error, not a silent
fallback to the default project. It does not import or change images.
The no-flag `images list` remains a legacy v0.1 ambient-project view
until migration. See [configuration](configuration.md) for the rollout
boundary.

## Explicit create-only scoped image import (mutation)

On an **approved, disposable local Incus host** with an existing compatible
Aginctus project:

```sh
aginctus images import --project aginctus my-test-image /path/to/nix-image --confirm-create
```

Unlike `images list --project`, this **mutates the local project**.
It accepts only an **absent** alias: the command checks exact project
ownership and feature policy plus alias absence, repeats those checks
before the import, and calls Incus with `--force-local --project`
without `--reuse`. Incus must reject a conflicting alias, including
one created after preflight. It verifies that the resulting alias points
to the expected image fingerprint; if verification fails, it reports
uncertain state without automatically deleting or rolling back an image.
This first write slice **does not update/replace an existing alias**,
move v0.1 images, activate systemd services, or authorize Home Manager
reconciliation. Existing `images update NAME PATH` still uses the legacy
ambient project. See [configuration](configuration.md) for migration limits.

## Image ownership-claim audit (read-only)

```sh
aginctus images owner-audit --project aginctus --owner system my-image
```

This read-only command requires the selected `incus.project`, verifies
ownership of the local Incus project, and checks the requested alias
against image `properties`. The provisional ownership-claim contract
requires `user.aginctus.managed=true`,
`user.aginctus.resource=image`, `user.aginctus.deployment=PROJECT`,
`user.aginctus.alias=ALIAS`, and
`user.aginctus.owner=system|user` with the selected owner value.
It also requires one exclusive alias and an unambiguous full image
fingerprint.

The JSON report distinguishes `claimed`, `absent`, `unmarked`,
`foreign-or-incomplete`, `shared`, `ambiguous` and `unverified`.
A result other than `claimed` exits nonzero. Access and parsing errors
fail closed without displaying raw Incus output. **Even `claimed`
is not write permission or an authenticated attestation**: these
properties can be edited by an actor with appropriate Incus access,
and the audit does not prove which lifecycle controller is currently
active. The current create-only image import does **not** add these
markers, so its images remain unmarked pending a separate provenance
creation contract. Do not mark or adopt existing images as a workaround.

No existing alias is modified by this command. Alias replacement
still requires controller exclusion, provenance evidence and
transaction-safe, explicitly project-scoped rollback.

## Create a newly claimed image (opt-in mutation)

An authorized operator can create a *new*, project-scoped image whose
descriptive properties declare the intended lifecycle owner:

```sh
aginctus images import-owned --project aginctus --owner system my-image /path/to/nix-image --confirm-create
```

This is a **write**, not an ownership handoff or an attestation. It uses
one Incus image-import request with the five
`user.aginctus.{managed,resource,deployment,alias,owner}` properties,
matching the read-only `images owner-audit` contract. It refuses an
existing alias **and** an already-present image fingerprint, including
one with another alias, so it never attempts to rewrite or adopt
existing image metadata. It repeats the project and inventory checks
before importing without `--reuse`, then verifies exclusive alias,
fingerprint and claims. A failed or uncertain import may have left an
image in the store; the command never automatically deletes or
restamps it. Incus's server-side duplicate-fingerprint guard provides
the final protection against a concurrent import of identical content.

The `--owner` argument **does not verify the caller's identity or
which systemd/Home Manager process is in control**. The property claims
are mutable descriptive metadata; they are not authoritative write
permission. Use this command only in an explicitly approved disposable
Incus environment during validation, not on the v0.1 pilot. Keep
`images update`, `images import`, service units and migration paths
unchanged until separate owner arbitration and atomic update/rollback
work is complete.

## Cooperative scoped image-writer exclusion

The two opt-in, create-only commands, `images import --project` and
`images import-owned --project`, now hold the **same project-wide**
Linux abstract Unix socket lease from their initial project inspection
through final import verification. If another participating Aginctus
image writer already holds that lease, the command fails **before**
contacting Incus. Different aliases in the same project still conflict:
they might resolve to an identical image fingerprint. Different
projects have independent lease names.

A lease is automatically released when its process exits, including
after a crash; it needs no writable shared lockfile, NixOS tmpfiles
rule, or global group. On non-Linux platforms, these opt-in writes
fail closed rather than skip the lease. On a host where the system and
user operators share a **network namespace**, this serializes their
participating Aginctus processes even when their UIDs differ.

**This is only cooperative exclusion, never authorization.** A process
holding an abstract socket cannot prove its identity or lifecycle
ownership. Unrelated Incus commands, external controllers, network
namespaces, systemd services running the legacy `images update`,
and concurrent Incus administrators do **not** acquire this lease.
Another local process can intentionally hold its name and deny writes.
The lease therefore does not make `replace-plan` authoritative and
does not enable alias replacement, Home Manager handoff, or live pilot
migration. Host/controller authority and project-scoped atomic
replacement/rollback remain future work.

## Proposed scoped image replacement (read-only)

```sh
aginctus images replace-plan --project aginctus --owner system my-image /path/to/nix-image
```

This command **does not replace an image or authorize doing so**. It
requires the declared, exactly owned local Incus project; reads image
metadata in that project; fingerprints the proposed Nix image artifacts;
and checks that the current alias has an exclusive, complete image
owner **claim**. Existing image content under any other alias cannot
be adopted as a replacement target.

For a changed image, it also surveys the four known image/reconciler
systemd units in the **system manager and current user's manager**.
Installed/active units under the other owner block the plan; missing
or unreadable manager state is also a blocker. These checks cannot
see all users, manually invoked Incus commands, or a competing writer
that starts afterward. Consequently, **every JSON report contains
`"writeAuthorized": false`**, including a
`candidate-requires-authorization` result. Only an exact matching,
claimed image returns `up-to-date`. For an eligible candidate, the
command exits successfully as a *read-only plan*, not as an approval
to run a replacement.

A replacement cannot become active until separately designed host
and lifecycle authority, concurrency control, mutation-time project
and alias rechecks, and project-scoped rollback have passed review
and disposable Incus acceptance. The existing v0.1 `images update`
and new create-only image commands remain unchanged. Do not use
this plan's JSON as a machine authorization token.

## Commands that change state

| Surface | Side effect or prerequisite |
| --- | --- |
| `aginctus doctor --fix` | Configures the Incus HTTPS listener **only if absent**, using a loopback address |
| `aginctus network apply/remove` | Creates/reconciles or removes a named management bridge |
| `aginctus herdr client apply/remove/start/stop` | Changes the managed client container lifecycle |
| `aginctus workload apply/remove/start/stop ID` | Changes the selected managed workload lifecycle |
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
