# Host operations

These pages describe local host authority and **explicitly selected** Incus state. No read-only audit grants mutation privileges.

- [Read-only Incus operator preflight](operator-preflight.md) — selected project and access limitations.
- [Gateway credential state audit](operator-state-audit.md) and [system/user service unit audit](operator-units-audit.md) — inspect ownership collisions and local service overlap.
- [NixOS and Home Manager coexistence](home-manager.md) — user operator installation versus separate NixOS host firewall authority.
- [Firewall backend contract](firewall-host-backends.md) — NixOS firewall and Incus policy boundaries.
- [Firewall test/pilot validation](firewall-pilot-validation.md) — explicit tier; not permission to change host rules.

For gated gateway-only egress maintenance, use the [gateway workflow](../gateway/README.md); for settings and default activation consequences, use [configuration reference](../../reference/configuration.md).
