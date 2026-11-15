# CLI application operations

This directory owns CLI-specific execution handlers and their direct
unit/policy tests. The root `internal/cli` package injects access to
Incus, host OS commands, filesystem data and domain stores through typed
functions/interfaces; operation packages must not depend on Cobra, the CLI
root or command registration.

## Ownership

- `aggregate`, `workload`, `herdr`: lifecycle/ownership observations,
  bounded operations and state transitions
- `images`: separate legacy ambient image behavior from explicitly scoped,
  create-only writes and project-wide cooperative writer leases
- `network`, `operator`: firewall and host privilege operations with
  fail-closed authorization and bounded evidence
- `gateway`: credential, lifecycle, egress, registry, SIWC and input
  operations with explicit host/store capabilities
- `config`, `doctor`, `testenv`: CLI application action dispatch,
  preserving no-side-effect help and invalid-command behavior

New pure policy and request validation tests belong next to the owning
operation. Keep root-level integration tests that validate public syntax,
output/exit behavior, privilege boundaries, injected transport and
fail-closed refusal across multiple packages.

Do not recreate domain models or infer mutation authorization from
self-declared Incus metadata, state or operator evidence. Where an algorithm
is shared beyond the CLI, place it in the appropriate domain package rather
than another generic operation wrapper.

See [`../ARCHITECTURE.md`](../ARCHITECTURE.md) and issue #755 for
current inventory, registration/operation separation and open exceptions.
