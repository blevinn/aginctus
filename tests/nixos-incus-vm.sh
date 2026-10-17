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
      firewall.enable = false; # Native nftables Incus VM; #223 is iptables-only.
      package = flake.packages.${system}.aginctus;
      images.herdrClient.enable = true;
      images.herdrClient.source = flake.packages.${system}.herdr-client;
      images.opencodeWorkload.enable = false;
      reconcile.enable = false;
    };
  };

  nodes.reconcile = { pkgs, ... }: {
    imports = [ flake.nixosModules.default ];
    system.stateVersion = "26.05";
    # Exercise service ordering without running an additional Incus daemon.
    systemd.services.incus = {
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        ExecStart = "${pkgs.coreutils}/bin/true";
      };
    };
    services.aginctus = {
      enable = true;
      firewall.enable = false; # Existing image lifecycle test does not use firewall reconciliation.
      package = pkgs.writeShellScriptBin "aginctus" ''
        if [ -e /run/aginctus-test-fail-image ]; then
          case "$*" in
            *"images update"*) exit 1 ;;
          esac
        fi
        echo "$*" >> /var/log/aginctus-test-commands
      '';
      images.herdrClient = {
        enable = true;
        source = pkgs.runCommand "aginctus-test-image" { } "mkdir -p $out";
      };
      images.opencodeWorkload.enable = false;
      reconcile.enable = true;
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

    # Reuse already-imported image bytes under a second alias without
    # attempting a duplicate Incus image import.
    machine.succeed("${flake.packages.${system}.aginctus}/bin/aginctus --configuration-file /etc/aginctus/config.json images update aginctus-herdr-client-reused ${flake.packages.${system}.herdr-client}")
    reused = machine.succeed("incus image list --format json | jq -r '.[] | select(any(.aliases[]?; .name == \"aginctus-herdr-client-reused\")) | .fingerprint'").strip()
    assert reused == before, f"reused alias fingerprint differs: {before} -> {reused}"
    machine.succeed("incus image alias list --format json | jq -e '[.[] | select(.name == \"aginctus-herdr-client-reused\")] | length == 1'")


    # Promote a genuinely different valid image into the active alias, then
    # restore the original. This exercises staging, backup, and promotion.
    machine.succeed("${flake.packages.${system}.aginctus}/bin/aginctus --configuration-file /etc/aginctus/config.json images update aginctus-herdr-client ${flake.packages.${system}.opencode-workload}")
    replacement = machine.succeed("incus image list --format json | jq -r '.[] | select(any(.aliases[]?; .name == \"aginctus-herdr-client\")) | .fingerprint'").strip()
    assert len(replacement) >= 12 and replacement != before, f"image replacement did not change fingerprint: {before} -> {replacement}"
    machine.succeed("incus image alias list --format json | jq -e '[.[] | select(.name == \"aginctus-herdr-client\")] | length == 1'")
    machine.succeed("${flake.packages.${system}.aginctus}/bin/aginctus --configuration-file /etc/aginctus/config.json images update aginctus-herdr-client ${flake.packages.${system}.herdr-client}")
    restored = machine.succeed("incus image list --format json | jq -r '.[] | select(any(.aliases[]?; .name == \"aginctus-herdr-client\")) | .fingerprint'").strip()
    assert restored == before, f"restoring original image failed: {before} -> {restored}"
    machine.succeed("incus image alias list --format json | jq -e '[.[] | select(.name | startswith(\"aginctus-herdr-client-previous-\"))] | length == 0'")
    machine.succeed("incus image alias list --format json | jq -e '[.[] | select(.name | startswith(\"aginctus-herdr-client-staged-\"))] | length == 0'")

    # Test the systemd dependency graph with a recording CLI in the second VM.
    reconcile.wait_for_unit("aginctus-reconcile.service")
    reconcile.succeed("systemctl is-active --quiet aginctus-image-herdr-client.service")
    commands = reconcile.succeed("cat /var/log/aginctus-test-commands").splitlines()
    assert len(commands) == 3, f"unexpected commands: {commands!r}"
    assert "images update" in commands[0], f"image import did not run first: {commands!r}"
    assert "network ensure" in commands[1], f"missing network reconciliation: {commands!r}"
    assert "herdr client ensure" in commands[2], f"missing Herdr reconciliation: {commands!r}"

    # An actual failed image-import dependency must block reconciliation.
    reconcile.succeed("systemctl stop aginctus-reconcile.service")
    reconcile.succeed("systemctl stop aginctus-image-herdr-client.service")
    reconcile.succeed("touch /run/aginctus-test-fail-image")
    reconcile.fail("systemctl start aginctus-reconcile.service")
    commands_after = reconcile.succeed("cat /var/log/aginctus-test-commands").splitlines()
    assert commands_after == commands, f"reconciliation ran despite failed dependency: {commands_after!r}"

    # A malformed replacement must fail without disturbing the active alias.
    # This exercises an Incus import error after artifact path validation,
    # rather than a trivial CLI argument or missing-file error.
    machine.succeed("mkdir -p /tmp/aginctus-invalid-image/metadata /tmp/aginctus-invalid-image/rootfs")
    machine.succeed("printf 'invalid metadata' > /tmp/aginctus-invalid-image/metadata/broken.tar.xz")
    machine.succeed("printf 'invalid rootfs' > /tmp/aginctus-invalid-image/rootfs/broken.squashfs")
    machine.fail("${flake.packages.${system}.aginctus}/bin/aginctus --configuration-file /etc/aginctus/config.json images update aginctus-herdr-client /tmp/aginctus-invalid-image")
    still_active = machine.succeed("incus image list --format json | jq -r '.[] | select(any(.aliases[]?; .name == \"aginctus-herdr-client\")) | .fingerprint'").strip()
    assert still_active == before, f"failed update changed active alias: {before} -> {still_active}"
    machine.succeed("incus image alias list --format json | jq -e '[.[] | select(.name == \"aginctus-herdr-client\")] | length == 1'")
    machine.succeed("incus image alias list --format json | jq -e '[.[] | select(.name | startswith(\"aginctus-herdr-client-staged-\"))] | length == 0'")

  '';
}
NIX

# Firewalld uses the native nftables backend and can exercise real Incus
# ownership validation with the packaged Aginctus CLI in a disposable VM.
echo 'Building and executing the disposable NixOS Incus/firewalld integration check...'
nix build '.#checks.x86_64-linux.firewall-firewalld-integration' --no-link --print-build-logs --show-trace "$@"
echo 'PASS: Incus/firewalld integration check completed.'

# Exercise the native NixOS nftables backend with a real, disposable Incus VM.
echo 'Building and executing the disposable NixOS native-nftables/Incus check...'
nix build '.#checks.x86_64-linux.firewall-nixos-nft-integration' --no-link --print-build-logs --show-trace "$@"
echo 'PASS: native NixOS nftables/Incus integration check completed.'

# Run the iptables hook/packet-filtering integration in an independent
# disposable NixOS VM before the existing native-nftables Incus lifecycle test.
# The iptables test intentionally uses a fixture; NixOS Incus requires nftables.
echo 'Building and executing the disposable NixOS iptables firewall check...'
nix build '.#checks.x86_64-linux.firewall-iptables-integration' --no-link --print-build-logs --show-trace "$@"
echo 'PASS: iptables firewall integration check completed.'

export AGINCTUS_VM_REPO="$repo"
echo 'Building and executing the disposable NixOS QEMU/Incus integration test...'
# Realizing the NixOS test derivation starts QEMU and executes testScript.
nix build --impure --file "$workdir/test.nix" --print-build-logs --show-trace --out-link "$workdir/result" "$@"
[[ -e "$workdir/result" ]] || { echo 'VM test did not produce a result' >&2; exit 1; }
echo 'PASS: real Incus image lifecycle and systemd reconciliation dependency tests passed in disposable VMs.'
