# Engineering contract

This is the go-forward engineering policy approved in the [codebase audit](https://github.com/blevinn/aginctus/issues/1094). It describes required design properties, not a claim that all current implementations satisfy them. Existing transitional paths must not be extended without a documented present need. Architectural changes require an explicit decision; see [ADR 0006](../adr/0006-environment-incus-authority.md) and the [compatibility policy](compatibility.md).

## Authority and state

- Incus server identity and project delimit authorization; immutable environment identity delimits lifecycle ownership within a project. Names and labels are evidence, not authorization. Use the canonical environment-id ownership marker for new native resources.
- NixOS owns privileged host policy independently. Home Manager and NixOS may produce the same typed candidate; neither configuration origin nor merged preferences grant authority. No root proxy or privileged recursive merge of user and system candidates.
- Use explicit JSON-first typed candidates, immutable accepted revisions, authorized CAS promotion and separately observed state. Promotion does not mutate workloads. Optional higher-level producers are not core dependencies.
- Pin the accepted revision and verify scope, ownership, preconditions and live writer exclusion at effects. Fail closed on unknown or foreign resources; never silently adopt, redirect, delete or replace. Keep partial/uncertain outcomes recoverable. Cooperative locks do not constrain unrelated administrators.
- Keep secret values out of accepted state, Nix store, readable resource configuration and diagnostics. Use scoped, versioned credential references and verified delivery/revocation. Host networking and firewall authority remain independent.

## Lifecycle and composition

Use native Incus reconciliation and explicit operator-triggered execution first. Prefer static typed plugin composition, concrete logical workloads and managed components over speculative generic frameworks. The gateway belongs to its environment; running containers do not prove application readiness. Retire legacy drivers and forks after equivalent supported behavior and safety are established. Follow the pre-stability policy: do not invent historical migration or compatibility machinery; clean reinstantiation still requires explicit authorization for destructive effects.

Use these terms consistently in new contracts:

| Term | Meaning |
| --- | --- |
| Configured | Valid configuration exists. |
| Accepted | An authorized immutable revision exists. |
| Planned | Intended changes have been calculated. |
| Applied | Requested resource operations completed. |
| Running | Runtime state was observed. |
| Ready | Application-specific readiness was verified. |
| Converged | Observed managed state matches the accepted revision. |
| Unverified | Evidence is insufficient for the claimed condition. |

## Implementation and review

- Prefer domain-owned types and small, coherent vertical slices. Parse once, execute once, render once; use typed errors and explicit injected effect capabilities. Avoid new mutable global transport hooks, redundant adapters and generic utility layers.
- Correlate diagnostics with operation and revision identity. Separate ephemeral telemetry, durable operation outcomes and acceptance evidence. Preserve cancellation and uncertainty.
- Keep one canonical owner for each contract and default. Check Go, Nix, guest and documentation consumers when changing it. Update tests for changed behavior, including denial, interruption and unknown observations.
- Before removal, inspect compiled and indirect consumers (Nix, generation, embedding, build tags, registration and external API use). Text search alone is insufficient.
- Distinguish source review, hermetic execution, disposable VM evidence and actual operator acceptance. Record exact revision, commands, outcomes and limitations. Never equate an implemented test with a passing test or synthetic execution with live acceptance.
- Correct misleading security and readiness claims promptly. Do not polish implementations scheduled for retirement unless needed for current correctness.

## Delivery priorities

The approved audit roadmap prioritizes v0.2 authority, native lifecycle, concrete plugin integration, credential isolation and functional acceptance. Residual simplification and auditability target v0.2.5, which may fold into v0.2 if ready. Existing release commitments remain in force. Do not expand scope with optional configuration languages, autonomous controllers or new backends as part of hygiene.

Run a bounded near-term operator acceptance checkpoint and a comprehensive pre-release checkpoint, with separate approval for host mutations and live providers. Use the existing [testing guidance](../testing/README.md); do not create competing evidence frameworks.
