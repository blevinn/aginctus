# Design proposals

These pages describe proposed or evolving designs, not guarantees of current
runtime behavior. Use [current architecture](../architecture/README.md),
[operations](../operations/README.md) and [testing](../testing/README.md) for
implemented contracts, operator procedures and evidence requirements.

## Core architecture and state

- [Plugin-oriented core, validation and observable orchestration](plugin-oriented-core.md) — source ownership, public extensions, image contracts and incremental delivery.
- [Native Incus reconciliation](native-incus-reconciliation.md) — backend migration and parity gates.
- [Native OCI feasibility](native-oci-feasibility.md) — experimental image/runtime investigation.
- [Three-layer state](three-layer-state.md) — configured, accepted and current state, promotion and recovery.

## Authority and operator integration

- [Home Manager operator](home-manager-operator.md) — standalone and combined operator surfaces.
- [Controller ownership](controller-ownership.md) — lifecycle write-time gates and competing controllers.
- [Incus project authority gate](incus-project-authority-gate.md) — threat model and required negative tests.
- [Incus project authority evidence](incus-project-authority-evidence.md) — evidence scope and disposition.
- [Home Manager delegated authority](home-manager-delegated-authority.md) — delegated operator boundaries.

## Workloads and validation tooling

- [Local models](local-llms.md) — local inference deployment direction.
- [Workspace sharing](workspace-sharing.md) — guest workspace and storage proposals.
- [Test reporting](test-reporting.md) — structured validation reporting design.
- [Testenv Incus backend](testenv-incus-backend.md) — disposable worker architecture.
