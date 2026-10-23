{ self, system }:
{
  config,
  lib,
  pkgs,
  ...
}:
{
  # Shared guest definition for both scripted VM checks and interactive debugging.
  # All Incus and system services run INSIDE the QEMU guest.
  imports = [ self.nixosModules.default ];
  system.stateVersion = "26.05";

  virtualisation = {
    cores = 2;
    memorySize = 4096;
    diskSize = 16384;
    incus.enable = true;
  };

  networking.nftables.enable = true;
  # The NixOS service calls the store-pinned CLI directly, but the VM test
  # driver executes guest commands through a login shell. Provide the same
  # package on the guest PATH so operator-style CLI checks can run.
  environment.systemPackages = [
    pkgs.incus
    pkgs.jq
    pkgs.iproute2
    self.packages.${system}.aginctus
  ];

  services.aginctus = {
    enable = true;
    package = self.packages.${system}.aginctus;
    firewall.enable = false;
    images.herdrClient = {
      enable = true;
      source = self.packages.${system}.herdr-client;
    };
    images.opencodeWorkload.enable = false;
    # Integration scenarios explicitly reconcile their own resources.
    reconcile.enable = false;
  };

  # Tests initialize Incus before starting image imports; boot-time imports
  # otherwise race initialization and explicit service starts.
  systemd.services =
    lib.optionalAttrs config.services.aginctus.images.herdrClient.enable {
      aginctus-image-herdr-client.wantedBy = lib.mkForce [ ];
    }
    // lib.optionalAttrs config.services.aginctus.images.opencodeWorkload.enable {
      aginctus-image-opencode-workload.wantedBy = lib.mkForce [ ];
    };
}
