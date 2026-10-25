# Owned Herdr lifecycle phases for #124

This is **stage 3** of the [OpenCode acceptance decision path](acceptance-opencode.md#3-opt-in-real-herdr-lifecycle-not-yet-the-full-agent-task). A workspace-only pass demonstrates owned Herdr operations, not model inference or agent-executed tests. The #124 issue's closure does not replace the independent #122/#257 live provider and egress gates.

The Python implementation `scripts/acceptance/herdr-lifecycle.py` now calls documented Herdr commands for `open`, `start`, `prompt`, `wait`, `stop` and `close`. It uses the Herdr `--machine` selector, which points to a **previously saved and authenticated Herdr machine on the same machine where the CLI runs**, not an arbitrary SSH host. The pilot's intended Herdr client runs inside the `aginctus-herdr` Incus container; the host's `herdr machine list --json` is not its machine registry. The API is documented at [Herdr agent automation](https://herdr.dev/docs/agent-automation/) and [Herdr CLI reference](https://herdr.dev/docs/cli-reference/); pilot validation must confirm the pinned Herdr version supports them.

This is a partial integration, **not** complete acceptance. The live dispatcher requires all nine hooks. There is still no built-in `preflight` covering authenticated Herdr reachability and guest workspace transport, no real `credential-check` or `gateway-evidence` hook. The OpenCode gateway launcher from #123 must be provisioned and validated separately. The Herdr agent's settled state does not prove it ran tests; inspect agent tool traces, gateway logs and guest test execution independently.

## Safety and prerequisites

All phase calls require `AGINCTUS_ACCEPTANCE_HERDR_MACHINE` set to a reviewed saved machine profile and an owned random `aginctus-acceptance.XXXXXXXX` workspace path from the existing harness. `open` additionally requires a guest path `AGINCTUS_ACCEPTANCE_GUEST_ROOT=/home/agent/aginctus-acceptance/<RUN_ID>` and `AGINCTUS_ACCEPTANCE_GUEST_STAGED=<RUN_ID>`. These are **operator declarations**, not verified transport: only set them after checking the fixture is copied into an isolated guest-local directory for that unique run, owned by `agent`. Do not mount arbitrary host workspace paths or relax container networking.

A private `.herdr-owned.json` file records the Herdr-returned workspace/pane IDs and the selected machine, preventing later `stop`/`close` from targeting guessed or unrelated IDs. Closing the workspace is intentionally restricted to the recorded ID. If creation succeeds but response parsing or recording fails, the new remote workspace may remain: inspect Herdr by the unique run label and clean up that resource manually. Never automatically remove an unverified resource.

### Independent Herdr workspace close readback

The owned `close` phase now queries `herdr --machine <reviewed-machine> workspace list` **before** issuing `workspace close <owned-ID>`. The returned
JSON must contain a bounded `result.type=workspace_list` inventory with
unique valid workspace IDs; the exact ID in the private ownership record
must be present and be the **only** workspace with that run label. It
refuses to close an absent ID, a mismatched label, a second same-run-label
workspace, or an ambiguous/malformed inventory. Other workspaces may have
no labels; they are never targeted. After closing, no workspace with that
run label may remain.

After the close command returns successfully, the phase repeats this
read-only query **against the same managed machine**. Only when that exact
workspace ID is absent does it remove the local ownership record and
acknowledge close success. A stale inventory still containing the ID,
a malformed response, or a transport failure causes a nonzero result and
retains the private ownership metadata while the enclosing disposable
directory still exists. The smoke harness discards its temporary directory
on exit, so investigate failed run IDs promptly through the managed Herdr
client rather than relying on a persistent local ownership file. There is no
automatic retry, credential creation, new agent permission, deletion of
unrelated resources, or fallback to a local machine. This is exercised by
hostless mock-inventory tests; no remote operation runs in normal CI.

This verifies only that the selected **Herdr workspace** disappeared from
the managed Herdr inventory at readback time. It cannot establish guest
directory or volume deletion, process termination beyond Herdr's own
workspace model, upstream Responses completion, or gateway/egress controls.
The workspace smoke wrapper now also returns failure if its scoped
`close` or guest-directory cleanup helper fails; warnings cannot be
followed by a misleading PASS. The helper is still attempted even when
the close operation fails.

The generic live dispatcher may use different operator-supplied hooks, so
the runner's existing `stop`/`close` command statuses do not automatically
become independently verified remote cleanup in the private result or
aggregate coverage. Keep `cleanup: not_verified` until the trusted runner
itself integrates an independently checked full owned-resource inventory.

The script is used as a **phase implementation**. Install small audited executable wrapper scripts named `open`, `start`, `prompt`, `wait`, `stop`, `close` in the hook directory. Each wrapper invokes `python3 /absolute/repo/scripts/acceptance/herdr-lifecycle.py <phase> "$@"` as a normal command (not `eval`). `preflight`, `credential-check`, and `gateway-evidence` must be implemented separately, and must fail closed rather than emit synthetic markers. The existing dispatcher refuses incomplete directories.

## Deterministic checks

```sh
nix develop -c python3 -m unittest discover -s scripts/acceptance -p 'test_herdr_lifecycle.py' -v
nix develop -c python3 -m py_compile scripts/acceptance/herdr-lifecycle.py
bash scripts/acceptance/opencode-live-adapter-test.sh
bash scripts/acceptance/opencode-test.sh
git diff --check origin/main...HEAD
just check
```

Live Herdr execution remains opt-in and **is not established by these test doubles**. Further live work must implement protected fixture transport/verification, real runtime key provisioning, credential checks and independent gateway-side inference correlation, without logging secrets or allowing direct-provider fallback.

## Opt-in real pilot workspace smoke (not release sign-off)

The new `scripts/acceptance/herdr-smoke.py` runs **two actual remote workspace create/get/close cycles** through the saved Herdr machine profile. It requires explicit opt-in and reviewed guest directory staging and cleanup commands.

There are two opt-in levels (see the module docstring):

- base: ``AGINCTUS_ACCEPTANCE_HERDR_SMOKE=I_UNDERSTAND`` — runs the
  Herdr workspace lifecycle through ``open`` / ``inspect``. **No agent
  is launched and no inference is required.**
- agent: ``AGINCTUS_ACCEPTANCE_HERDR_AGENT_SMOKE=I_UNDERSTAND`` —
  additionally launches the OpenCode agent, inspects it via
  ``herdr agent get``, and (if
  ``AGINCTUS_ACCEPTANCE_HERDR_AGENT_PROMPT`` is also set) issues a
  single constrained prompt. This level requires an
  inference-capable environment.

Importantly, no operator should set a fabricated `AGINCTUS_ACCEPTANCE_GUEST_STAGED` value without checking actual guest readiness.

First inspect the pinned pilot version and command contract:

```sh
nix develop -c bash -c 'command -v herdr || echo "Herdr CLI not on PATH"'
herdr --version
herdr machine status '<reviewed-profile>' --json
herdr workspace create --help
herdr workspace get --help
herdr workspace close --help
herdr agent start --help
herdr agent wait --help
```

Provide two small, executable, operator-reviewed programs:

- `AGINCTUS_ACCEPTANCE_HERDR_STAGE`: accepts `LOCAL_WORKSPACE RUN_ID GUEST_PATH`; ensures the dedicated guest-local directory is **new**, exists, is owned by agent, and is safe to use; should fail on collisions. Do not rewrite or mount user paths.
- `AGINCTUS_ACCEPTANCE_HERDR_CLEANUP`: same arguments; removes **only** the resource matching the supplied owned RUN_ID and GUEST_PATH, with verified ownership. Must be safe after a partial stage. It must not remove other guest directories or persistent volumes.

Then:

```sh
export AGINCTUS_ACCEPTANCE_HERDR_SMOKE=I_UNDERSTAND
export AGINCTUS_ACCEPTANCE_HERDR_MACHINE='<reviewed-profile>'
export AGINCTUS_ACCEPTANCE_HERDR_SMOKE_ROOT=/home/agent/aginctus-acceptance
export AGINCTUS_ACCEPTANCE_HERDR_STAGE=/absolute/path/to/reviewed-stage
export AGINCTUS_ACCEPTANCE_HERDR_CLEANUP=/absolute/path/to/reviewed-cleanup
nix develop -c python3 scripts/acceptance/herdr-smoke.py
```

Review the Herdr workspace list afterwards and confirm that neither run left a workspace or a guest directory. This only establishes remote workspace operations, not the full `agent start/prompt/wait/stop` contract. A separate real agent-lifecycle run is still required before regarding #233 as a validated real lifecycle integration. Do **not** treat mocked lifecycle assertions as real remote-lifecycle or release-acceptance evidence.

## Pilot topology correction

The host-side development shell's `herdr machine list --json` returned `[]`. That is not evidence that the dedicated Herdr client container lacks remote machines: Herdr settings are scoped to the account and machine running the CLI. Check the managed Herdr client instance independently using authenticated Incus exec (read-only):

```sh
incus exec aginctus-herdr -- herdr machine list --json
incus exec aginctus-herdr -- herdr --version
incus exec aginctus-herdr -- herdr status server
```

Use the configured Herdr client instance name in place of `aginctus-herdr` if overridden. If the client also has no registered machines, inspect Aginctus SSH provisioning before trying to register anything manually. Do not create a host-local Herdr machine entry as a workaround: it would bypass the Aginctus client identity, host key pinning and registration workflow. The existing lifecycle script executes `herdr` locally; it must be adapted to run in the intended **client container context** or explicitly tested with the correct remote profile before live execution. Likewise, test guest staging with the managed, authenticated Incus interface; do not invent unverified SSH shortcuts.

## Managed guest helper for workspace-only smoke

`scripts/acceptance/guest-owned.py` now supplies the stage and cleanup actions via authenticated `incus exec aginctus-dev -- runuser -u agent -- /bin/sh -c ...`. It creates a **new empty** workspace directory under `/home/agent/aginctus-acceptance` with a private run-ID marker. Cleanup refuses directories with unexpected files and will never delete the existing Herdr workspace `w1`. The guest runs as `agent`, and the host never receives any SSH private key.

The smoke defaults to this helper for staging and cleanup. The helper requires `/bin/sh`, `stat`, `find`, `mkdir`, `chmod`, `rm`, and `rmdir` in the workload; check these and the `agent` home before opt-in. `AGINCTUS_ACCEPTANCE_WORKLOAD` can select an explicitly reviewed instance if the workload name differs. The helper is intended **only for the empty workspace smoke** and does not yet stage a coding fixture. Cleanup failures require manual investigation; do not forcibly remove unexpected content.

### Separate guest-directory removal readback

After the smoke's operator-selected guest cleanup helper runs, the trusted
host invokes the repository's fixed `guest-owned.py inspect-absent` action
as a **second, read-only Incus guest execution** against the selected workload.
That action requires the expected guest parent directory to be a real,
owner-controlled, non-world-writable directory, and then checks that this
run's exact guest path is absent as both an existing entry and a dangling
symlink. It does not create, remove, rename or walk other directories.

The independent readback runs even when a custom
`AGINCTUS_ACCEPTANCE_HERDR_CLEANUP` hook returned success or failed. A
lingering directory, unsafe parent, missing guest readback capability or
unreachable workload fails the smoke rather than producing a final PASS.
The existing cleanup attempt is always made, and a primary phase error is
preserved. The result proves only **guest-directory absence at the time of
this read-only probe**, not the absence of agent processes, volumes, other
Incus instances, provider sessions, or any end-to-end inference.

No `inspect-absent` action is invoked by `just check ci` against a real
workload: normal tests exercise the fixed shell program on a local temporary
directory and use injected process doubles. Real readback only occurs
inside a separately authorized Herdr smoke. This does not promote the
main acceptance report's `cleanup` or `complete_acceptance` fields.

Before mutating the pilot, perform read-only checks:

```sh
nix develop -c incus exec aginctus-dev -- runuser -u agent -- /bin/sh -c 'command -v stat && command -v find && command -v mkdir && command -v chmod && command -v rm && command -v rmdir'
nix develop -c incus exec aginctus-dev -- runuser -u agent -- test -d /home/agent
nix develop -c incus exec aginctus-herdr -- herdr --machine aginctus-dev workspace list
```

After checking these prerequisites and reviewing the helper and ownership model, a scoped **workspace-only** smoke can run with:

```sh
AGINCTUS_ACCEPTANCE_HERDR_SMOKE=I_UNDERSTAND \
AGINCTUS_ACCEPTANCE_HERDR_MACHINE=aginctus-dev \
AGINCTUS_ACCEPTANCE_HERDR_SMOKE_ROOT=/home/agent/aginctus-acceptance \
nix develop -c python3 scripts/acceptance/herdr-smoke.py
```

Do not run this if there are previously unreviewed failed runs with the same IDs. Inspect newly created resources after each failure. This still does not verify actual agent lifecycle.

The managed guest staging helper uses a fixed POSIX shell program rather than requiring Python inside `aginctus-dev`. Repeat the guest prerequisite and deterministic checks before attempting an opted-in smoke; a failed run supplies no positive remote-lifecycle result.

## Opt-in real agent lifecycle (separate from workspace smoke)

After the workspace-only smoke has passed, run a further pilot test **only if OpenCode runtime readiness has been independently verified** *and* the inference prerequisites below have been confirmed. The following starts a real OpenCode agent in each newly owned disposable pane, checks Herdr's agent inspection API, attempts an interrupt, and closes only the recorded workspace:

```sh
AGINCTUS_ACCEPTANCE_HERDR_SMOKE=I_UNDERSTAND \
AGINCTUS_ACCEPTANCE_HERDR_AGENT_SMOKE=I_UNDERSTAND \
AGINCTUS_ACCEPTANCE_HERDR_MACHINE=aginctus-dev \
AGINCTUS_ACCEPTANCE_HERDR_SMOKE_ROOT=/home/agent/aginctus-acceptance \
nix develop -c python3 scripts/acceptance/herdr-smoke.py
```

Adding `AGINCTUS_ACCEPTANCE_HERDR_AGENT_PROMPT=I_UNDERSTAND` additionally sends a minimal no-file-change prompt and runs a bounded wait. **Do not opt into prompting until the gateway workload key, model selection, and private inference endpoint are verified**; agent readiness alone is not proof of inference. This agent test is still unvalidated against the pilot. The non-root guest cleanup refuses unexpected files, so if OpenCode writes persistent files under the disposable cwd, a failed cleanup should be inspected manually. Neither the test nor the operator should remove unrelated resources, including `w1`.
