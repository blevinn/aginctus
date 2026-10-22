# Project overview

Aginctus provides a small, configurable agent lab on [Incus](https://linuxcontainers.org/incus/). Its current deliverable is a **single-host, CLI-operated** environment: a dedicated Herdr client, a managed OpenCode workload and a private LiteLLM/PostgreSQL gateway with an independently controlled egress path.

The [current architecture](architecture.md) documents what is implemented. [Getting started](getting-started.md) gives the host prerequisites and safe inspection/deployment sequence. The [v0.1 release tracker](https://github.com/blevinn/aginctus/issues/118) records pilot acceptance separately from implemented code; do not assume that gateway provisioning or a Herdr session proves real upstream inference.

## Design principles

- **Explicit ownership:** manage only identified Incus resources by default, reject foreign collisions, make destructive overrides deliberate and visible.
- **Separate trust boundaries:** host/Incus administration, Herdr SSH identity, workload account, scoped gateway key and upstream provider credentials have different authorities.
- **Small, testable contracts:** configuration, plan rendering, runtime discovery and guest helper protocols should be usable without a running pilot for hermetic validation.
- **Portable intent, honest implementation:** use the same high-level workload vocabulary across future backends without claiming that today's NixOS container images work as VMs or on arbitrary guest distributions.
- **Operator-controlled mutation:** distinguish inspection, dry-runs, Incus deployment, credential provisioning, host firewall changes and cost-bearing live model use.

## Implemented v0.1 slice

The Go CLI reconciles an isolated management bridge, Incus guests and Herdr client state, manages an explicit public-key authorization / pinned-host-key / machine-registration sequence, and can deploy a LiteLLM gateway alongside PostgreSQL. The NixOS module supplies declarative image and firewall integration; Incus resource reconciliation and gateway activation are separately opted in. The OpenCode launcher reads its approved LiteLLM alias and workload key at runtime rather than embedding secrets in an image.

Operational details belong in the [architecture reference](architecture.md), [CLI guide](cli.md), [Herdr operations](herdr-machine-registration.md), [OpenCode workload](workloads/opencode.md), and [gateway guidance](ai-gateway.md). Actual end-to-end inference and isolation remain evidence-based acceptance gates.

## Future direction (not current features)

Aginctus may add a shared domain API, dashboard/TUI, multiple Incus projects, safe host workspace sharing, additional agent runtimes and aggregation shells, Kubernetes/remote execution, native Incus reconciliation, model-provider verification, and explicit MCP/tool authorization. These are milestone or design discussions, **not supported CLI commands or deployment guarantees**. The [native reconciliation ADR](adr/0002-native-incus-reconciliation.md) and [future design](native-incus-reconciliation.md) preserve decisions and open tradeoffs without documenting them as shipping behavior.

Aginctus is not intended to replace every Incus management operation, create a new agent protocol, or grant agents ambient host privileges. A management bridge is not a per-workload security boundary, and an implemented configuration path is not proof of a live approved inference transaction.
