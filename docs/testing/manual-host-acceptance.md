# Manual product acceptance on the development host (v0.2)

This is a **human-run usability and integration exercise**, not a CI test or authorization to change the host. Its primary target is the operator's **actual NixOS development host**. It complements, rather than replaces, [the staged OpenCode acceptance path](acceptance/README.md), [the existing host preflight](../operations/host/operator-preflight.md), and [the getting-started deployment boundary](../getting-started.md). In particular, a pilot preflight, a synthetic model, or a successfully started workspace **is not** evidence of live agent inference.

## Ground rules and preparation

- Reserve a 3–5 hour uninterrupted session. Use a **pinned clean `main` commit** (record `git rev-parse HEAD` and `git status --porcelain`); do not advance the checkout during the run. A dedicated worktree is appropriate so parallel work is unaffected.
- **No unattended changes to the real host.** Inspect existing Incus projects, instances, pools, volumes, images, host firewall state, NixOS settings, persistent state, SSH trust and gateway configuration before authorizing any write. The host may contain an existing pilot under a reconciler hold; preserve it. Do **not** assume resources are disposable, even if named Aginctus.
- Maintain a small **operator-owned** scratch/fixture repository separate from Aginctus and from any unrelated developer work. Workloads and Herdr workspaces must be uniquely identified and scoped to this run; record IDs before creating anything. Never give agents broad host/Incus administrative access.
- Review `services.aginctus.firewall.enable` (default true) and `services.aginctus.reconcile.enable` (default false) before *any* NixOS activation. Do not run `nixos-rebuild switch`, `aginctus doctor --fix`, `apply`, `remove`, `just update image`, destructive testenv/integration recipes, or live-model commands as part of a read-only preflight.
- Keep evidence **outside the repository and agent-writable workspaces**. Do not paste secrets, raw `config show` output, credentials, full guest metadata, tokens, IP topology, private paths, or unredacted logs into public PRs/issues. Review and redact any shared summary.

Start from the repository root:

```sh
just check host-acceptance-preflight
# Explicitly create a private local worksheet; this writes ONLY under
# your existing, operator-owned XDG state directory.
just check host-acceptance-init
```

The first command checks only local tools and repository metadata, without contacting Incus or a model. The second writes a *local* Markdown worksheet, prints its path, and does not run acceptance. Alternatively take notes in a separately secured location. Neither command approves host changes.

## Session protocol

For every phase, write **expected**, **observed**, **friction**, **evidence reference** and **result** (`pass`, `fail`, `blocked`, `not_run`). A pass requires direct human observation; do not infer it from a green automated test. Stop at a blocked phase and record the blocker rather than troubleshooting by changing host privileges or firewall policy.

| Phase | Human actions | Evidence / exit criterion |
| --- | --- | --- |
| 0. Baseline | Record git revision, NixOS generation (nonsecret identifier), CLI version, active config authority and intended Incus project. Inspect current resources and separate them from run-owned resources. | Precise baseline and ownership inventory; no writes |
| 1. First contact | Follow [getting started](../getting-started.md) and [CLI reference](../reference/cli.md) without source inspection. Try `aginctus help`, `aginctus doctor`, `aginctus status`, and inspect applicable `config`/dry-run commands. | Can explain setup, command discovery, missing prerequisites and next action |
| 2. Read-only topology | With explicit project selection, run [operator preflight](../operations/host/operator-preflight.md) and, if applicable, the [staged pilot checks](acceptance/README.md#2-read-only-pilot-inspection). Inspect the intended resource ownership and existing reconciler holds. | Correct target/project, readable and comprehensible diagnostics; no authorization inferred |
| 3. Provision + converge **(approval gate)** | After reviewing plan and rollback, explicitly approve **individual** network, image, client and workload operations described in [getting started](../getting-started.md). Record before/after inventory. Repeat a no-op dry-run, then perform an approved declarative change only if safely isolated from preexisting resources. | Expected managed resources only; understandable plan and convergence; no foreign-resource mutation |
| 4. Real coding loop **(separate approval gates)** | Register/pin Herdr trust using the [Herdr guide](../operations/workloads/herdr-machine-registration.md). Create one uniquely owned workspace, launch OpenCode and fix a failing test in the run-only fixture repository. Use [staged OpenCode acceptance](acceptance/README.md#3-opt-in-real-herdr-lifecycle-not-yet-the-full-agent-task), advancing to [live mode](acceptance/README.md#4-operator-authorized-live-nine-phase-acceptance) **only** with separately reviewed credential, egress and billing approvals. | Human observes edit and tests; independent tool trace and gateway-side evidence required before claiming live inference |
| 5. Configuration boundaries | Inspect effective NixOS/root and Home Manager/user mode without making unsolicited host activation changes. Where supported, exercise accepted-state status/diff on an operator-controlled *test* state directory; do not accept/commit state on a production store. | Can identify configuration ownership and unproven/missing enforcement; no privilege-boundary assumptions |
| 6. Failure/recovery **(separate approval gate)** | Prefer a harmless invalid CLI input or dry-run. For interruption/retry, use only a run-owned instance/workspace and record the recovery plan before inducing any fault. **Never** deliberately interrupt host firewall reconciliation, production gateway or unrelated Incus resources. | Error is actionable; recovery is comprehensible and scoped, or documented as blocked |
| 7. Reporting | Run `just check report` for hermetic health **separately**; if approved, use `just check preflight-report pilot-topology` and `opencode-runtime` for selected pilot inspection. Consider the [testenv guidance](testenv/README.md) separately; its destructive integration modes are **not** safe to run on the development host. | Report distinguishes human observations, hermetic success and live evidence tiers |
| 8. Cleanup and repeat | Close **only** the workspaces/instances created and positively identified during this run. Review exact targets before each removal. Keep existing pilot, persistent keys, images, volumes and bridges unless separately authorized. Verify an inventory diff; repeat one non-mutating workflow. | Owned cleanup observed independently; retained resources and exceptions named |

If a stage is not available on this checkout, record `blocked` or `not_run`; do not invent command flags or lower security policy to force a pass. The documentation referenced above owns the precise versioned syntax and stage-specific authority.

## Findings and release decisions

Classify defects: **P0** security breach/destructive data loss; **P1** core journey blocked or irrecoverable; **P2** significant friction/diagnostics problem; **P3** polish. Record problems even when a command eventually succeeds. Each finding should have a redacted reproduction, expected versus observed behavior, affected commit, severity, and whether it is an existing limitation or regression.

This exploratory run is **successful if it produces actionable findings**, not necessarily if every phase passes. For a later release-candidate signoff require a new clean-host deployment/repeat, all in-scope core phases passing, no unresolved P0/P1, audited resource cleanup, separate credential/egress/provider evidence where claimed, and an explicit operator decision. The exploratory run **does not satisfy** the deferred fresh-host reproducibility criterion (#580) by itself.

For a lightweight machine-inspectable report, preserve only a manually reviewed *redacted* phase/status table in your private worksheet and promote only a sanitized summary to an issue. The preflight and worksheet creator intentionally do not scrape host inventory or raw diagnostics.
