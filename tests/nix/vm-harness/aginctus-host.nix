# Shared, declarative Aginctus-under-test host configuration.
# Used in BOTH the NixOS test-driver guest and the Incus VM image.
# Outer worker policy and batch-VM sizing are intentionally separate.
{ self, system }:
{
  config,
  lib,
  pkgs,
  ...
}:
{
  imports = [ self.nixosModules.default ];
  system.stateVersion = "26.05";
  virtualisation.incus.enable = true;
  networking.nftables.enable = true;

  environment.systemPackages = [
    pkgs.incus
    pkgs.jq
    pkgs.iproute2
    pkgs.btrfs-progs
    self.packages.${system}.aginctus
  ];

  services.aginctus = {
    enable = true;
    package = self.packages.${system}.aginctus;
    # Synthetic tests explicitly configure/verify their own network/firewall.
    # Do not activate pilot-targeted reconciliation at image boot.
    firewall.enable = false;
    reconcile.enable = false;
    images.herdrClient = {
      enable = true;
      source = self.packages.${system}.herdr-client;
    };
    images.opencodeWorkload.enable = false;
  };

  # Incus must be initialized by the disposable fixture before importing
  # container images. Never make the image import race uninitialized Incus.
  systemd.services =
    lib.optionalAttrs config.services.aginctus.images.herdrClient.enable {
      aginctus-image-herdr-client.wantedBy = lib.mkForce [ ];
    }
    // lib.optionalAttrs config.services.aginctus.images.opencodeWorkload.enable {
      aginctus-image-opencode-workload.wantedBy = lib.mkForce [ ];
    };
}
