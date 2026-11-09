# Incus project authority: disposable acceptance evidence and decision ledger

**Status: PENDING EXECUTION / NOT SECURITY APPROVED** (2026-11-09). Tracking: [#704](https://github.com/blevinn/aginctus/issues/704), [#693](https://github.com/blevinn/aginctus/issues/693); test code in the #750 → #752 → following PR stack. See [threat model](incus-project-authority-gate.md) and [ADR 0004](../adr/0004-three-layer-state.md).

## Why this ledger exists

A resource's Incus project name, ownership label or a CLI `--project` argument is not a security boundary when invoked over an unrestricted local administrative socket. A single accepted revision and CAS transaction also do not authorize root/user configuration merging. We must validate a **genuinely confined principal** using server-enforced restrictions on the *actual pinned Incus daemon*, before recommending any cross-privilege accepted-state writer.

## Disposable-VM setup

Run exclusively on an explicitly authorized test host with a disposable NixOS QEMU VM. The NixOS VM has its own Incus daemon and dedicated virtual disk. The test provisions two projects, a host-owned shared bridge, per-project profiles/volumes and a test-only operator certificate. The server's HTTPS listener is **VM loopback only**. Do not point the test at an existing Incus remote, the pilot host, an administrator's credentials or a production VM.

```bash
# On a clean, isolated worktree of the completed B stack:
nix build -L --no-link .#vmTests.x86_64-linux.incus-project-authority
```

This is intentionally excluded from ordinary PR hermetic checks due to its cost and the need for a safe KVM/test host. Building/evaluating the test and passing unprivileged unit tests **do not** substitute for executing the full NixOS test.

## Evidence matrix — fill only with real outcomes

| Proof obligation | Test assertion | Status |
| --- | --- | --- |
| Real server identity and authorization routing | Capture `incus version`, server restricted-TLS routing and effective project settings | **Not run** |
| Operator identity | Client cert registered for one project, no Unix `incus-admin` access | **Not run** |
| Allowed operations | Read permitted project, create project profile and volume | **Not run** |
| Approved shared bridge | Create and run/stop pinned image instance on approved host bridge and pool | **Not run** |
| Foreign project isolation | Deny list/volume/profile writes outside permitted project | **Not run** |
| Host-level denial | Deny daemon configuration, bridge and pool creation, project creation/policy relaxation | **Not run** |
| Host filesystem protection | Deny user-added host disk and low-level unconfined runtime config | **Not run** |
| Invariants | Compare host bridge, foreign project, pool and instance configuration before/after denied attempts | **Not run** |
| Recovery/revocation | Revoke client while operations occur; crash/restore state service; test altered bridge/pool policy | **Not implemented; follow-up required** |
| Confused deputy and schema upgrades | Adversarial nested/dotted config, cross-principal merge, defaults/null/unknown paths, TOCTOU | **Not implemented; blocked until #696 design** |

Attach the full **nonsecret** test output and the Nix/Incus version identity to #704 as an issue comment or in repository-local committed evidence. Never attach client private keys, client certificate body, tokens, environment or runtime secrets. Explicitly identify any probe that failed because of invalid CLI syntax or unsupported feature: that is a **test failure**, not proof of denial.

## Security disposition (must be reviewed, not auto-filled)

Only after all required tests are verified, record:

1. **Authority topology:** restricted project TLS/per-user identity with independent host policy, separate accepted revisions, shared scoped CAS or composite. Identify who authenticates the operator and what API enforces access.
2. **Host/shared resource boundary:** list which shared bridges, pools, volume attachments and profile features are supported or explicitly not delegated.
3. **Residual risk and exclusions:** root socket exposure, inherited project objects, authentication routing, Incus/CVE/version assumptions, lifecycle/secret owners and privileged host proxies.
4. **Accepted-state layout:** one shared revision vs independent host/project revisions, revision pinning/fencing, CAS conflict and recovery semantics. A read-only composite view does not require privileged config merging.
5. **Decision and reviewer:** record dated evidence and approving human; update ADR 0004 and security issue #704 with a direct link to results, not merely a test plan.

## Gate until approval

Continue safe single-principal typed state, nonsecret comparison, and local service work. **Do not enable cross-principal root/user accepted-state merging, privileged promotion or a user-triggered root/Incus-admin executor.** If one negative test unexpectedly succeeds, preserve the VM and redacted evidence as authorized, treat as a blocker and do not weaken the test to obtain a green result.
