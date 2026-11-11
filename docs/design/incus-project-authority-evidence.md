# Incus project authority: disposable acceptance evidence and decision ledger

**Status: EXECUTED WITH A BLOCKING ASSERTION / NOT SECURITY APPROVED**. Tracking: [#704](https://github.com/blevinn/aginctus/issues/704), [#693](https://github.com/blevinn/aginctus/issues/693); test code in the #750 → #752 → #756 stack. See [threat model](incus-project-authority-gate.md) and [ADR 0004](../adr/0004-three-layer-state.md).

## Why this ledger exists

A resource's Incus project name, ownership label or a CLI `--project` argument is not a security boundary when invoked over an unrestricted local administrative socket. A single accepted revision and CAS transaction also do not authorize root/user configuration merging. We must validate a **genuinely confined principal** using server-enforced restrictions on the *actual pinned Incus daemon*, before recommending any cross-privilege accepted-state writer.

## Disposable-VM setup

Run exclusively on an explicitly authorized test host with a disposable NixOS QEMU VM. The NixOS VM has its own Incus daemon and dedicated virtual disk. The test provisions two projects, a host-owned shared bridge, per-project profiles/volumes and a test-only operator certificate. The server's HTTPS listener is **VM loopback only**. Do not point the test at an existing Incus remote, the pilot host, an administrator's credentials or a production VM.

```bash
# On a clean, isolated worktree of the completed B stack:
flock /tmp/opencode/aginctus-review-nix.lock nix build --accept-flake-config --max-jobs 1 --cores 2 -L --no-link .#vmTests.x86_64-linux.incus-project-authority
```

This is intentionally excluded from ordinary PR hermetic checks due to its cost and the need for a safe KVM/test host. Building/evaluating the test and passing unprivileged unit tests **do not** substitute for executing the full NixOS test.

## Evidence matrix — fill only with real outcomes

| Proof obligation | Test assertion | Status |
| --- | --- | --- |
| Real server identity and authorization routing | Capture `incus version`, server restricted-TLS routing and effective project settings | **Observed:** client/server 7.5.1; routing empty (default TLS evaluator); project assertions passed |
| Operator identity | Client cert registered for one project, no Unix `incus-admin` access | **Passed** in disposable VM |
| Allowed operations | Read permitted project, create project profile and volume | **Passed** in disposable VM |
| Approved shared bridge | Create and run/stop pinned image instance on approved host bridge and pool | **Passed** on `authz-mgmt` / `default` |
| Foreign project isolation | Deny list/volume/profile writes outside permitted project | **BLOCKED:** list returned exit 0 / `[]`; volume/profile writes denied |
| Host-level denial | Deny daemon configuration, bridge and pool creation, project creation/policy relaxation | **Passed:** nonzero permission/policy errors |
| Host filesystem protection | Deny user-added host disk and low-level unconfined runtime config | **Passed:** server reported forbidden pool-less disk and `raw.lxc` |
| Invariants | Compare host bridge, foreign project, pool and instance configuration before/after denied attempts | **Passed**, including foreign volumes and daemon config; instance baseline taken after start/stop |
| Recovery/revocation | Revoke client while operations occur; crash/restore state service; test altered bridge/pool policy | **Not implemented; follow-up required** |
| Confused deputy and schema upgrades | Adversarial nested/dotted config, cross-principal merge, defaults/null/unknown paths, TOCTOU | **Not implemented; blocked until #696 design** |

Attach the full **nonsecret** test output and the Nix/Incus version identity to #704 as an issue comment or in repository-local committed evidence. Never attach client private keys, client certificate body, tokens, environment or runtime secrets. Explicitly identify any probe that failed because of invalid CLI syntax or unsupported feature: that is a **test failure**, not proof of denial.

## Isolated review execution

Executed using Nix 2.34.9 on x86_64-linux with a KVM-backed disposable
NixOS test guest, two virtual CPUs, 4 GiB RAM and guest-local directory
storage. No host Incus socket, authority or trust configuration was used.
The fixture-only #750 test completed successfully. The #752 and cumulative
#756 tests reached the final assertion and failed solely because
`incus list scoped: --project unrelated --format json` returned exit 0 and
`[]`, instead of the expected denial. The assertion remains intact.

This empty result is **not evidence of a foreign-resource disclosure**:
the foreign project contains no instances. Before changing the expected
outcome, populate it with a recognizable administrator-created instance,
compare the raw API response with the CLI result, and establish whether
the server deliberately filters inaccessible objects. Keep the gate open
until that investigation and the policy decision are reviewed.

Other observed denials were `Certificate is restricted`, `Network not
allowed in project`, `User does not have permission for project
"unrelated"`, and explicit forbidden host-disk / low-level-config errors.
The guest reported AppArmor unavailable; these results exercise API policy,
not AppArmor confinement or container-escape resistance. Revocation,
restart/crash recovery, custom-volume attachment, populated foreign-image
and instance isolation, and state-service confused-deputy/TOCTOU checks
remain outstanding. No authorization approval or cross-principal write
permission follows from these partial results.

## Security disposition (must be reviewed, not auto-filled)

Only after all required tests are verified, record:

1. **Authority topology:** restricted project TLS/per-user identity with independent host policy, separate accepted revisions, shared scoped CAS or composite. Identify who authenticates the operator and what API enforces access.
2. **Host/shared resource boundary:** list which shared bridges, pools, volume attachments and profile features are supported or explicitly not delegated.
3. **Residual risk and exclusions:** root socket exposure, inherited project objects, authentication routing, Incus/CVE/version assumptions, lifecycle/secret owners and privileged host proxies.
4. **Accepted-state layout:** one shared revision vs independent host/project revisions, revision pinning/fencing, CAS conflict and recovery semantics. A read-only composite view does not require privileged config merging.
5. **Decision and reviewer:** record dated evidence and approving human; update ADR 0004 and security issue #704 with a direct link to results, not merely a test plan.

## Gate until approval

Continue safe single-principal typed state, nonsecret comparison, and local service work. **Do not enable cross-principal root/user accepted-state merging, privileged promotion or a user-triggered root/Incus-admin executor.** If one negative test unexpectedly succeeds, preserve the VM and redacted evidence as authorized, treat as a blocker and do not weaken the test to obtain a green result.
