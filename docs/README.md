# Aginctus documentation

This directory contains project documentation that is too detailed or long-lived for the root README.

## Current documentation

- [Project overview](project-overview.md) — vision, design principles, broad scope, and non-goals.
- [Architecture](architecture.md) — initial component boundaries, workload roles, management networking, SSH identity, and deferred decisions.
- [Milestone 1: Herdr console over managed workloads](milestones/0001-herdr-console.md) — the first usable vertical slice and its success criteria.
- [Development](development.md) — reproducible development shell, tooling, and Incus development notes.

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
