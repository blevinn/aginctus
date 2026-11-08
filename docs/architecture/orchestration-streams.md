# Orchestration document streams

`aginctus apply --render` prints one deterministic JSON `Orchestration` document.
This is itself a valid one-document stream, and can be passed back to
`aginctus apply --from FILE` without modification.

`aginctus apply --from FILE` also accepts streams of multiple YAML or JSON
`Orchestration` documents, separated by YAML `---` markers. Each document
must specify `apiVersion: aginctus.dev/v1alpha1`, `kind: Orchestration`,
a common `metadata.name` and a nonempty `steps` array. Each step specifies
an `id`, a registered `driver`, and its `configuration` object.

For example:

```yaml
apiVersion: aginctus.dev/v1alpha1
kind: Orchestration
metadata:
  name: aginctus
steps:
  - id: network
    driver: apply
    configuration:
      operation: upsert
      documents: []
---
apiVersion: aginctus.dev/v1alpha1
kind: Orchestration
metadata:
  name: aginctus
steps:
  - id: workload
    driver: apply
    configuration:
      operation: upsert
      documents: []
```

This example illustrates stream syntax only; use `apply --render` for
complete, deployable resource definitions.

Steps run in file order. The full stream is parsed and structurally validated
before any step executes; the orchestration engine also validates every driver
before execution. Duplicate step IDs, unsupported fields, invalid documents,
and trailing garbage are rejected. The input must be a regular file and is
limited to 16 MiB. A document stream is **not transactional**: if a later
execution step fails, earlier steps may already have changed resources.
Compose credentials remain scoped to the configured gateway project.
