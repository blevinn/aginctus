# Managed Herdr machine registration (v0.1 / issue #120)

The current host-independent Aginctus CLI supports:

```sh
aginctus herdr authorize dev
aginctus herdr pin-host dev
aginctus herdr register dev
```

The last command is **not** a bypass for authorization or host trust. It
validates owned, running Incus client and workload instances, repeats the
strict SSH preflight, configures a persistent SSH alias inside the Herdr client,
verifies the alias actually authenticates as `agent`, and only then runs the
supported `herdr machine add` command. Private keys never leave the Herdr
client.

## SSH alias contract

For managed workload instance `aginctus-dev`, the client holds an immutable,
root-owned `/var/lib/aginctus/ssh/machines/aginctus-dev.conf`, included in
its global OpenSSH config by the NixOS **guest** image. The file sets:

- Host `aginctus-dev`, HostName from authenticated Incus management-`eth0` discovery, User `agent`;
- IdentityFile under the persistent current identity generation;
- IdentitiesOnly, no SSH agent, BatchMode, StrictHostKeyChecking;
- explicit `HostKeyAlias`, known_hosts under persistent system-state, Ed25519
  host-key algorithm, and no global known_hosts or automatic key updates.

The dedicated `aginctus-ssh-machine-config` helper refuses symbolic links,
different existing content, and foreign-owned state. A changed management IP or
rotated credential requires **explicit recovery**, not silent reassignment of
trust. This initial implementation supports the existing NixOS guest image,
not arbitrary guest distributions. The host can be any Linux supporting local
Incus API access.

The Herdr profile label and SSH target are both the managed instance name,
and its remote session is `default`. The CLI reads `herdr machine list
--json`; an absent profile is added using `herdr machine add
aginctus-dev --label aginctus-dev --remote-session default`. An existing
matching profile is retained without repeating a potentially intrusive Herdr
remote setup. A mismatched existing label fails closed.

Herdr's machine-add process can start the remote server or require explicit
user approval to update an incompatible installation. The CLI does **not**
auto-approve binary replacement. Compatible, pre-provisioned Herdr servers
are the supported v0.1 path.

## Operational validation

Run these non-mutating validations from the current checkout before attempting explicit machine registration:

```sh
nix develop -c go test ./internal/cli -run 'Test(ParseSavedHerdrMachines|SavedHerdrMachine|HerdrRegister)' -count=1
nix develop -c go test ./...
nix flake check
nix build .#herdr-client --no-link
```

On the already provisioned, owned client with a verified persistent SSH identity, confirm that authorization and host-key trust are valid. Do not replace an existing instance as part of a routine registration check: an image upgrade needs a separate, reviewed, state-preserving procedure. Then:

```sh
nix develop -c go run ./cmd/aginctus herdr pin-host dev
nix develop -c go run ./cmd/aginctus herdr register dev
incus exec aginctus-herdr --project default -- herdr machine list --json
incus exec aginctus-herdr --project default -- herdr machine status aginctus-dev --json
```

Do not change host bridge filtering or firewall defaults as a machine registration workaround.

## Limitations

The registration operation does not implement remote sessions other
than `default`, automatic SSH alias drift remediation, or user-controlled
host aliases. It does not register machines during a generic `aginctus apply`;
`herdr register` is an explicit, independently verifiable action for v0.1.
Automatic reconciliation and enriched machine health reporting are follow-ups.
