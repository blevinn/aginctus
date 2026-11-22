# Go client and environment read acceptance (v0.2)

**Status: NOT RUN.** This checklist is an operator-held acceptance procedure,
not evidence of deployed production functionality or approval to use unrestricted
Incus administrative credentials. It covers the read-only client and structured
output feature, complementing the [CLI architecture checklist](cli-architecture-v02.md)
and [project-state backup and recovery procedure](project-state-backup-recovery.md).

The currently installed CLI still lacks a production-authenticated state reader.
It is **expected** to fail closed for environment/workload/state reads until
issues [#695](https://github.com/blevinn/aginctus/issues/695),
[#696](https://github.com/blevinn/aginctus/issues/696) and
[#704](https://github.com/blevinn/aginctus/issues/704) establish and validate
the project-scoped service and restricted transport. An implementation-only
CLI-injected reader test must not be recorded as a live service PASS.

## Scope, prerequisites and evidence

- Use the actual NixOS development host for read-only user-facing acceptance.
  Do **not** supply the host Incus admin socket, copy private TLS keys, or
  modify host network/firewall settings to satisfy a test.
- Use a **separately authorized disposable NixOS/Incus VM** for negative
  project isolation, credential revocation, restart and fault tests. Never
  execute disruptive tests against the development host's real Incus daemon.
- Identify the exact Incus server and explicitly authorized nondefault
  project; identify two **different immutable environment UUIDs in one
  project**, optionally with distinct display names.
- Capture nonsecret git revision, Nix flake lock fingerprint, CLI version,
  selected project, server identity proof, schema version, stdout/stderr and
  exit code for each check. Redact credentials, secrets, private paths and
  project metadata not authorized for disclosure.
- Obtain independent evidence from an authenticated Incus/project reader;
  CLI output or an environment name alone is not proof of ownership.

Suggested evidence header (fill only from actual observations):

```text
Revision: NOT RUN
Nix flake lock SHA-256: NOT RUN
Incus client/server versions: NOT RUN
Project/server authorization: NOT RUN
Environment UUID A/B: NOT RUN
Run environment: NOT RUN
Operator and time: NOT RUN
```

## Gate A — Hermetic contract checks (no live Incus authority)

Run from a clean isolated worktree of the reviewed commit:

```sh
nix --quiet develop --no-update-lock-file -c go test ./pkg/api ./pkg/client ./internal/cli/output ./internal/cli
nix --quiet develop --no-update-lock-file -c just check report-json
```

Require focused client and root CLI tests to pass, including invalid identity,
foreign environment, ambiguous display name, missing read capability,
contradictory workload observation, and structured stderr failure cases.
Record the actual command output and exit status. Passing Gate A **does not**
unlock or simulate production authority.

## Gate B — Installed CLI fail-closed baseline

Before deploying a production restricted reader, the following **read-only**
calls must return nonzero, with no successful resource document on stdout:

```sh
aginctus --incus-project team environment list -o json
aginctus --incus-project team --environment dev workloads list -o json
aginctus --incus-project team --environment dev state show -o json
aginctus --incus-project team --environment dev state plan-accept -o json
```

Replace `team` and `dev` with *authorized* test selectors. For supported
`-o json`, check that the machine error is one versioned `Error` document
on stderr with nonzero exit, and stdout contains no resource payload. An
empty `items: []` or `hasAccepted: false` from an unavailable service is
a **FAIL**, not a graceful degradation.

## Gate C — Production authenticated reads (blocked until approved service)

Once a reviewed project-restricted reader is actually deployed and authorized,
repeat read-only commands with both UUID and unambiguous display-name
selectors. Require all of the following:

| Check | PASS condition |
| --- | --- |
| Environment list/show | Exact enrolled UUIDs, verified server/project binding, deterministic list order; no foreign environments |
| Selection | UUID and unique display name resolve to the same immutable identity; ambiguous name or stale binding fails |
| Workload list/show/status | Only environment-owned logical resources, typed classes and distinct components; nonowned instances never leak |
| Configured/accepted | Independent layers and explicit accepted revision; a read failure never resembles verified absence |
| State diff/plan-accept | Accepted baseline → configured candidate; no mutation, no implicit deletion on unselection; pinned digest/revision |
| Machine output | One versioned JSON/YAML document on stdout on success; errors exclusively on stderr with nonzero exit |
| Credential and project denial | Foreign project, revoked principal and missing enrollment return an error, never partial data or elevated access |

Run the same read paths through the in-process Go client and Cobra adapter,
comparing identities and schema semantics—not merely exit status. A
configuration-file or `--incus-project` override must not change the server
identity or authorize another project.

## Gate D — Disposable VM authority and recovery

Only with explicit approval to operate the *disposable* VM:

1. Enroll two independently revisioned environments in the same project.
   Verify project isolation and exact foreign-resource absence with independent
   administrator controls.
2. Revoke the restricted principal and repeat reads using the same identity,
   including an existing/persistent connection if the server supports it.
   Record whether in-flight operations stop; a new-connection-only denial is
   insufficient for the full security signoff.
3. Restart/recreate the service using its retained project-owned volume.
   Prove successful identity revalidation and accepted revision persistence.
4. Test state-store unavailability, partial/corrupt revision, stale accepted
   pointer, wrong server binding, and concurrent reader/revision changes.
   Require failures to be explicit and nonsecret, without fabricated absence.
5. Validate backup/restore and upgrade separately under the dedicated
   project-state recovery procedure. Do not interpret restart alone as a
   power-loss, backup or disaster-recovery test.

For mutation evidence (CAS promotion, fencing, native reconciliation and
removal), use the dedicated #696/#698/#701 acceptance gates. **This read
checklist never authorizes those operations.**

## Completion record

Record `PASS`, `FAIL`, `BLOCKED`, or `NOT RUN` separately for Gates A–D,
with observed evidence and clean exit/cleanup status. Do not equate
`BLOCKED` with `PASS`.

Feature signoff requires the ordinary installed CLI to pass Gate C without
injected test readers, the relevant Gate D isolation/recovery evidence, and
independent operator approval. Until then, the Go API and structured output
can be considered implemented at contract level, **not production-complete**.
