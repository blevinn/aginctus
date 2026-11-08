# Changelog

Notable user-facing changes are recorded here when a version is released.
A release is not complete until its acceptance gates and release notes are approved.

## [Unreleased]

v0.2 development is in progress. These entries describe merged code, not
release approval or independently verified end-to-end operation. See the
[working v0.2 release notes](docs/releases/v0.2.0.md) for acceptance gates
and incomplete features.

- Replaced the positional CLI dispatcher with typed Cobra command families, generated help, compatibility coverage, and a unified `aginctus-testenv` entry point ([#372](https://github.com/blevinn/aginctus/issues/372)).
- Introduced official Incus SDK-based native planning and guarded project/network/instance lifecycle building blocks, including limited scoped workload reconciliation. The gateway cutover, full project co-location, and retirement of Apply/Compose remain incomplete ([#153](https://github.com/blevinn/aginctus/issues/153), [#198](https://github.com/blevinn/aginctus/issues/198)).
- Added standalone Home Manager operator packaging/configuration and expanded read-only authority, mixed-manager, firewall-boundary, and disposable-VM regression coverage; default-on user reconciliation and safe migration remain gated ([#426](https://github.com/blevinn/aginctus/issues/426)).
- Added typed MCP declarations and redacted effective-policy inspection, plus declarative nonsecret gateway provider/model asset configuration. Enforcement, full provider/model reconciliation, and live integration are not yet complete ([#162](https://github.com/blevinn/aginctus/issues/162), [#496](https://github.com/blevinn/aginctus/issues/496)).
- Added Go-owned hermetic suite catalog, runner, structured redacted reporting and VM evidence inspection, along with more disposable-VM and dedicated-worker testenv foundations; genuine fresh-host acceptance remains outstanding ([#630](https://github.com/blevinn/aginctus/issues/630), [#580](https://github.com/blevinn/aginctus/issues/580)).
- Added the initial protected LiteLLM-owned ChatGPT session/token lifecycle and Responses transport implementation, without claiming complete deployment, consent/recovery validation or live OAuth acceptance ([#187](https://github.com/blevinn/aginctus/issues/187)).
- Began parallel Go/static CI validation while preserving the existing comprehensive required gate, and protected main-branch run evidence from superseding cancellation ([#705](https://github.com/blevinn/aginctus/issues/705)).

## [0.1.0]

- Initial single-host operator-controlled NixOS/Incus Aginctus CLI and module, with owned Herdr and OpenCode deployment workflows.
- Private LiteLLM/PostgreSQL gateway, selective OpenCode Go Responses forwarding, model alias registration, and scoped revocable workload credentials.
- Management-only network defaults, separate gateway egress, dynamic firewall DNS/DHCP integration, SSH host pinning, persistent Herdr identity, and guarded provisioning.
- Hermetic and disposable-VM validation/reporting, read-only diagnostics, and operator-approved acceptance procedures.
- Real pilot Herdr/OpenCode coding and private provider inference verified with independent cleanup; **full fresh-host deploy/smoke/cleanup/repeat unverified and deferred to v0.2**. See [release notes](docs/releases/v0.1.0.md) and [release tracking](https://github.com/blevinn/aginctus/issues/118).
