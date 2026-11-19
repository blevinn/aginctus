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
- [Project-scoped state storage](environment-state-storage.md) — accepted-state authority with independent environment revisions.
- [Explicit environment candidate sources](environment-candidate-sources.md) — typed NixOS/Home Manager/standalone declarations without system/user merging.

## CLI design

- [Environment-centric CLI north star](cli-north-star.md) — agreed top-level vocabulary, active environment selection, state introspection, images and explicit plumbing opt-in.
- [Domain-owned CLI migration](cli-domain-ownership.md) — decomposition of existing Cobra operation adapters into domain-owned implementations.
- [Go client and structured CLI output](cli-go-client-output.md) — in-process public API, versioned projection schemas, error semantics, and standard output formats.

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
