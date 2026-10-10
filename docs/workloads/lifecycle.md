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

## Isolation and image compatibility

`workloads.<name>.isolation` accepts `container` or `vm`. The selected
`workloads.<name>.image.alias` must refer to an Incus image built for the
same instance type. Aginctus does not automatically convert container images
into VM images or infer VM compatibility from an alias name.

The shipped `aginctus-opencode-workload` default is a **container image**
(metadata plus a squashfs root filesystem); it cannot boot as an Incus VM.
To use `isolation: "vm"`, first build or obtain a VM-capable Incus image,
import it under a separate alias, and configure that alias alongside VM isolation.
VM image provisioning, guest agent and guest service readiness remain operator
responsibilities; the project does not currently ship a default VM image.

Aginctus intentionally accepts custom VM image aliases because their format is
validated by Incus when launched; an alias string alone cannot safely establish
image type. Test a new image on a disposable Incus host before adopting it in a
managed workload.
