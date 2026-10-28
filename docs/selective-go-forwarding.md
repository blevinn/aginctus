# Selective OpenCode Go forwarding

This is the opt-in native Responses policy for `aginctus-go-pilot`.
Implementation and isolated protocol acceptance are available; **live deployment
and provider acceptance remain pending** under [#456](https://github.com/blevinn/aginctus/issues/456),
[#122](https://github.com/blevinn/aginctus/issues/122) and
[#123](https://github.com/blevinn/aginctus/issues/123).

## Policy and packaging

The gateway requires a native `ses_...` conversation ID and the original
identifying OpenCode User-Agent. Only `x-opencode-session` and `User-Agent`
are derived from client metadata and passed upstream. The actual model's
provider credential remains separate. Missing, invalid or conflicting metadata
returns sanitized HTTP 400 before provider execution. The approved alias rejects
Chat Completions and every other non-Responses callback type; unrelated aliases
are unchanged. Both native session-header spellings are accepted only when they
agree. Bare OpenCode UAs and its bounded, ordered provider-utils/Bun trailer are
supported; arbitrary suffixes and control characters are rejected.

Syntax checks are **not client attestation**. An authenticated caller can imitate
valid-looking metadata. Authenticity evidence comes from running the genuine
client and comparing its own session events with the provider-bound request;
the callback does not invent or cryptographically certify session IDs.

The built Go CLI directly embeds the two canonical Python modules and a
callback-only YAML, through `scripts/gateway/assets.go`. Both gateway renderers
deliver exactly these public inline Compose configs:

| Target | Purpose |
| --- | --- |
| `/app/aginctus-go/config.yaml` | Loads only the selective callback; no model routes or credentials |
| `/app/aginctus-go/litellm_opencode_go_hook.py` | Actual LiteLLM callback and sanitized client-error adapter |
| `/app/aginctus-go/opencode_go_forwarding.py` | Alias/protocol-scoped metadata validation and allowlist |

Only LiteLLM receives them, together with `CONFIG_FILE_PATH` and `PYTHONPATH`.
PostgreSQL, persistent model/key state and runtime secret initialization retain
their existing configuration. Source dollars are escaped at the Compose boundary
and restored by the pinned loader. No custom image, host bind mount, startup
download, additional Python dependency or external artifact publication is needed.
The pinned OCI supplies all Python dependencies; callback import rejects any
installed LiteLLM version other than `1.103.0`.

The source revision, CLI package identity, SHA-256 bundle manifest and immutable
OCI reference are separate review inputs. A manifest is a reproducibility check,
not publisher authentication or a signature. Changing the runtime requires a
reviewed version-guard update and the full protocol acceptance again.

## Explicit configuration

`gateway.litellm.selectiveGoForwarding` is a boolean, default **false**, shared
by CLI configuration, NixOS and Home Manager. Enabling it requires an immutable
`ghcr.io/berriai/litellm[:tag]@sha256:<64 lowercase hex>` reference. It rejects
stock `forwardClientHeaders = true` and **any** nonempty model-group list, even
one naming another alias. `false` and `[]` are compatible. Invalid effective
configuration fails at gateway validation/rendering before deployment.

For review, the following digest was exercised locally on x86_64-linux; verify
its provenance and platform independently before choosing it for deployment:

```nix
services.aginctus.settings.gateway = {
  images.litellm = "ghcr.io/berriai/litellm:v1.103.0@sha256:bd089afdcd35b894b14a93f9743cdc8b591f82da1a38dd43a010a7b0c9de5fd7";
  litellm = {
    selectiveGoForwarding = true;
    forwardClientHeaders = false;
  };
};
```

This declaration does not register an upstream model or provide a key. Retain
the existing protected master/salt state and separately managed DB registry.
For host configuration review, keep automatic gateway reconciliation disabled;
do not deploy the host configuration as part of an offline forwarding test.

Read-only previews, using the intended configuration source and built CLI:

```sh
aginctus config get gateway.litellm.selectiveGoForwarding
aginctus config get gateway.images.litellm
aginctus gateway validate
aginctus gateway render
```

The existing stock `forwardClientHeaders` interface remains for compatibility.
Its model list selects aliases, **not header names**. Actual native Responses
auditing found unrelated `x-*` and `anthropic-beta` forwarding and loss of the
original UA. It is unsuitable for this Go route; do not activate it as fallback.

## Offline acceptance and exact scope

The existing `probe_pinned_litellm_responses.py` runs only with explicit opt-in
and a loopback-only namespace. All credentials and providers are synthetic.
The real production-launcher mode consumes direct CLI-rendered Compose through
the pinned loader, without altering the delivered callback YAML. It creates
its own temporary PostgreSQL cluster inside the disposable container, registers
synthetic models, generates a restricted key, and uses the real pinned OpenCode
through production `launch.py`.

Observed on x86_64-linux:

| Gate | Observation |
| --- | --- |
| Unit, lint, Nix and renderer parity | All eight hermetic suites pass |
| Packaged callback loading | Real pinned LiteLLM passes; missing YAML or either module stops startup |
| Genuine client identity | New/resumed/independent client event IDs match provider-bound sessions |
| Native streaming | Marker text and terminal client `step_finish: stop`, not merely HTTP 200 |
| User-Agent | Actual original wire UA preserved, including the SDK trailer |
| Credentials | Generated workload/admin keys absent upstream; synthetic provider key used |
| Model isolation | Restricted virtual key denied unrelated and unregistered aliases; admin positive control succeeds without Go metadata |
| Adversarial metadata | Missing/malformed/conflicting session, invalid UA and body spoof rejected without upstream calls |
| Unsupported protocol | Valid Chat Completions request to approved alias rejected with HTTP 400 and zero upstream calls |
| Cleanup | Synthetic key deletion followed by HTTP 401 and zero upstream calls |

These tests do **not** boot the workload image, traverse Herdr/Incus or exercise
the pilot firewall. They use a cached PostgreSQL **18.6 binary**, not the declared
production PostgreSQL image, and do not validate upgrades or persistent gateway
restart. aarch64 execution and a real wrong-version OCI image are unrun; the
version rejection has import-level regression coverage. Fast, single-packet
buffered replies expose the real client's event-consumer completion defect,
tracked in [#542](https://github.com/blevinn/aginctus/issues/542): completed
parts exist in its disposable database, but the CLI can exit before printing
terminal text/stop events. This reproduces directly and through the gateway.
The accepted fixture sends complete incremental SSE and does not establish
buffered-output reliability. A minimal source fix is feasible, but the exact
pinned source/dependency/tool closure is not cached and its realization is
blocked; no unverified patch or sleep-based production workaround is shipped.
External Go entitlement, provider
accounting, billing and model-driven filesystem tools remain live release gates.

### Reproduce without contacting providers

Prerequisites: cached locked Nix inputs, cached exact image, existing rootless
Podman user-namespace setup, and a cached Nix-store PostgreSQL bin directory.
Set `PODMAN` and `POSTGRES_BIN` to those existing absolute paths. No host service
or privilege changes are part of this test. The container rootfs is disposable
and writable for Prisma initialization; the repository and Nix store mounts are
read-only. Container networking is disabled, image pulls are forbidden, and no
host home, Incus socket, live DB or credential directory is mounted.

Run from this checkout, with no production credentials in the environment.
These commands do not use shell-exiting modes; inspect each result before
proceeding to the next group:

```sh
IMAGE=ghcr.io/berriai/litellm:v1.103.0@sha256:bd089afdcd35b894b14a93f9743cdc8b591f82da1a38dd43a010a7b0c9de5fd7
WORK="$(mktemp -d /tmp/aginctus-go-offline.XXXXXX)"
RUNTIME="$(nix --quiet build --offline --no-link --print-out-paths .#opencode-runtime)/bin/opencode"
nix --quiet develop --offline -c env AGINCTUS_EXPORT_GO_BUNDLE="$WORK/bundle" \
  go test ./scripts/gateway -count=1
nix --quiet develop --offline -c go run ./cmd/aginctus \
  --config=gateway.litellm.selectiveGoForwarding=true \
  --config="gateway.images.litellm=$IMAGE" gateway render > "$WORK/compose.yaml"
nix --quiet develop --offline -c env AGINCTUS_EXPORT_GO_DEPLOYMENT="$WORK/deployment" \
  AGINCTUS_GO_COMPOSE_FILE="$WORK/compose.yaml" AGINCTUS_PROBE_LITELLM_IMAGE="$IMAGE" \
  go test ./internal/gateway -run TestExportGoDeployment -count=1
```

Require successful exports before running either probe:

```sh
"$PODMAN" run --rm --pull=never --network none \
  -e AGINCTUS_ISOLATED_LITELLM_PROBE=1 -e LITELLM_LOCAL_MODEL_COST_MAP=True \
  -v "$PWD:/probe:ro" -v "$WORK/bundle:/bundle:ro" \
  --entrypoint /app/.venv/bin/python "$IMAGE" \
  /probe/scripts/gateway/probe_pinned_litellm_responses.py \
  /probe/deploy/litellm-go/model-group-forwarding.yaml --packaged-hook /bundle

"$PODMAN" run --rm --pull=never --network none \
  -e AGINCTUS_ISOLATED_LITELLM_PROBE=1 -e LITELLM_LOCAL_MODEL_COST_MAP=True \
  -v "$PWD:/probe:ro" -v "$WORK/deployment:/bundle:ro" -v /nix/store:/nix/store:ro \
  --entrypoint /app/.venv/bin/python "$IMAGE" \
  /probe/scripts/gateway/probe_pinned_litellm_responses.py \
  /probe/deploy/litellm-go/model-group-forwarding.yaml --packaged-hook /bundle \
  --production-launcher /probe/guest/opencode/launch.py \
  --opencode "$RUNTIME" --postgres-bin "$POSTGRES_BIN"

nix --quiet develop --offline -c just check-all
```

Require both probes to exit zero and print their fixed PASS classifications.
Do not substitute fixture unit-test output for a real OCI result. Exported
bundles contain public source and synthetic configuration only. The container,
DB and client state disappear with its removal; no live keys are involved.

## Inspect deployed delivery without inference

After an approved deployment, export the **small bundle**, not the deployment
fixture, from its exact reviewed checkout/package revision. Independently map
the approved OCI digest to the Incus base-image fingerprint using trusted image
inventory; never copy the current instance fingerprint merely to make it match.
Incus fingerprints and OCI manifest digests are different identifiers.

The importer removes a tag when a digest is present and maps normalized image
references longer than Incus's 64-character alias limit to `ic-image-` followed
by the lowercase, unpadded Base32 encoding of the full SHA-256 of that reference.
The registry pull still uses the complete immutable reference. Cache aliases are
bounded independently with their platform included. The audit derives this exact
mapping; a mutable tag or arbitrary short alias is not an acceptable substitute.
The pinned incus-compose source is a minimal alias fix on its release base,
without unrelated upstream development changes.

```sh
nix --quiet develop --offline -c env AGINCTUS_EXPORT_GO_BUNDLE="$BUNDLE_DIR" \
  go test ./scripts/gateway -count=1
nix --quiet develop --offline -c python3 scripts/gateway/audit_selective_go.py \
  --project "$PROJECT" --instance "$INSTANCE" --bundle-dir "$BUNDLE_DIR" \
  --expected-incus-fingerprint "$VERIFIED_INCUS_FINGERPRINT" \
  --expected-image "$DIGEST_QUALIFIED_LITELLM_IMAGE"
```

All arguments are required. The tool verifies exact source/manifest bytes,
managed Compose ownership and service identity, image alias and fingerprint,
configured/exec environment, root-owned non-writable non-symlink delivery,
installed LiteLLM metadata and loopback readiness/liveliness. It emits only
allowlisted JSON classifications. It never imports the callback, prints expanded
configuration or inspects keys. PASS is **not** proof that an already running
proxy loaded the files; startup and actual request acceptance remain necessary.
The audit has fixture/subprocess coverage; it has not been run against the pilot.

## Migration, activation and rollback

The setting changes desired state only. Existing installations are not silently
modified. Image, environment, config-path and source-content changes need an
explicit change window; the current Compose driver does not fully converge
existing instances. In particular:

- Changing image aliases/fingerprints or existing environment values fails as
  drift. Config-path removal also fails closed rather than reporting opt-out.
- Files are delivered during the stopped-instance start path. Applying a render
  to an already running instance does not prove files were rewritten or loaded.
- Source hashes can change while target paths remain equal. Compare actual
  delivery with the exact new bundle; reload/restart needs separate authorization.
- Removing a declaration does not delete old environment fields or guest files.
  Rollback must explicitly restore/remove both config and Python path overrides.

Before a **human-authorized LiteLLM-only** migration, record privately:

1. Exact project/instance ownership, service label, source image/fingerprint and
   expected new mapping; current config/Python paths and old public file hashes.
2. Existing PostgreSQL readiness, registered aliases, scoped-key state, unchanged
   persistent master/salt state and every LiteLLM storage attachment. Preserve
   any existing rollback material under the operator's normal secret policy.
3. New effective render and package revision; the three public delivered files;
   both offline probe results. Confirm no accidental stock forwarding declaration.
4. An explicit narrow instance/config/image action plan. A tag-to-digest image
   change may require LiteLLM instance recreation; do not fake `user.image_alias`
   or base-image markers to suppress drift. Review all persistent attachments
   before authorizing that action. PostgreSQL recreation/restart, host network
   changes, salt/key rotation and unscoped `gateway apply` are outside this plan.

No generic live mutation command is prescribed: the observed instance/image
mapping and storage determine the safe action. The required approval is narrowly
**delivery and reload/recreation of the verified owned LiteLLM instance**, with
PostgreSQL and secret state preserved. This workstream has not performed it.

After the approved action, run the delivery audit, readiness and existing scoped
registry/key checks. Establish fresh-process loading and real forwarding in the
separately authorized acceptance below. Missing assets, version mismatch, failed
health or hash drift halt rollout; restore the previous trusted image/config and
environment in the same narrow scope. Recheck readiness and model/credential
continuity. Restoring an older broad-forwarding configuration does **not** make
it safe for Go traffic: disable Go consumers until the selective policy is valid.

## Final operator acceptance sequence

1. Human merges the reviewed stack in parent order, retargeting each child and
   requiring its hosted checks. Pin the resulting Aginctus revision.
2. Repeat both offline probes, including restricted-key deletion/denial. Complete
   disposable guest-delivery/production-PostgreSQL acceptance where available;
   record the remaining Incus/guest boundary separately from loopback proof.
3. Approve the narrowly scoped deployment plan above. Audit delivery and confirm
   active loading, private gateway readiness and existing model registry without
   issuing credentials or calling a provider as a health check.
4. Reconfirm effective gateway-only egress, workload/PostgreSQL direct-provider
   denial, no public listener, and private/host destination restrictions using
   [firewall pilot validation](firewall-pilot-validation.md).
5. Obtain **separate authorization** for one bounded genuine coding request using
   the operator's entitled Go model. Use the existing
   [restricted-key procedure](gateway-workload-key-pilot.md), protected system
   state and a new one-hour/$1/5-RPM/concurrency-one alias-scoped key. LiteLLM's
   dollar budget is not independent proof of provider subscription accounting.
6. Provision only the approved owned workload using the documented
   [OpenCode path](workloads/opencode.md), and invoke it through Herdr with the
   native session plugin. Require terminal output plus redacted gateway/provider
   correlation and genuine session/UA continuity. Do not replace this with the
   generic nonce smoke, manufactured session headers or a direct provider call.
7. Always revoke the exact test key and verify denial with the guarded tool;
   preserve protected evidence on uncertain cleanup. Record only classifications
   and safe correlation, not headers, keys, transcripts or raw spend-log rows.
8. Only then progress #121's model-driven filesystem/tool task and #124's
   repeatable all-up proof. Keep #456/#122/#123/#118 open until their distinct
   live requirements have evidence; offline completeness is not release approval.
