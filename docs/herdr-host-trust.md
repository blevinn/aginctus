# Herdr SSH host trust and strict connectivity probe (#120)

This increments the explicit #120 bootstrap after `aginctus herdr authorize dev`.

```sh
aginctus herdr pin-host dev
```

It verifies running Incus ownership for the configured client and OpenCode workload; retrieves the workload Ed25519 **public** host key from `/etc/ssh/ssh_host_ed25519_key.pub` using authenticated `incus exec` (never from unauthenticated `ssh-keyscan`); discovers the workload's IPv4 address from its guest management interface; and asks `aginctus-ssh-host-trust` in the client to pin the key in `/var/lib/aginctus/ssh/trust/known_hosts`.

The guest trust helper is delivered with the **updated Herdr client NixOS image**. It creates an owned, private directory and 0600 trust file, refuses malformed/duplicate existing alias entries, and fails closed if the existing key differs. Do not delete an unexpected pin to make reconciliation succeed; an explicit recovery/incarnation workflow is still needed.

After verifying the persisted pin, Aginctus selects the existing managed SSH identity generation (without reading or copying the private key) and runs an SSH probe **inside the Herdr client** to the workload's `agent` account using:
- an explicit key file, `IdentitiesOnly=yes`, `IdentityAgent=none`, and `BatchMode=yes`;
- `StrictHostKeyChecking=yes`, `HostKeyAlias=<managed instance>`, `UpdateHostKeys=no`, and `HostKeyAlgorithms=ssh-ed25519`;
- `UserKnownHostsFile=/var/lib/aginctus/ssh/trust/known_hosts` and `GlobalKnownHostsFile=/dev/null`;
- a five-second TCP connection timeout and `id -un`, which must return `agent`.

The command exits unsuccessfully if the SSH probe is blocked or rejected, even though the verified pin remains installed. Running it again will reuse the existing matching pin. It does not change the host's firewall or publish a Herdr machine.

## Validation (host)

```sh
git fetch origin
git switch feat/120-herdr-pin-host-trust
git pull --ff-only

nix develop -c go test ./internal/cli -run 'Test(ParseWorkloadHostKey|ParseManagementAddress|HerdrPinHost)' -count=1
nix develop -c go test ./...
nix flake check

# IMPORTANT: The Herdr client image must be rebuilt and applied from this
# branch so that aginctus-ssh-host-trust exists inside the client.
incus exec aginctus-herdr --project default -- command -v aginctus-ssh-host-trust

# Only after confirming the helper is present:
nix develop -c go run ./cmd/aginctus herdr authorize dev
nix develop -c go run ./cmd/aginctus herdr pin-host dev
echo "Pin/probe exit status: $?"

incus exec aginctus-herdr --project default -- \
  aginctus-ssh-host-trust inspect aginctus-dev
```

Do not recreate the Herdr client merely to update the image unless its persistent SSH identity has been preserved. This bootstrap still requires lifecycle-safe image refresh, and the current host firewall may continue blocking SSH.

## Remaining boundaries

This is an **explicit operator action**, not an automated Apply driver or a Herdr machine publication. It currently assumes the workload's management device is `eth0`, the SSH account is `agent`, and the canonical alias is the configured managed instance name. Host-key replacement requires manual, explicit recovery. Target incarnation tracking, automatic host trust regeneration after workload recreation, safe upgrades of existing client containers, and guaranteed immunity to concurrent guest recreation remain incomplete. Herdr CLI integration must prove that its remote-session subprocess honors the dedicated SSH trust settings before registration is claimed.

The host firewalld bridge-netfilter issue is separate. At the last test, authenticated SSH succeeded with `net.bridge.bridge-nf-call-iptables=0`, and the original value `1` was restored. Prefer a narrowly scoped host firewall exception; do not disable bridge filtering as part of this feature.
