# Changelog

Notable user-facing changes are recorded here when a version is released.
A release is not complete until its acceptance gates and release notes are approved.

## [Unreleased]

v0.2 development is in progress. These are **merged implementation changes**,
not release approval or independently verified end-to-end acceptance. See
[working v0.2 release notes](docs/releases/v0.2.0.md) for WIP features and gates.

- Refactored CLI and testenv operation families to typed Cobra subcommands, generated help, error/compatibility tests, and dedicated command packages ([#372](https://github.com/blevinn/aginctus/issues/372), [PR #667](https://github.com/blevinn/aginctus/pull/667)).
- Added guarded native Incus project/bridge/instance lifecycle and scoped workload reconciliation, plus **native gateway design groundwork** for two-service topology, execution-safety validation, and immutable OCI image selection. Gateway cutover, project co-location and `incus-apply`/`incus-compose` retirement remain open ([#153](https://github.com/blevinn/aginctus/issues/153), [#158](https://github.com/blevinn/aginctus/issues/158), [PRs #719–#721](https://github.com/blevinn/aginctus/pull/721)).
- Added standalone Home Manager operator configuration, read-only authority/ownership diagnostics and VM regressions covering host firewall coexistence, activation, recovery and Incus downtime; default-on user reconciliation and migration remain gated ([#426](https://github.com/blevinn/aginctus/issues/426)).
- Added nonsecret gateway provider/model asset declarations, normalized MCP grants and redacted policy inspection. Live registration convergence and MCP runtime enforcement remain incomplete ([#496](https://github.com/blevinn/aginctus/issues/496), [#162](https://github.com/blevinn/aginctus/issues/162)).
- Integrated an **Aginctus `openai-siwc` plugin** with the managed LiteLLM gateway for consented ChatGPT sign-in: protected gateway-only credential state, browser enrollment, serialized refresh, workload-scoped route binding, Responses transport and gateway apply/login/logout flows. This is **not** LiteLLM's native `chatgpt/` provider; separately authorized real-account and host acceptance remain unverified ([PR #692](https://github.com/blevinn/aginctus/pull/692), [#187](https://github.com/blevinn/aginctus/issues/187)).
- Added initial nonsecret configured-to-accepted **selection manifests** and read-only identity projection; durable accepted revisions, promotion, reconciliation and root/user authorization are *not* implemented ([#693](https://github.com/blevinn/aginctus/issues/693), [PRs #716–#717](https://github.com/blevinn/aginctus/pull/717)).
- Consolidated hermetic test catalogs, execution and redacted reporting in Go, expanded VM evidence and disposable testenv infrastructure, and unified worker CLI entry points. Genuine fresh-host deploy/coding/cleanup/repeat acceptance remains outstanding ([#630](https://github.com/blevinn/aginctus/issues/630), [#580](https://github.com/blevinn/aginctus/issues/580)).
- Split CI into parallel Go/static jobs with fail-closed targeted Nix contracts and a preserved required aggregate; run comprehensive validation after merge, on demand and on schedule. Performance/operations closeout remains tracked ([#705](https://github.com/blevinn/aginctus/issues/705), [#710](https://github.com/blevinn/aginctus/issues/710)).
- Reorganized documentation into task-oriented operations, reference, architecture, testing, development and history sections, with revised navigation, link checks and contributor/security reporting entry points; project-level governance closeout remains tracked ([#645](https://github.com/blevinn/aginctus/issues/645)).

## [0.1.0]

- Initial single-host operator-controlled NixOS/Incus Aginctus CLI and module, with owned Herdr and OpenCode deployment workflows.
- Private LiteLLM/PostgreSQL gateway, selective OpenCode Go Responses forwarding, model alias registration, and scoped revocable workload credentials.
- Management-only network defaults, separate gateway egress, dynamic firewall DNS/DHCP integration, SSH host pinning, persistent Herdr identity, and guarded provisioning.
- Hermetic and disposable-VM validation/reporting, read-only diagnostics, and operator-approved acceptance procedures.
- Real pilot Herdr/OpenCode coding and private provider inference verified with independent cleanup; **full fresh-host deploy/smoke/cleanup/repeat unverified and deferred to v0.2**. See [release notes](docs/releases/v0.1.0.md) and [release tracking](https://github.com/blevinn/aginctus/issues/118).
