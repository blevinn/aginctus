# Herdr workload public-key authorization (#120)

The first host-side SSH bootstrap operation is available as:

```sh
aginctus herdr authorize dev
```

This command is **not** a full remote-connectivity or machine-registration command. It deliberately stops after establishing verified public-key authorization for the configured, running OpenCode container. It does not change guest firewall rules, replace a client identity, pin SSH host trust, or register a Herdr machine.

## Preconditions and safety

- The Herdr client and workload must be running and have matching Aginctus ownership metadata in the default Incus project.
- The client identity must already have been created with `aginctus-ssh-client-identity ensure`; this operation only uses `inspect` and never generates/rotates a private key.
- Only the client public Ed25519 key goes through Aginctus and the workload's `aginctus-ssh-authorize reconcile agent` helper.
- If the workload already contains a different managed key, the operation fails without replacing it. An existing matching key is idempotent.
- After reconciling a missing key, the command reads the workload helper output back and requires an exact match.
- Incus ownership is checked before any guest helper invocation. No shell command interpolation is used; fixed helper argument lists are passed to Incus.
- Initial implementation uses the existing host CLI's spawned `incus exec` boundary, like current lifecycle and console commands. The [credential-lifecycle design](ssh-credentials.md) calls for moving this into the Incus Go API/adapter and adding incarnation-aware mutation protection before automatic orchestration. Until then, treat this as an **explicit operator-requested step**, not an automatic Apply driver.

## Validation

```sh
nix develop -c go test ./internal/cli -run 'Test(ParseHerdrPublicIdentity|HerdrAuthorize)' -count=1
nix develop -c go test ./...
nix flake check

# On a host with running, owned Herdr client and OpenCode workload:
nix develop -c go run ./cmd/aginctus herdr authorize dev
echo "Authorization exit status: $?"

incus exec aginctus-dev --project default -- aginctus-ssh-authorize inspect agent
echo "Inspection exit status: $?"

# Re-run: should report verified without changing the installed key.
nix develop -c go run ./cmd/aginctus herdr authorize dev
echo "Repeat exit status: $?"
```

These blocks intentionally do not enable `set -e` or `set -u`, so a failing diagnostic does not terminate an existing interactive shell.

## Remaining gates

The SSH host key must be collected from the workload through authenticated Incus, associated with the workload's stable incarnation, and published into dedicated pinned trust and SSH configuration inside the Herdr client. Probe authenticated SSH using strict host-key checking and only then register the machine through the pinned Herdr interface. Verify end-to-end Herdr remote attach and execution, and integrate the lifecycle with declarative reconciliation after guest operation and incarnation safety are established.

The host's firewalld/bridge-netfilter configuration is an independent infrastructure prerequisite. In the 2026-10-11 validation, authenticated client-to-workload SSH succeeded when IPv4 bridge netfilter was temporarily disabled, but the host setting was restored to `1`. Do not disable that protection as part of this command.
