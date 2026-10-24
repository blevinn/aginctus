# Aginctus documentation

This directory contains project documentation that is too detailed or
long-lived for the root README. New documents intended to be
generally discoverable should be linked from this index.

## Start here

- [Getting started](getting-started.md) — prerequisites, NixOS side effects, non-mutating inspection, and explicitly approved deployment.
- [Home Manager operator integration](home-manager.md) — standalone user installation and coexistence with NixOS host firewall policy (v0.2).
- [CLI operations](cli.md) — supported command families, mutation boundaries, dry-run and force semantics.
- [Operator Incus preflight](operator-preflight.md) — read-only, explicitly project-scoped access diagnostics and their limitations.

## Current architecture and component contracts

- [Project overview](project-overview.md) — the supported v0.1 slice and
  longer-term direction.
- [Architecture](architecture.md) — implemented single-host topology,
  runtime state, ownership and trust boundaries.
- [AI gateway](ai-gateway.md) — implemented LiteLLM model gateway and
  separately labeled future MCP/policy design.
- [Declarative orchestration](orchestration.md) — current Jsonnet-generated
  plans and Apply/Compose integration.
- [Orchestration streams](orchestration-streams.md) — plan stream
  validation, ownership and execution ordering.

## Proposed designs (not implemented)

The designs below describe future contracts; their examples are **not**
currently supported configuration or a release acceptance claim.

- [Locally hosted LLMs](design/local-llms.md) — owned inference
  workloads, model-storage volumes and gateway-facing policy.
- [Live host workspace sharing](design/workspace-sharing.md) —
  opt-in bidirectional mount policy and ownership.
- [Unified test reporting](design/test-reporting.md) — proposed cross-suite output contract and opt-in hermetic adapter.\n- [Native Incus reconciliation](design/native-incus-reconciliation.md) —
  staged replacement of the current Apply/Compose drivers after release gates.
- [Home Manager operator / NixOS host authority](design/home-manager-operator.md) —
  proposed v0.2 privilege boundary, safe operator installation and migration gates.

### Architecture (subdirectory)

- [LiteLLM private volume](architecture/litellm-private-volume.md) —
  persistent storage placement and access policy for LiteLLM.
- [Native ChatGPT OAuth](architecture/native-chatgpt-oauth.md) —
  accepted decision on the OAuth credential path (supersedes the
  earlier oauth-gateway-daemon proposal).

### Architecture decision records

- [ADR 0001: Use a dedicated Herdr client container with
  SSH-managed targets](adr/0001-herdr-client-topology.md)
- [ADR 0002: Evolve toward Aginctus-native Incus
  reconciliation](adr/0002-native-incus-reconciliation.md)

## Operations and gateway integration

- [Start here: gateway and egress operator path](gateway-operator-path.md) —
  staged read-only checks, separately authorized network maintenance,
  provider registration, scoped keys, inference evidence and revocation.
- [Gateway egress network](gateway-egress-network.md) — LiteLLM
  egress bridge, OCI NIC, and DHCP design.
- [Gateway API key pilot](gateway-api-key-pilot.md) — operator
  workflow for registering and rotating API-key providers.
- [Gateway workload key pilot](gateway-workload-key-pilot.md) —
  scoped virtual-key lifecycle for pilot inference.
- [Gateway inference smoke](gateway-inference-smoke.md) —
  read-only inference smoke for the gateway path.

## Security and host policy

- [Security controls and limitations](security.md) — implemented boundaries,
  hermetic/VM validation scope, and outstanding live pilot acceptance.
- [Firewall host backends](firewall-host-backends.md) — iptables,
  nftables, and firewalld integration surfaces.
- [LiteLLM firewalld egress](gateway-firewalld-egress.md) — fail-closed host policy, runtime grants and recovery procedures.
- [Firewall pilot validation](firewall-pilot-validation.md) —
  disposable VM validation script for host firewall integration.

## LiteLLM OCI egress

- [LiteLLM OCI egress pilot](litellm-oci-egress-pilot.md) —
  controlled LiteLLM OCI egress pilot rollout and acceptance
  (reconciles with the active #257 work).

## Workloads and images

- [Workload lifecycle](workloads/lifecycle.md) — named workload
  provisioning, isolation, ownership, and teardown.
- [OpenCode workload](workloads/opencode.md) — first agent workload
  image, guest services, and local image workflow.

## Herdr SSH and persistent state

- [Herdr operations and validation](herdr-machine-registration.md) — supported authorization, pinned host trust, strict SSH probe, registration, diagnostics and recovery.
- [Herdr SSH identity contract](ssh-credentials.md) — fixed guest helpers, key locations, trust model and unsupported rotation.
- [Herdr persistent state volumes](herdr-persistent-state-volumes.md) — volume ownership, mounts and separately approved legacy migration.

## Acceptance and validation

- [Disposable NixOS VM harness](vm-harness.md) — experimental
  opt-in Aginctus/Incus VM tests and agent-debugging security limits.
- [Start here: OpenCode acceptance decision path](acceptance-opencode.md) —
  distinguish offline mocks, read-only inspection, owned Herdr lifecycle,
  and separately authorized real provider inference; no implicit sign-off.
- [Acceptance: OpenCode infrastructure preflight](acceptance-opencode-preflight.md) —
  read-only Incus/host resource metadata (not gateway inference).
- [Acceptance: OpenCode Herdr](acceptance-opencode-herdr.md) —
  Herdr-managed workspace lifecycle, including the agent and
  prompt opt-ins.
- [Acceptance: OpenCode gateway readiness](acceptance-opencode-inference.md) —
  read-only guest runtime configuration and key-file preflight (not inference).
- [Acceptance: OpenCode live](acceptance-opencode-live.md) —
  live OpenCode→LiteLLM acceptance and what stays unverified.

## Release tracking (current status)

GitHub release roll-ups, not old repository milestone narratives, own current
scope, accepted evidence and status: [v0.1 / #118](https://github.com/blevinn/aginctus/issues/118),
[v0.2 / #173](https://github.com/blevinn/aginctus/issues/173), and
[v0.3 / #174](https://github.com/blevinn/aginctus/issues/174).
The [release process](releasing.md) describes versioning and human sign-off.

## Development and configuration

- [Release process](releasing.md) — version/tag standards, release gates,
  automated draft creation, and human-controlled publication.
- [Development](development.md) — reproducible development shell,
  tooling, and Incus development notes.
- [Configuration](configuration.md) — configuration sources,
  precedence, environment mapping, and inspection commands.

## Reading convention

Current operator procedures describe supported behavior; proposed designs and milestone descriptions are not proof of implemented features. Dated pilot evidence and the release acceptance checklist are tracked in GitHub issues. Keep generally discoverable new documents linked here, and prefer a single canonical explanation for each behavior.
