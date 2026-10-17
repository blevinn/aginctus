# OpenCode gateway runtime readiness gate (#124)

This narrow follow-up to #233 checks whether the **real pilot guest** has the provisioned OpenCode Responses runtime configuration needed before enabling model-backed Herdr prompts. It runs only read-only Incus guest commands, including protected metadata and DNS checks. It **never reads the workload key**, calls a model, or emits secret configuration values.

Run from the NixOS pilot checkout:

```sh
nix develop -c python3 -m unittest discover -s scripts/acceptance -p 'test_opencode_inference_preflight.py' -v
nix develop -c python3 -m py_compile scripts/acceptance/opencode-inference-preflight.py
nix develop -c python3 scripts/acceptance/opencode-inference-preflight.py
```

The gate requires a regular `/var/lib/aginctus/opencode/workload-key` with mode 0600, owned by `agent`, and readable by that account. It reads the **non-secret** `gateway.json` only, validates Responses protocol, endpoint syntax and approved model presence, and resolves the configured hostname from the guest.

**This check is necessary, not sufficient.** It does not establish that the hostname resolves to an isolated gateway, that requests reach LiteLLM, that a credential is authorized, or that a real upstream model responds. Those require independent gateway-side audit, a proven working live prompt, and network/credential isolation assertions. Failures produce categorized text without printing actual keys, endpoint or model data.

After it passes, operator can consider the separately opted-in prompt test in `docs/acceptance-opencode-herdr.md`. Do not use synthetic gateway evidence to close #124.
