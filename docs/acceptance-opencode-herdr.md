# Owned Herdr lifecycle phases for #124

The Python implementation `scripts/acceptance/herdr-lifecycle.py` now calls documented Herdr commands for `open`, `start`, `prompt`, `wait`, `stop` and `close`. It uses the Herdr `--machine` selector, which points to a **previously saved and authenticated Herdr machine on the same machine where the CLI runs**, not an arbitrary SSH host. The pilot's intended Herdr client runs inside the `aginctus-herdr` Incus container; the host's `herdr machine list --json` is not its machine registry. The API is documented at [Herdr agent automation](https://herdr.dev/docs/agent-automation/) and [Herdr CLI reference](https://herdr.dev/docs/cli-reference/); pilot validation must confirm the pinned Herdr version supports them.

This is a partial integration, **not** complete acceptance. The live dispatcher requires all nine hooks. There is still no built-in `preflight` covering authenticated Herdr reachability and guest workspace transport, no real `credential-check` or `gateway-evidence` hook. The OpenCode gateway launcher from #123 must be provisioned and validated separately. The Herdr agent's settled state does not prove it ran tests; inspect agent tool traces, gateway logs and guest test execution independently.

## Safety and prerequisites

All phase calls require `AGINCTUS_ACCEPTANCE_HERDR_MACHINE` set to a reviewed saved machine profile and an owned random `aginctus-acceptance.XXXXXXXX` workspace path from the existing harness. `open` additionally requires a guest path `AGINCTUS_ACCEPTANCE_GUEST_ROOT=/home/agent/aginctus-acceptance/<RUN_ID>` and `AGINCTUS_ACCEPTANCE_GUEST_STAGED=<RUN_ID>`. These are **operator declarations**, not verified transport: only set them after checking the fixture is copied into an isolated guest-local directory for that unique run, owned by `agent`. Do not mount arbitrary host workspace paths or relax container networking.

A private `.herdr-owned.json` file records the Herdr-returned workspace/pane IDs and the selected machine, preventing later `stop`/`close` from targeting guessed or unrelated IDs. Closing the workspace is intentionally restricted to the recorded ID. If creation succeeds but response parsing or recording fails, the new remote workspace may remain: inspect Herdr by the unique run label and clean up that resource manually. Never automatically remove an unverified resource.

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

## Real pilot lifecycle smoke (required before merge)

The new `scripts/acceptance/herdr-smoke.py` runs **two actual remote workspace create/get/close cycles** through the saved Herdr machine profile. It requires explicit opt-in and reviewed guest directory staging and cleanup commands. It does **not** run an agent or inference. Importantly, no operator should set a fabricated `AGINCTUS_ACCEPTANCE_GUEST_STAGED` value without checking actual guest readiness.

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

Review the Herdr workspace list afterwards and confirm that neither run left a workspace or a guest directory. This only establishes remote workspace operations, not the full `agent start/prompt/wait/stop` contract. A separate real agent-lifecycle run is still required before regarding #233 as a validated real lifecycle integration. Do **not** merge solely on mocked lifecycle assertions.

## Pilot topology correction

The host-side development shell's `herdr machine list --json` returned `[]`. That is not evidence that the dedicated Herdr client container lacks remote machines: Herdr settings are scoped to the account and machine running the CLI. Check the managed Herdr client instance independently using authenticated Incus exec (read-only):

```sh
incus exec aginctus-herdr -- herdr machine list --json
incus exec aginctus-herdr -- herdr --version
incus exec aginctus-herdr -- herdr status server
```

Use the configured Herdr client instance name in place of `aginctus-herdr` if overridden. If the client also has no registered machines, inspect Aginctus SSH provisioning before trying to register anything manually. Do not create a host-local Herdr machine entry as a workaround: it would bypass the Aginctus client identity, host key pinning and registration workflow. The existing lifecycle script executes `herdr` locally; it must be adapted to run in the intended **client container context** or explicitly tested with the correct remote profile before live execution. Likewise, test guest staging with the managed, authenticated Incus interface; do not invent unverified SSH shortcuts.
