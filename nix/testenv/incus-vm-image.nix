# Incus owns the OUTER test VM. This is a separate image from the
# NixOS test-driver QEMU batch guest, and cannot access pilot credentials.
{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  guest = nixpkgs.lib.nixosSystem {
    inherit system;
    modules = [
      "${nixpkgs}/nixos/modules/virtualisation/incus-virtual-machine.nix"
      (import ../vm-harness/aginctus-host.nix { inherit self system; })
      (
        { lib, pkgs, ... }:
        {
          system.stateVersion = "26.05";
          # The Incus VM image module supplies the Incus agent loader, EFI
          # boot and qcow2 output. Explicitly retain the native guest agent.
          virtualisation.incus.agent.enable = true;
          # The shared host module supplies the inner Incus daemon,
          # Aginctus NixOS integration and pinned workload image identity.

          # No guest network configuration, DHCP, SSH, credentials or
          # writable host mounts. Outer instance policy also has NO NIC.
          networking.useDHCP = false;
          networking.networkmanager.enable = lib.mkForce false;
          systemd.network.enable = lib.mkForce false;
          services.openssh.enable = lib.mkForce false;

          environment.systemPackages = [
            pkgs.git
            pkgs.util-linux
          ];
        }
      )
    ];
  };
  disk = guest.config.system.build.qemuImage;
  # The VM image is intentionally NOT an unconditional flake check: creating
  # a full NixOS disk is a large, explicitly requested operator build.
  image =
    pkgs.runCommand "aginctus-testenv-incus-vm-image"
      {
        nativeBuildInputs = [
          pkgs.gnutar
          pkgs.xz
        ];
      }
      ''
            set -euo pipefail
            shopt -s nullglob
            disks=(${disk}/*.qcow2)
            if [ "''${#disks[@]}" -ne 1 ]; then
              echo "expected precisely one pinned NixOS qcow2 disk" >&2
              exit 1
            fi
            mkdir -p "$out"
            cp -- "''${disks[0]}" "$out/disk.qcow2"
            chmod 0444 "$out/disk.qcow2"
            mkdir -p meta
            cat > meta/metadata.yaml <<'YAML'
        architecture: x86_64
        creation_date: 0
        properties:
          os: NixOS
          description: Aginctus isolated disposable test VM
          type: virtual-machine
        YAML
            tar --sort=name --mtime='@1' --owner=0 --group=0 --numeric-owner \
              -C meta -cJf "$out/metadata.tar.xz" metadata.yaml
            chmod 0444 "$out/metadata.tar.xz"
      '';
in
{
  inherit guest image;
}
