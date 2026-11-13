# Incus project authority: disposable acceptance evidence and decision ledger

**Status: IMPLEMENTED VM MATRIX PASSED / NOT SECURITY APPROVED**. Tracking: [#704](https://github.com/blevinn/aginctus/issues/704), [#693](https://github.com/blevinn/aginctus/issues/693); test code in the #750 → #752 → #756 stack. See [threat model](incus-project-authority-gate.md) and [ADR 0004](../adr/0004-three-layer-state.md).

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
| Foreign project isolation | Filter populated foreign instance collections; deny named instance/profile/volume reads and mutations | **Passed:** CLI/raw API hide foreign instance; named API reads HTTP 403; CLI reads/mutations denied; admin existence controls passed |
| Host-level denial | Deny daemon configuration, bridge and pool creation, project creation/policy relaxation | **Passed:** nonzero permission/policy errors |
| Host filesystem protection | Deny user-added host disk and low-level unconfined runtime config | **Passed:** server reported forbidden pool-less disk and `raw.lxc` |
| Invariants | Compare host bridge, foreign project, pool and instance configuration before/after denied attempts | **Passed**, including foreign volumes and daemon config; instance baseline taken after start/stop |
| Recovery/revocation | Revoke test TLS credential after permitted operations; require stale CLI and raw API denial with administrator existence controls. Mid-flight revoke/crash recovery remain separate | **Revocation fixture proposed; real VM execution pending** |
| Confused deputy and schema upgrades | Adversarial nested/dotted config, cross-principal merge, defaults/null/unknown paths, TOCTOU | **Not implemented; blocked until #696 design** |

Attach the full **nonsecret** test output and the Nix/Incus version identity to #704 as an issue comment or in repository-local committed evidence. Never attach client private keys, client certificate body, tokens, environment or runtime secrets. Explicitly identify any probe that failed because of invalid CLI syntax or unsupported feature: that is a **test failure**, not proof of denial.

## Isolated review execution

Executed using Nix 2.34.9 on x86_64-linux with a KVM-backed disposable
NixOS test guest, two virtual CPUs, 4 GiB RAM and guest-local directory
storage. No host Incus socket, authority or trust configuration was used.
The fixture-only #750, populated denial-matrix #752 and cumulative workload
#756 tests completed successfully. The earlier list-denial blocker was
resolved by proving the pinned server's collection-filtering semantics,
not by treating arbitrary successful requests as denials.

### Populated foreign-resource proof

Guest root creates `foreign-sentinel` instance, profile and custom volume
in `unrelated`, each marked `user.authz_sentinel=foreign-only`. The
restricted operator creates `scope-control` in `aginctus-authz`, marked
`allowed-only`. These are stopped, empty-container visibility fixtures;
#756 separately boots the pinned workload image. No project restrictions
or certificate authority were widened to make the checks pass.

| Request / control | Guest administrator | Restricted TLS operator |
| --- | --- | --- |
| Foreign instance collection, recursion 0/1/2 | HTTP 200, exactly the foreign sentinel present | HTTP 200, metadata exactly `[]` |
| Allowed instance collection, recursion 0/1/2 | HTTP 200, allowed resources present | HTTP 200, metadata exactly equals admin's allowed-project collection |
| All-projects instance collection, recursion 0/1/2 | HTTP 200, foreign and allowed sentinels present | HTTP 200, exactly the allowed-project collection, no foreign instance |
| CLI instance list, foreign / allowed / all projects | Foreign existence independently checked above | Exit 0 / `[]` for foreign; allowed control visible; all-projects exactly equals allowed project/name pairs |
| Named foreign instance, profile and custom volume | HTTP 200 with exact `foreign-only` marker | HTTP 403 with API error envelope |
| Named allowed instance | Allowed existence independently checked above | HTTP 200 with exact `allowed-only` marker |
| CLI named foreign reads and marker updates | Fixtures exist with original markers | Nonzero permission errors for instance, profile and volume |

The raw requests use `curl` with the **same operator-owned TLS certificate**
as the CLI, bypassing CLI filtering. Admin controls use only the guest's
local socket. Collections are tested at URL-only, recursive and full-state
levels, including `all-projects=true`; foreign absence is checked again
after denial probes. Foreign configuration/profile/volume inventories and
host policy remain unchanged. A nonempty foreign collection, unexpected
named-read success, missing positive control or permitted mutation fails
the test.

This matches the [Incus v7.5.1 instance-list implementation](https://github.com/lxc/incus/blob/v7.5.1/cmd/incusd/instances_get.go):
`instancesGet` obtains a `CanView` permission checker and removes instances
the requester cannot access before rendering the response. Thus a successful
empty collection is intentional **server-side authorization filtering**,
not evidence of disclosure or a CLI-only concealment. The previous assertion
requiring a nonzero collection response was incorrect; it is replaced by
strict populated absence controls plus explicit named-read and write denials.

Other observed denials were `Certificate is restricted`, `Network not
allowed in project`, `User does not have permission for project
"unrelated"`, and explicit forbidden host-disk / low-level-config errors.
The guest reported AppArmor unavailable; these results exercise API policy,
not AppArmor confinement or container-escape resistance. The certificate-revocation fixture is now proposed but **not yet executed** on its exact head. In-flight revocation, restart/crash recovery, custom-volume attachment, populated foreign-image isolation, and state-service confused-deputy/TOCTOU checks remain outstanding. No authorization approval or cross-principal write
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
