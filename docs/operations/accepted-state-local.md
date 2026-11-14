# Experimental local accepted-state workflow

**Status:** developer/operator experiment only; not the deployed Aginctus control plane. Tracks [#693](https://github.com/blevinn/aginctus/issues/693), [#695](https://github.com/blevinn/aginctus/issues/695), [#697](https://github.com/blevinn/aginctus/issues/697) and the [#704](https://github.com/blevinn/aginctus/issues/704) authority gate.

This workflow demonstrates a **single Unix-user-owned** accepted workload revision surviving restarts, and explicit candidate→accepted CAS promotion without invoking Incus. The state service is read-only. The separate local acceptance command edits its private same-UID store **directly**, without passing through an authenticated server API. **Do not run this as root in response to Home Manager requests**; do not point it at the pilot host's state path, production Incus volume, or another user's data.

## Build separately, never install as part of ordinary module activation

```sh
nix build --no-link .#state-local
nix build --no-link .#state-service
```

The packages expose `aginctus-state-local` and `aginctus-state`, respectively. They are **not** NixOS/Home Manager service units and are not added to the normal `aginctus` CLI or an MCP tool. These packages add no activation-time daemon, promotion or workload reconciliation. Existing separately enabled reconciliation units retain their current behavior; this experiment does not migrate them to accepted state.

## Explicit local experiment

Use a scratch directory owned by the invoking user, outside any real deployment:

```sh
nix shell .#state-local .#state-service
# Run the remaining commands inside this temporary package shell.
state_dir="$(mktemp -d)"
chmod 700 "$state_dir"
aginctus-state-local init --state-dir "$state_dir/local"
```

Prepare a **nonsecret, manually pinned** `DeploymentSpec` candidate JSON file with `aginctus.dev/accepted/v1alpha1`, `WorkloadDeployment`, and exactly one `WorkloadSpec` per `selectedWorkloads` entry. Each workload must name an immutable lowercase SHA256 image fingerprint and an explicit ordered `profiles` array (possibly empty). Do not put credentials, environment dumps, privileged Incus properties, raw device maps, or secrets in this file.

Use an explicit deployment/project identity for every action:

```sh
aginctus-state-local status --state-dir "$state_dir/local" --deployment demo --project aginctus
aginctus-state-local diff --state-dir "$state_dir/local" --deployment demo --project aginctus --candidate ./candidate.json
```

Nothing changes until explicit acceptance:

```sh
aginctus-state-local accept --state-dir "$state_dir/local" --deployment demo --project aginctus --candidate ./candidate.json --expected-revision none --confirm
```

For later updates, use the **exact active revision ID** reported by `status` in place of `none`. A stale expectation fails under a revision lock instead of overwriting a concurrent acceptance. An identical candidate is a no-op error, not a new revision. On restart, `status` must report the same active revision; `diff` must report no change for an identical candidate. Local CLI output contains revision metadata, explicit deployment/project and workload IDs, and changed field names, not workload configuration values. The read-only service returns the complete typed nonsecret specification to the same UID.

The Unix service can independently expose read-only status to the **same UID** via an owned private socket:

```sh
aginctus-state --state-dir "$state_dir/local" --socket "$state_dir/local/status.sock"
```

It provides only `GET /v1/health` and `GET /v1/active?deployment=demo`. There is **no HTTP promotion endpoint**; stopping the service does not remove accepted records. Stop it gracefully before restarting at the same socket path. An existing socket is never automatically adopted or deleted: after a crash, the operator must verify that no listener owns it before removing that socket path. For service packaging, place the socket in a private ephemeral runtime directory separate from the persistent store. Always use an explicitly selected local store; there is no XDG or root/system fallback.

## Pinned workload inspection and unresolved attempt records (Go integration only)

The internal `acceptedstate.LocalRevisionPin` can now provide an independently
copied, **selected-only** `PinnedWorkloadSnapshot` from its exact immutable
accepted revision (see #814). It rechecks the accepted pointer while holding
the same-UID CAS lock, without consulting the Home Manager candidate file,
environment or mutable image aliases. The pin is not a controller claim,
Incus credential, or permission to run the native SDK.

The companion experimental `RecordLocalUnverifiedAttempt` (see #816/#818)
can persist an immutable, randomly identified, **nonsecret** intention for one
selected workload under the same pin. The private `operations/` directory is
created only on explicit use and retains records across local process death,
store reopen and later accepted revisions. Every such record's outcome is
**`unverified`**. It does not prove an Incus request was sent, that one
succeeded, or that anything was rolled back. A crashed writer does not turn
an unverified record into a completed operation. Recovery must independently
inspect actual authorized resources and reconcile ambiguity under a trusted
controller before any retry.

These are **Go integration primitives only**, not new `aginctus-state-local`
CLI flags, Home Manager services, status endpoints, privileged proxies or
an authenticated operation journal. They do not grant any authority to users
over the NixOS host. The direct CLI above remains read-only for status/diff,
and explicit same-UID `accept` remains a separate non-executing operation.

A future accepted→current executor must first authenticate a project-restricted
principal, prove exclusive resource ownership, pin the accepted revision,
revalidate the policy and Incus resource ETag at the mutation boundary, record
the possible partial outcome **durably before** issuing a write, and verify
current state independently. Until #704/#441/#696 approve this complete
sequence, **do not enable Home Manager image-update or workload-reconcile
units**. See the [controller ownership contract](../design/controller-ownership.md).

## Important limitations and exit criteria for the actual feature

- This accepts only the **workload-only fragment**; mutable image aliases are not converted to accepted immutable image IDs from ambient configuration. Full configured-state resolution, provenance, network/gateway state and opaque secret references remain pending #694/#697.
- A local filesystem path is **not** an authority delegation mechanism. In particular, same-UID CAS does not authorize system/user merges or ownership transfers. Incus project selection over an admin socket does not confine access.
- The service is not yet an independently bootstrapped, authenticated Incus deployment with protected persistent volumes, end-to-end backup/restore or an authenticated operation journal (#695/#696); the separate same-UID unverified-record API above is not that service.
- No accepted→current execution or NixOS/Home Manager automatic reconcile exists here (#698/#699). Nothing should change running Incus resources as a side effect of candidate comparison or promotion.
- State and credentials must not be migrated from a production host based on this experiment (#700). Disposable NixOS/Incus end-to-end acceptance remains #701.

See [ADR 0004](../adr/0004-three-layer-state.md) for the target contract and [Incus authority evidence](../design/incus-project-authority-evidence.md) for the explicit unapproved security boundary.
