# ADR 0007: Consolidate executable and deployment boundaries

- Status: **Accepted direction; implementation and retirement evidence pending**
- Date: 2026-11-26
- Tracking: [audit #1094](https://github.com/blevinn/aginctus/issues/1094), [synthesis #1102](https://github.com/blevinn/aginctus/issues/1102), [CLI #725](https://github.com/blevinn/aginctus/issues/725), [native reconciliation ADR 0002](0002-native-incus-reconciliation.md)
- Related: [ADR 0003](0003-validation-catalog.md), [ADR 0006](0006-environment-incus-authority.md), [pre-stability compatibility](../development/compatibility.md)

## Context and decision

Distribute **one first-party production Go executable, `aginctus`**, and one
developer Go executable, `aginctus-dev`. Prefer cohesive domain packages and
thin command adapters, not a new executable per service or operation. A separate
process, systemd unit, user, Incus guest, image or Nix derivation does not by
itself require a separately compiled executable.

- `aginctus`: product CLI, supported production services and subprocess
  helpers. Prefer `serve <role>` for supervised services and
  `internal <role>` for infrastructure-owned subprocess entry points.
  Hide implementation-only commands from ordinary help where appropriate;
  document their invocation contracts in the relevant deployment reference.
- `aginctus-dev`: testenv orchestration (including worker actions), validation,
  generation, fixtures and synthetic test services. Prefer `testenv`,
  `check`, `generate`, `fixture` and `serve` namespaces.
- `aginctus-testsrv`: **not approved for creation now**. Keep synthetic
  handlers independently testable and reusable under the developer CLI.
  A future separately deployed server-only binary may be justified by
  demonstrated guest footprint, dependency, or authority minimization needs.

The command spellings above are target conventions, not a statement of
current implemented syntax. Just remains the canonical developer task
interface and Nix the packaging/environment interface. External runtime
dependencies, scripts with independent language/runtime requirements, Go
test binaries and separate images are not prohibited. Do not introduce a
second first-party Go entry point under tests or tools merely to evade this
policy. Temporary test programs and build fixtures are not supported
distribution executables.

An additional persistent first-party executable requires a reviewed ADR or
amendment describing why existing modes, process isolation and packaging do
not meet a concrete security, platform, dependency or distribution need.
Examples might be a minimal guest agent or an executable requiring file-level
capabilities; neither is currently justified. Avoid file capabilities/setuid
on a multipurpose binary; prefer restricted supervised processes and
authenticated IPC.

## Security and dependency invariants

- No production dependency on developer command composition, testenv,
  fixtures, synthetic services or validation/generation tooling. Enforce
  this transitively (for example, inspect the compiled Go dependency graph
  for the supported build configurations). Shared production domain libraries
  may be used by both roots; the reverse dependency is forbidden.
- The product CLI and MCP exposure are separate allowlists. Neither hidden
  commands nor command registration grant authority. No test operations or
  internal service entry points are automatically exposed through MCP.
- Dispatch the service role before ordinary CLI/plugin/config initialization.
  Each role receives explicit narrowly scoped dependencies, config and
  credentials. Preserve cancellation, exit semantics, listener bounds,
  fail-closed authorization and secret isolation. No ambient Incus-admin
  client or unrelated user configuration in a restricted service.
- Worker commands remain operator-only and deployed on a **dedicated
  disposable worker**, never the pilot; preserve the existing nonroot
  service identities, credentials, marker/policy checks and Incus confinement.
  A shared developer binary does not make a synthetic service a privileged
  worker. Synthetic processes receive no worker credentials or daemon socket.
- Preserve separate systemd units, accounts, mounts, credentials, hardening,
  project authority, and service lifecycles. This ADR neither authorizes
  deployment of gated services nor changes accepted-state/host authority.
- Use the same production artifact under test, not a fixture-enabled product
  variant. Separate synthetic protocol behavior from the production
  implementation so tests can detect integration faults.

## Audit reconciliation and disposition

The [Phase 3A inventory](https://github.com/blevinn/aginctus/issues/1098#issuecomment-7122203873)
counted nine cmd entry points at baseline
`a375a27e38fcad2eb96558fdb5ed6e544fe3cc14`. The follow-up
[source inventory](https://github.com/blevinn/aginctus/tree/2207ea86540985d64986424804fd869bd51e876c/cmd)
at `2207ea86540985d64986424804fd869bd51e876c` identifies ten
(the project-state read service was added). The target is two persistent
first-party Go entry points, subject to the exception policy above.

| Current entry point or surface | Target disposition |
| --- | --- |
| `cmd/aginctus` | Retain product CLI; remove developer testenv registration after migration. |
| `cmd/aginctus-testenv` | `aginctus-dev testenv worker`; retain worker process and authority boundaries. |
| Product `testenv` command family | Move to `aginctus-dev testenv`, including VM harness and test MCP modes; no production registration. |
| `cmd/aginctus-check` | `aginctus-dev check`; preserve report schema, evidence selection, nonzero incomplete status and hermetic policy. |
| `cmd/aginctus-plugin-gen` | `aginctus-dev generate plugins`; preserve deterministic generation and stale-source check. |
| `cmd/aginctus-project-state-read` | If retained, `aginctus serve project-state`; preserve nonroot, restricted mTLS, read-only contract and deployment gates. |
| `cmd/aginctus-project-state-fixture` | `aginctus-dev fixture project-state` (including any fixture service mode); not a production writer. |
| `cmd/aginctus-state`, `cmd/aginctus-state-local` | Retire superseded same-UID experiments after consumer/replacement proof; necessary test-only behavior may live under developer serve/fixture. |
| `cmd/aginctus-legacy-preview` | Retire unless a concrete approved user migration need remains; if needed, a read-only product migration preview. |
| `cmd/aginctus-oauth-gateway` | Prefer coherent retirement of superseded liveness-only prototype, including image/export and consumers. Only a supported production gateway role belongs under `aginctus serve`. Do not conflate with managed SIWC or LiteLLM OAuth. |
| `tests/oci-stage` | Review and fold maintained test-only staging entry point into developer tooling; preserve pinned/digest-verified behavior. |
| Synthetic test upstream/proxy services | Keep independent fixtures; expose through developer serve when migrating to Go is useful. No mandatory Python-to-Go rewrite or new testsrv binary. |

This refines AUD-03A-013 and AUD-03F-010, preserves AUD-04-009's dedicated
worker boundary, and simplifies AUD-04-013's export policy. It retains
AUD-03A-012/AUD-03F-001's preference to delete the obsolete OAuth scaffold,
rather than migrate it merely to meet a binary count. See the
[3A findings](https://github.com/blevinn/aginctus/issues/1098#issuecomment-7122213029),
[3F findings](https://github.com/blevinn/aginctus/issues/1098#issuecomment-7128433994)
and [Nix review](https://github.com/blevinn/aginctus/issues/1099#issuecomment-7128511450).
The audit's outstanding mechanical and operator evidence is not closed by
this decision.

## Implementation and acceptance

Use existing remediation tracking rather than a new competing roadmap.
Coordinate with native cutover/legacy deletion and state authority work;
do not port dead tools or enable deferred services. Preserve the approved
release split (residual simplification v0.2.5 unless ready for v0.2);
no new release blocker is established by this ADR.

1. Create the developer root and migrate all testenv, check and generation
   callers together with their Just, CI, Nix, worker unit and documentation
   consumers. Keep worker operation policy, revision provenance and existing
   test evidence formats intact.
2. Migrate surviving production service entry points with independent startup,
   narrow privileges, signal handling, Nix image/service invocation and
   negative authorization checks. Reuse the unwrapped production executable
   where possible to avoid pulling unrelated CLI runtime dependencies into
   guest image closures.
3. Dispose of experimental and superseded tools with compiled/transitive
   reachability, Nix, generated/embedded assets, build tags, scripts, docs,
   external consumers and replacement-test evidence. Keep required fixtures
   until their replacement is demonstrated; no implicit state/secret deletion.
4. Remove obsolete entry points/exports and caller spellings in coherent
   increments; document consequential breaks per pre-stability policy. Avoid
   speculative aliases. Preserve distinct image/VM artifacts where needed.
5. Add a hermetic entry-point inventory/allowlist and production dependency
   guard (including persistent main packages outside cmd); assert developer
   and internal service operations are absent from production MCP, and
   developer commands absent from production help/dispatch. Test service
   startup without ordinary CLI initialization and worker denial on
   non-disposable hosts. Build both executables, inspect production release
   contents and Nix closures/units on supported systems, run applicable
   hermetic and disposable-worker/VM scenarios, and record exact evidence.

The policy is documentation now, not implementation completion. No build,
runtime, privilege change, provider access or live host action is authorized
by this record. Source review is not a substitute for execution evidence.
