# Changelog

Notable user-facing changes are recorded here when a version is released.
A release is not complete until its acceptance gates and release notes are approved.

## [Unreleased]

## [0.1.0]

- Initial single-host operator-controlled NixOS/Incus Aginctus CLI and module, with owned Herdr and OpenCode deployment workflows.
- Private LiteLLM/PostgreSQL gateway, selective OpenCode Go Responses forwarding, model alias registration, and scoped revocable workload credentials.
- Management-only network defaults, separate gateway egress, dynamic firewall DNS/DHCP integration, SSH host pinning, persistent Herdr identity, and guarded provisioning.
- Hermetic and disposable-VM validation/reporting, read-only diagnostics, and operator-approved acceptance procedures.
- Real pilot Herdr/OpenCode coding and private provider inference verified with independent cleanup; **full fresh-host deploy/smoke/cleanup/repeat unverified and deferred to v0.2**. See [release notes](docs/releases/v0.1.0.md) and [release tracking](https://github.com/blevinn/aginctus/issues/118).
