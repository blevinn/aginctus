# Pre-stability compatibility policy

Aginctus is an experimental project. Until a separate stability and compatibility
commitment is explicitly designed, approved, and adopted, **there is no presumption
of backward compatibility, forward migration, or preservation of state from earlier
Aginctus versions**. This applies to all development and pilot deployments, even
when they are running a released version.

## Scope and duration

This policy applies to Aginctus-managed interfaces and artifacts, including CLI
commands and output, APIs, configuration and accepted-state schemas, persistent
data formats, on-disk layouts, images, internal protocols, and automation
interfaces. Existing pilot deployments are disposable; they are not a protected
production compatibility baseline.

The policy applies to v0.2 and v0.3 and remains in effect until explicitly
superseded. v0.4 or v0.5 is a possible time to establish a compatibility
commitment, **not** a promise that one will exist by either release. No release
number, earlier implementation, or previous pilot behavior implicitly creates a
compatibility guarantee.

## Engineering defaults

- Prefer the simplest coherent current design over compatibility layers,
  deprecated aliases, transitional adapters, dual-read/dual-write formats, or
  legacy fallbacks. Add such mechanisms only for a documented present need.
- Breaking changes may be made directly; no automatic upgrade, historical
  schema migration, downgrade support, or rollback compatibility is required.
- Prefer clean, reproducible bootstrap and reinstantiation from the currently
  authoritative declarative inputs over migration of previous installations.
- Test the current documented contract and fresh deployments. Historical-version
  interoperability and migration tests are optional unless specifically required
  by a feature or separately approved commitment.
- Describe material breaking changes and whether redeployment is required,
  without inventing upgrade paths or maintaining legacy behavior purely to
  accommodate disposable pilot installations.
- Review designs and PRs against this policy; do not request compatibility
  scaffolding merely because a current pilot or previous version would break.

## Unchanged safety and correctness obligations

Absence of compatibility guarantees **does not authorize destructive actions**.
Removing instances, projects, volumes, data, credentials, or other resources
still requires the normal ownership, scope, explicit approval, and safety
checks. Never assume a target host is disposable solely because pilot
deployments *can* be rebuilt. Destructive operations must remain appropriately
guarded, visible, and fail-safe.

Security, privilege and isolation boundaries, external protocol conformance
(including Incus contracts), deterministic reconciliation, and reproducible
clean bootstrap remain required. In particular, do not weaken authentication,
authorization, state ownership, or recovery safety to avoid implementing a
migration. Accepted-state authority must remain independently validated.

This policy distinguishes **not guaranteeing preservation across versions**
from **unintended loss or corruption within the current version**: the latter
remains a correctness defect.

## Future stability commitment

When the project is ready, introduce an explicit, separately reviewed
stability/compatibility policy defining the supported interfaces, scope,
versioning rules, migration obligations, retention guarantees, and rollout
boundary. Until that policy is adopted, compatibility exceptions are
case-by-case engineering decisions, not precedent.
