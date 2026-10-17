# Owned Herdr lifecycle phases for #124

The Python implementation `scripts/acceptance/herdr-lifecycle.py` now calls documented Herdr commands for `open`, `start`, `prompt`, `wait`, `stop` and `close`. It uses the Herdr `--machine` selector, which points to a **previously saved and authenticated Herdr machine**, not an arbitrary SSH host. The API is documented at [Herdr agent automation](https://herdr.dev/docs/agent-automation/) and [Herdr CLI reference](https://herdr.dev/docs/cli-reference/); pilot validation must confirm the pinned Herdr version supports them.

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
