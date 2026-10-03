# AGENTS.md

This file defines repository-wide instructions for automated coding agents working on Aginctus.

These rules apply to all files in the repository unless a more specific `AGENTS.md` in a subdirectory adds stricter local instructions.

## Repository governance

### Pull requests are mandatory

All substantive repository changes must happen on a topic branch and be proposed through a pull request.

Agents must not push substantive changes directly to `main` or another protected branch.

### Only humans merge

Agents must never merge pull requests.

This applies regardless of:

- whether the pull request is approved;
- whether required checks pass;
- whether the agent created the pull request;
- whether the agent has technical permission to merge;
- whether merging appears to be the obvious next step.

A human must perform every merge.

Agents may prepare a pull request for merge, address review feedback, summarize its state, and tell a human when it appears ready for review.

### Do not force-push unless explicitly instructed

Agents must not force-push, use `--force`, use `--force-with-lease`, or otherwise rewrite the history of a topic branch unless a human explicitly instructs the agent to do so for that branch.

When responding to review or correcting earlier work, agents should prefer additive commits.

Rebasing or otherwise rewriting branch history is not implicitly authorized by a request to "update the PR", "address feedback", "fix the branch", or similar language.

### Preserve human reviewability

Agents should keep changes scoped to the task being performed and avoid unrelated cleanup.

PR descriptions should explain:

- the purpose of the change;
- the important design decisions;
- how the change was validated;
- known limitations or follow-up work;
- any security-relevant implications.

Security-sensitive changes involving sandboxing, network controls, credentials, secrets, MCP permissions, model access, or host capabilities must be called out explicitly.

## Project principles

Aginctus is a portable and configurable agentic lab built on Incus.

Changes should preserve the following architectural principles unless a pull request explicitly proposes changing them.

### Incus is the execution substrate

Agentic workloads may run in either Incus system containers or Incus virtual machines.

Container and VM support should share the same higher-level workload model wherever practical rather than becoming independent product paths.

### Guest Linux distributions are not an API boundary

The platform should remain agnostic to the Linux distribution used inside a workload.

Do not introduce unnecessary assumptions about package managers, init systems, filesystem layout, or distribution-specific commands into shared control-plane interfaces.

NixOS is a first-class supported guest and should receive a well-designed path from the beginning, but it must not become a requirement for all workloads.

### Agent runtimes are adapters

OpenCode and Hermes are the initial target agent runtimes.

Shared control-plane concepts should not be designed around quirks of either runtime. Runtime-specific behavior belongs behind an adapter or similarly clear boundary.

### Gateways and frontends are replaceable integrations

LiteLLM is the leading initial choice for the AI gateway, and Herdr is a candidate frontend for interacting with deployed agents.

Public configuration and core domain models should avoid unnecessarily hard-coding either implementation where a small abstraction can preserve portability.

### Network and MCP controls are first-class policy

Network reachability and MCP access should be explicit and auditable.

Prefer least-privilege defaults. Do not grant unrestricted network, host, credential, or MCP access merely to simplify an implementation.

### The API is canonical

The internal state of the Aginctus environment should be available through a documented API suitable for the dashboard and future clients, including a TUI.

Avoid designs in which important control-plane behavior is available only through shell commands, UI-only state, or direct database manipulation.

### Observability matters

New lifecycle operations should expose useful status and errors. Where appropriate, design for events, auditability, and enough state to explain what the platform is doing.

## Working conventions

Before making a non-trivial change, agents should read the relevant repository documentation and nearby code.

When implementation details are not yet established, prefer a small, reversible design over introducing broad framework commitments.

Agents should add or update tests when behavior changes and should report the validation they actually performed. Do not claim tests or checks passed unless they were run.

Do not commit credentials, tokens, private keys, or other secrets.

Do not weaken sandboxing, network policy, authentication, authorization, or secret-handling behavior merely to make tests or demos easier without clearly documenting and justifying the tradeoff.

If a requested change conflicts with these repository rules, surface the conflict instead of silently violating the rules.
