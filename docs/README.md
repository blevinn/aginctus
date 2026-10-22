# Aginctus documentation

This directory contains project documentation that is too detailed or
long-lived for the root README. New documents intended to be
generally discoverable should be linked from this index.

## Start here

- [Getting started](getting-started.md) — prerequisites, NixOS side effects, non-mutating inspection, and explicitly approved deployment.
- [CLI operations](cli.md) — supported command families, mutation boundaries, dry-run and force semantics.

## Overview

- [Project overview](project-overview.md) — vision, design
  principles, broad scope, and non-goals.
- [Architecture](architecture.md) — initial component boundaries,
  workload roles, management networking, SSH identity, and deferred
  decisions.
- [AI gateway design](ai-gateway.md) — single-operator LiteLLM
  deployment, inference and MCP policy, CLI authentication flows,
  and MVP acceptance criteria.
- [Locally hosted LLMs](local-llms.md) — proposed managed Incus
  model-serving lifecycle with persistent shared model storage,
  gateway integration, isolation, compatibility, and acceptance
  criteria.

## Architecture decisions and component designs

- [Native Incus reconciliation migration plan](native-incus-reconciliation.md) —
  staged post-v0.1 transition away from Apply/Compose, without v0.2
  completion pressure.
- [Declarative orchestration design](orchestration.md) —
  Jsonnet-generated execution plans with direct Go integration for
  incus-apply and incus-compose.
- [Orchestration streams](orchestration-streams.md) — stream
  semantics, ownership, and ordering during plan execution.
- [Live host workspace sharing design](workspace-sharing.md) —
  workload mount contract, host policy, permissions, lifecycle, and
  tests.

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

## Herdr SSH credential and lifecycle

- [Herdr SSH credential lifecycle](ssh-credentials.md) — proposed
  generation, guest delivery, host trust, rotation, revocation,
  and orchestration integration.
- [SSH bootstrap workflow](ssh-bootstrap-workflow.md) — concrete
  invocation and data flow between the Herdr client identity
  helper, Aginctus host process, and workload authorization helper.
- [Herdr host trust](herdr-host-trust.md) — host trust directory
  ownership, scope, and validation rules.
- [Herdr machine registration](herdr-machine-registration.md) —
  managed-machine registration and lifecycle.
- [Herdr workload authorization](herdr-workload-authorization.md) —
  per-workload authorization, key pinning, and revocation.
- [Herdr persistent state volumes](herdr-persistent-state-volumes.md) —
  scoped state-volume provisioning and reuse.
- [Herdr connectivity validation](herdr-connectivity-validation.md) —
  remote Herdr CLI reaches the managed server through strict SSH.

## OAuth and native ChatGPT

- [OAuth gateway daemon (superseded)](oauth-gateway-daemon.md) —
  earlier daemon proposal; see
  [Native ChatGPT OAuth](architecture/native-chatgpt-oauth.md) for
  the accepted decision.

## Acceptance and validation

- [Acceptance: OpenCode preflight](acceptance-opencode-preflight.md) —
  read-only host infrastructure preflight before launch.
- [Acceptance: OpenCode](acceptance-opencode.md) — OpenCode
  workload lifecycle acceptance gates.
- [Acceptance: OpenCode Herdr](acceptance-opencode-herdr.md) —
  Herdr-managed workspace lifecycle, including the agent and
  prompt opt-ins.
- [Acceptance: OpenCode live](acceptance-opencode-live.md) —
  live OpenCode→LiteLLM acceptance and what stays unverified.

## Milestones

- [Milestone 1: Herdr console over managed workloads](milestones/0001-herdr-console.md) —
  the first usable vertical slice and its success criteria.
- [v0.2 milestone: controlled workload access](milestones/0002-v0.2.md) —
  network segmentation and live host workspace sharing release
  goals.

## Development and configuration

- [Release process](releasing.md) — version/tag standards, release gates,
  automated draft creation, and human-controlled publication.
- [Development](development.md) — reproducible development shell,
  tooling, and Incus development notes.
- [Configuration](configuration.md) — configuration sources,
  precedence, environment mapping, and inspection commands.

## Reading convention

Current operator procedures describe supported behavior; proposed designs and milestone descriptions are not proof of implemented features. Dated pilot evidence and the release acceptance checklist are tracked in GitHub issues. Keep generally discoverable new documents linked here, and prefer a single canonical explanation for each behavior.
