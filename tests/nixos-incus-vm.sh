#!/usr/bin/env bash
# Disposable NixOS QEMU test: real Incus, packaged Aginctus CLI, Herdr image.
# Never connects to the host's Incus daemon.
set -euo pipefail

command -v nix >/dev/null || { echo 'nix is required' >&2; exit 1; }
[[ -f flake.nix && -f nix/nixos-module.nix ]] || {
  echo 'Run from the Aginctus repository root' >&2
  exit 1
}
[[ $(uname -m) == x86_64 ]] || {
  echo 'VM test currently supports x86_64-linux only' >&2
  exit 1
}

repo=$(pwd -P)
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

cat > "$workdir/test.nix" <<'NIX'
let
  flake = builtins.getFlake (builtins.getEnv "AGINCTUS_VM_REPO");
  system = "x86_64-linux";
  pkgs = import flake.inputs.nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-real-incus-image-lifecycle";
  nodes.machine = { pkgs, ... }: {
    imports = [ flake.nixosModules.default ];
    system.stateVersion = "26.05";
    virtualisation = {
      memorySize = 4096;
      diskSize = 16384;
      cores = 2;
    };
    networking.nftables.enable = true;
    virtualisation.incus.enable = true;
    environment.systemPackages = [ pkgs.incus pkgs.jq ];
    services.aginctus = {
      enable = true;
      package = flake.packages.${system}.aginctus;
      images.herdrClient.enable = true;
      images.herdrClient.source = flake.packages.${system}.herdr-client;
      images.opencodeWorkload.enable = false;
      reconcile.enable = false;
    };
  };

  testScript = ''
    start_all()
    machine.wait_for_unit("incus.service")
    # The image unit can race with daemon initialization at boot.
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus info")
    machine.succeed("systemctl reset-failed aginctus-image-herdr-client.service || true")
    machine.succeed("systemctl restart aginctus-image-herdr-client.service")
    machine.succeed("systemctl is-active --quiet aginctus-image-herdr-client.service")
    machine.succeed("incus image info aginctus-herdr-client")
    before = machine.succeed("incus image list --format json | jq -r '.[] | select(any(.aliases[]?; .name == \"aginctus-herdr-client\")) | .fingerprint'").strip()
    assert len(before) >= 12, f"no image fingerprint after import: {before!r}"

    machine.succeed("systemctl restart aginctus-image-herdr-client.service")
    machine.succeed("incus image info aginctus-herdr-client")
    after = machine.succeed("incus image list --format json | jq -r '.[] | select(any(.aliases[]?; .name == \"aginctus-herdr-client\")) | .fingerprint'").strip()
    assert before == after, f"unexpected fingerprint change: {before} -> {after}"
    machine.succeed("incus image alias list --format json | jq -e '[.[] | select(.name == \"aginctus-herdr-client\")] | length == 1'")
    machine.succeed("test -f /etc/aginctus/config.json")

    # A malformed replacement must fail without disturbing the active alias.
    # This exercises an Incus import error after artifact path validation,
    # rather than a trivial CLI argument or missing-file error.
    machine.succeed("mkdir -p /tmp/aginctus-invalid-image/metadata /tmp/aginctus-invalid-image/rootfs")
    machine.succeed("printf 'invalid metadata' > /tmp/aginctus-invalid-image/metadata/broken.tar.xz")
    machine.succeed("printf 'invalid rootfs' > /tmp/aginctus-invalid-image/rootfs/broken.squashfs")
    machine.fail("${flake.packages.${system}.aginctus}/bin/aginctus --configuration-file /etc/aginctus/config.json images update aginctus-herdr-client /tmp/aginctus-invalid-image")
    still_active = machine.succeed("incus image list --format json | jq -r '.[] | select(any(.aliases[]?; .name == \\"aginctus-herdr-client\\")) | .fingerprint'").strip()
    assert still_active == before, f"failed update changed active alias: {before} -> {still_active}"
    machine.succeed("incus image alias list --format json | jq -e '[.[] | select(.name == \\"aginctus-herdr-client\\")] | length == 1'")
    machine.succeed("incus image alias list --format json | jq -e '[.[] | select(.name | startswith(\\"aginctus-herdr-client-staged-\\"))] | length == 0'")

  '';
}
NIX

export AGINCTUS_VM_REPO="$repo"
echo 'Building and executing the disposable NixOS QEMU/Incus integration test...'
# Realizing the NixOS test derivation starts QEMU and executes testScript.
nix build --impure --file "$workdir/test.nix" --print-build-logs --show-trace --out-link "$workdir/result" "$@"
[[ -e "$workdir/result" ]] || { echo 'VM test did not produce a result' >&2; exit 1; }
echo 'PASS: Incus image import, idempotent repeat update, and failed replacement preservation passed in disposable VM.'
