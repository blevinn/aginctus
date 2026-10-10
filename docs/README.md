# Aginctus documentation

This directory contains project documentation that is too detailed or long-lived for the root README.

## Current documentation

- [Project overview](project-overview.md) — vision, design principles, broad scope, and non-goals.
- [Architecture](architecture.md) — initial component boundaries, workload roles, management networking, SSH identity, and deferred decisions.
- [Herdr SSH credential lifecycle](ssh-credentials.md) — proposed generation, guest delivery, host trust, rotation, revocation, and orchestration integration.
- [SSH bootstrap workflow](ssh-bootstrap-workflow.md) — concrete invocation and data flow between the Herdr client identity helper, Aginctus host process, and workload authorization helper.
- [AI gateway design](ai-gateway.md) — single-operator LiteLLM deployment, inference and MCP policy, CLI authentication flows, and MVP acceptance criteria.
- [Locally hosted LLMs](local-llms.md) — proposed managed Incus model-serving lifecycle with persistent shared model storage, gateway integration, isolation, compatibility, and acceptance criteria.
- [Declarative orchestration design](orchestration.md) — Jsonnet-generated execution plans with direct Go integration for incus-apply and incus-compose.
- [Milestone 1: Herdr console over managed workloads](milestones/0001-herdr-console.md) — the first usable vertical slice and its success criteria.
- [OpenCode workload](workloads/opencode.md) — first agent workload image, guest services, and local image workflow.
- [Development](development.md) — reproducible development shell, tooling, and Incus development notes.
- [Configuration](configuration.md) — configuration sources, precedence, environment mapping, and inspection commands.

## Architecture decision records

- [ADR 0001: Use a dedicated Herdr client container with SSH-managed targets](adr/0001-herdr-client-topology.md)

## Documentation structure

As the project grows, documentation should be organized by purpose rather than accumulated in the root README.

Expected areas include:

- architecture and component boundaries;
- configuration and workload specifications;
- security and policy models;
- milestones and deployment guidance;
- developer documentation;
- architecture decision records;
- API documentation.

New documentation should be linked from this index when it is intended to be generally discoverable.
