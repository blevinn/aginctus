# Pinned OpenCode runtime validation

This records deterministic **no-provider** validation for
[#542](https://github.com/blevinn/aginctus/issues/542). It does not establish
fresh-host deployment, provider billing or human release approval.

## Source and artifact identity

Executed against clean Aginctus source
`57c2da642f5bcb400bdc4430b8102c2a181e877b`, with its unchanged lockfile.
`nix/patched-opencode.nix` appends the event-drain source patch and asserts
OpenCode `1.15.10`. The exported runtime and workload use the same binding.

```sh
nix --quiet build --offline --no-update-lock-file --no-link --print-out-paths .#opencode-runtime
nix --quiet derivation show .#opencode-runtime
nix --quiet build --offline --no-update-lock-file --no-link .#opencode-workload
```

All three succeeded. Derivation inspection identifies:

| Input/output | Store identity |
| --- | --- |
| Patched derivation | `03lwixabz0phhf0x4kfl9nz5nq92h1z1-opencode-1.15.10.drv` |
| Patched runtime | `zcz2hpxb4grwza0wk1wzmryizx6lb5x5-opencode-1.15.10` |
| Applied patch | `vzgsjxrgn7b7mdfncnhzksafdgzdk687-opencode-1.15.10-run-event-drain.patch` |
| Original source | `4ckl0p6mafmm242c9qv3sln5dbd9rr9j-source` |
| Original fixed-output dependencies | `rjag9b9m3n5i19ijga3l1bvnv6qhx4p6-opencode-node_modules-1.15.10` |

This is **not** the historical unpatched `np0i6dxm...` runtime. The output was
available locally; successful realization is not a new clean-room compilation
or independent publisher provenance claim.

## Observed results

The unchanged `completed_native_session()` requires marker text, native
`step_finish.reason == "stop"`, one genuine session, matching resume identity
and no error event. Both modes use byte-identical `response_events()`;
buffered mode writes/flushes once without sleep.

| Path | Incremental | Fully buffered |
| --- | --- | --- |
| Production launcher directly to synthetic upstream | PASS: text/stop | PASS: text/stop |
| Launcher through pinned LiteLLM/disposable PostgreSQL | PASS: new/resumed/independent sessions | PASS: new/resumed/independent sessions |

Both production runs also passed Responses-only Chat Completions rejection
(400, zero upstream calls), native session/UA preservation, synthetic
provider/admin/workload credential separation, restricted-key unrelated and
unregistered model denial, anonymous and malformed/missing/conflicting metadata
controls, body spoof/injection controls, and key deletion followed by HTTP 401
with zero upstream calls and local key removal. Every completed command exited
zero and emitted its fixed PASS classifications. Assertions/timing were unchanged.

A separate local synthetic driver exercised the same launcher/runtime with
provider HTTP 401 and in-flight request SIGINT to its isolated process group.
The error emitted a native error event; both cases terminated nonzero within
40 seconds after the provider was reached. Initial driver attempts used an
insufficient 30-second cold-start allowance and did not reach the fixture;
the final setup bound matched the existing probe's 120-second allowance.
This did not alter the patched drain timer or buffered fixture. Raw output was
suppressed. Internal subscription EOF and post-prompt drain-timeout injection
were **not run**; external cancellation is not proof of those internal paths.

The first production buffered command exceeded the outer tool's 120-second
capture bound; its disposable container subsequently exited zero. The complete
buffered command was captured with a 600-second allowance and all classifications
above observed. No firewall/topology VM was rerun. No real credentials, Incus
socket, host home or pilot DB were mounted.

## Reproduce the decisive four cases

First follow the [offline export instructions](selective-go-forwarding.md#reproduce-without-contacting-providers)
to set `WORK`, `RUNTIME`, `IMAGE`, `PODMAN`, `POSTGRES_BIN` and export
`WORK/deployment`. The observed PostgreSQL was cached Nix-store **18.6**, not
the production OCI. LiteLLM used the exact digest in that guide. Existing
rootless Podman setup is required. Allow ten minutes per production command.

```sh
for mode in incremental buffered; do
  "$PODMAN" run --rm --pull=never --network none \
    -e AGINCTUS_ISOLATED_LITELLM_PROBE=1 -e PYTHONDONTWRITEBYTECODE=1 \
    -e PYTHONUNBUFFERED=1 -e LITELLM_LOCAL_MODEL_COST_MAP=True \
    -v "$PWD:/probe:ro" -v /nix/store:/nix/store:ro \
    --entrypoint /app/.venv/bin/python "$IMAGE" \
    /probe/scripts/gateway/probe_pinned_litellm_responses.py \
    /probe/deploy/litellm-go/model-group-forwarding.yaml --direct-only \
    --production-launcher /probe/guest/opencode/launch.py \
    --opencode "$RUNTIME" --delivery-mode "$mode" || break

  "$PODMAN" run --rm --pull=never --network none \
    -e AGINCTUS_ISOLATED_LITELLM_PROBE=1 -e PYTHONDONTWRITEBYTECODE=1 \
    -e PYTHONUNBUFFERED=1 -e LITELLM_LOCAL_MODEL_COST_MAP=True \
    -v "$PWD:/probe:ro" -v "$WORK/deployment:/bundle:ro" \
    -v /nix/store:/nix/store:ro \
    --entrypoint /app/.venv/bin/python "$IMAGE" \
    /probe/scripts/gateway/probe_pinned_litellm_responses.py \
    /probe/deploy/litellm-go/model-group-forwarding.yaml --packaged-hook /bundle \
    --production-launcher /probe/guest/opencode/launch.py \
    --opencode "$RUNTIME" --postgres-bin "$POSTGRES_BIN" \
    --delivery-mode "$mode" || break
done
```

Inspect each command's exit/classifications, not the loop's final status alone.
Container rootfs is disposable/writable for Prisma, mounts read-only, networking
loopback-only, pulls forbidden. All provider and database state is synthetic.

`nix --quiet develop --offline --no-update-lock-file -c just check-all` also
passed **8 groups, 0 failed, 0 errors**, including byte-equality and strict
completion unit regressions. Workload realization passed; no new guest boot
or pilot deployment is claimed by this validation.
