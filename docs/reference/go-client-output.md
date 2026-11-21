# Go client and structured output (experimental)

The versioned public Go result projections live in
[`pkg/api`](../../pkg/api), and the in-process client entry point lives in
[`pkg/client`](../../pkg/client). This page documents **implemented**
read-only slices, not the complete
[CLI north star](../design/cli-go-client-output.md).

## Implemented methods

- `client.New(client.Options{PluginInventory: inventory}).Plugins.List(ctx, client.ListPluginsRequest{})`
  produces an `api.PluginList` of compiled descriptors. Discovery is
  independent of environment selection and does not activate plugins.
- `client.New(client.Options{ScopedWorkloadReader: reader}).Workloads.ScopedStatus(ctx, request)`
  produces an `api.ScopedWorkloadStatusList`. It requires a separately
  verified Incus project and a domain reader; project names do **not**
  confer authority. This is transitional single-instance observation,
  **not** the future environment-owned workload service.

The public result schema is `aginctus.dev/v1alpha1`; the embedded
`apiVersion` and `kind` fields identify the document. A plugin's own
`pluginApiVersion` and an accepted-state revision are distinct versions.

## CLI support

```sh
aginctus plugins inventory -o json
aginctus plugins inventory -o yaml
aginctus plugins inventory -o name
aginctus workload ls --project aginctus -o json
aginctus workload status --project aginctus dev -o yaml
```

Only supported paths accept explicit `--output` / `-o`. Other legacy
commands fail on explicit unsupported formats rather than silently
producing the wrong format. Without `-o`, existing legacy output stays
unchanged; it may be unversioned. `name` is a shell projection, not a
machine-document schema. Interactive terminal streams are outside the
JSON/YAML result contract.

## Environment and logical workload API status

The environment reader and logical workload client APIs are designed for an
**injected authenticated Incus-project enrollment and workload authority**.
The production project-scoped accepted-state service is **not yet connected**.
Do not treat local candidate files, arbitrary instance labels, or the legacy
same-UID socket as an authoritative enrollment service.

An embedding host with a verified logical reader can use
`client.New(client.Options{LogicalWorkloadReader: reader}).Workloads.List/Get/Status`.
For CLI integration the host may call `cli.RunWithLogicalWorkloads` with the
same reader. A normal installed CLI without such injection fails closed for
the new read family rather than returning an empty successful inventory.

For now, these typed read commands require **both** an explicit immutable
environment UUID and an Incus project:

```sh
aginctus --environment 11111111-1111-4111-8111-111111111111 \
  --incus-project aginctus workloads list -o json
```

The `agents`, `frontends` and `gateways` aliases use the same typed
workload read service with fixed class expectations. They are not independent
resource stores. Legacy `aginctus workload ...` commands remain separate.

Environment name resolution, default selection from effective configuration,
`environment list/show` command integration, and genuine project-state
discovery require the still-gated authoritative enrollment backend. No new
CLI command enables privilege escalation or reconciles resources.

## Wire compatibility discipline

`internal/cli/output/testdata/` contains v1alpha1 golden JSON documents
for `PluginList`, `ScopedWorkloadStatusList`, and `Error`. Tests
compare complete serialization bytes and verify YAML→JSON semantic parity,
while separate tests require empty collections to serialize as `[]`.

For schema updates:
1. Change public `pkg/api` types intentionally; never derive them
   indiscriminately from `internal/*` domain types.
2. Run `go test ./pkg/api ./pkg/client ./internal/cli/output` and the
   repository's existing validation suites.
3. Treat golden fixture changes as **schema compatibility reviews**,
   not automatic snapshot regeneration. Consider a new `apiVersion`
   for breaking changes.
4. Continue to redact secrets before projecting public data. Serialization
   is not an authorization or sanitization boundary.

These goldens constrain **already implemented** shapes only. Environment
read APIs, state revisions, plans, mutation outcomes, plugin usage and
general CLI output coverage remain separate work. Generated standalone
JSON Schemas and remote transport are deferred.
