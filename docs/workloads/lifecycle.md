# Workload lifecycle

Aginctus models agent workloads as named configuration entries under `workloads.<name>`.

The initial built-in workload is `dev`, configured for the OpenCode runtime:

```json
{
  "workloads": {
    "dev": {
      "instance": { "name": "aginctus-dev" },
      "runtime": { "type": "opencode" },
      "isolation": "container",
      "image": { "alias": "aginctus-opencode-workload" },
      "storage": { "pool": "default" }
    }
  }
}
```

Lifecycle commands are runtime-agnostic:

```sh
aginctus workload dev ensure
aginctus workload dev ensure --dry-run
aginctus workload dev teardown
```

`ensure` evaluates an ordered Apply plan that reconciles the management network
before the workload instance, then ensures the instance is running. `teardown`
deletes only the workload instance and leaves the shared management network intact.

Existing resources must carry the Aginctus workload ownership markers by default.
`--force` explicitly bypasses those ownership markers for adoption or deletion,
but immutable/create-only drift remains fail-closed. In particular, a workload
configured as a container will not silently adopt a same-named VM, and vice versa.

The workload resource currently covers instance lifecycle only. SSH credential
provisioning, address discovery, Herdr machine registration, gateway credentials,
and runtime-specific bootstrap/readiness are separate follow-up layers.
