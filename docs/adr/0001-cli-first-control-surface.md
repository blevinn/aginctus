# ADR 0001: Start with a CLI-first control surface

- Status: Accepted
- Date: 2026-10-03

## Context

Aginctus ultimately expects to expose an API used by a dashboard, automation, and future TUI clients.

The project does not yet have enough implementation experience to justify choosing an API framework, persistence layer, or long-running control-plane process.

The first usable milestone only requires orchestration from the Incus host.

## Decision

The first executable Aginctus control surface will be an `aginctus` CLI running on the Incus host.

The CLI will call reusable orchestration code rather than embedding all behavior directly in command handlers.

An HTTP API and daemon are deferred until the project has a concrete set of lifecycle operations and state requirements proven by the first milestones.

## Consequences

- The project can reach an end-to-end environment with fewer moving parts.
- Incus integration and domain boundaries can be exercised before an API contract is frozen.
- CLI command design must still operate on Aginctus concepts rather than becoming a generic pass-through to `incus`.
- Core orchestration code should remain separable from terminal presentation so a future API can reuse it.
- Persistence should not be introduced merely to support a daemon that the first milestone does not need.
