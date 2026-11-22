# Restricted project-state read authentication contract

Status: **application-layer interface only** (#1044); no production transport or authorization decision is implemented here.

The project accepted-state authority is the verified Incus server/project, not a caller-supplied `--project` flag, NixOS/HM source or a Unix UID. Environment UUIDs divide revision histories within an authorized project; they are not independent Incus ACL principals.

## Required physical transport

A concrete `RestrictedProjectReadSession` must establish and retain the same authenticated, **project-restricted** identity for binding verification and published record access. The service must have no host Incus administrator socket, unrestricted Incus certificate, host firewall/pool controls, root-deputy API or dynamic project override. An implementation that returns JSON claim fields as `VerifiedBinding` fails this contract.

Every read must reject missing/invalid environment UUIDs; foreign project/server bindings; unexpected revision digest, scope and candidate content; incomplete source results; cancellation; transport errors; and identity changes detected before returning a result. No source error may be interpreted as absent acceptance. The current guard checks binding twice, but those checks are not a transactional guarantee of authorization: revocation must be enforced at the physical service boundary, including after reads begin.

## Negative evidence required before production activation

- Authenticate two different restricted Incus projects against disposable *real* Incus, and prove read denial across project boundaries even when requests forge project or environment selectors
- Prove the backend cannot fetch a foreign project through a shared unrestricted credential, direct local socket or root broker
- Revoke a principal during request processing; refuse access on the new revocation state, without returning a privileged payload
- Verify project membership and immutable server identity are derived from authenticated connection evidence, not aliases, headers, environment variables or candidate files
- Verify the service instance remains unprivileged across restart and reattachment of its project-owned custom volume

No live `accept` endpoint, CAS grant or reconciling writer may be activated by these contracts. Promotion/writer fencing are separate gates under #696/#704.
