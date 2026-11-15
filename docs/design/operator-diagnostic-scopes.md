# Operator diagnostic cleanup: project authority, host observations

Status: accepted cleanup direction; no production mutation or multi-principal state service is approved.
Parent: [ADR 0006](../adr/0006-environment-incus-authority.md). Gates: [#696](https://github.com/blevinn/aginctus/issues/696), [#704](https://github.com/blevinn/aginctus/issues/704).

## Scope boundary

Incus **projects** are authorization and three-layer configured → accepted → observed state scopes; environments have independently versioned lifecycle records within a project, but no separate Incus ACL. NixOS/root owns host services and host-capability policy independently. Home Manager/root/local configuration inputs may produce environment candidates but are not *competing runtime authorities*. A future separate host three-layer state is possible; it does not merge a user revision into privileged state.

The old `operator units-audit --owner system|user`, `package-audit --owner system|user`, and `state-audit --owner system|user` are **read-only diagnostics**, never enforcement, and cannot prove that a particular Incus resource has one live writer. Never reinterpret their success as project authorization.

## Disposition

| Existing functionality | Replacement / owner | Required compatibility |
| --- | --- | --- |
| NixOS/HM `operator-package.json` emission and CLI `package-audit` | Remove declaration parity mechanism. Nix profiles remain normal package installation. Future running executable/service version evidence is independently observed, not inferred from manifests. | Remove the Cobra leaf atomically with Nix outputs, VM invocations, CLI bindings, tests and misleading docs. No silent always-success compatibility response. |
| `operator units-audit --owner` | Host unit inventory/diagnostic, not selected lifecycle authority. Project state service will fence actual participating writers. | Preserve known unit classification and timeout/denial behavior during migration. Do not infer other-user/manual writer absence. |
| `operator state-audit --owner` | Host-owned, metadata-only protected credential checks under a single explicit root/UID; a separate project-state inspection API when authenticated storage exists. | Preserve symlink/ownership/mode negative tests, never read secrets. Do not assume presence or absence of another user's credential tree confers lifecycle permission. |
| `operator preflight`, `project-plan`, `project-ensure` | Incus project-owned diagnostics and explicit create-only operation. | Retain existing no-ambient-project and negative authorization tests. |
| Controller enrollment `internal/controllerauthority` | Removed by #920. | Do not restore a root/user registration broker or mistake a local peer UID for project credentials. |

## Changes in this cleanup stack

1. Remove package manifest generation, audits, the command, the associated synthetic manifest parity tests, and VM assumptions in one vertical slice.
2. Replace the host unit-system/user conflict claim with a read-only host inventory report; preserve failure on unverified evidence, but eliminate the selected owner/competitor inference. Keep separate root-host and user unit observations only as observables.
3. Retain protected gateway credential metadata checks until an explicit single-owner host diagnostic exists. Never delete the safety assertions or reframe a file owner as a project security principal.
4. Update user-facing reference and help text to report retired command behavior and independent project authority. Do not add a privileged proxy or activate a mutating Home Manager unit.

## Gates

Before merging command deletions, run Go CLI/operation tests, Nix module assertions, Home Manager operator disposable VM, docs links/examples, and CI. Assertions that a user switch does not mutate Incus/host resources, firewall policy remains root-only, and protected credential metadata rejects symlinks/broad modes must survive. If full VM cannot run, mark that evidence pending rather than claiming it passed.

The legacy `aginctus-reconcile` NixOS systemd service is **not** deleted in this stack. Its operational behavior must only be retired once accepted-state reconciliation and manual host migration have been validated.
