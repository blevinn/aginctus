# Aginctus documentation

This directory contains project documentation that is too detailed or long-lived for the root README.

## Current documentation

- [Project overview](project-overview.md) — vision, design principles, broad scope, and non-goals.
- [Architecture](architecture.md) — initial component boundaries, networking model, workload model, and deferred decisions.
- [Milestone 1: remote Herdr on Incus](milestones/0001-remote-herdr.md) — the first usable vertical slice and its success criteria.

## Architecture decision records

- [ADR 0001: Start with a CLI-first control surface](adr/0001-cli-first-control-surface.md)
- [ADR 0002: Use SSH for the first host-to-workload access path](adr/0002-host-to-workload-access.md)

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
