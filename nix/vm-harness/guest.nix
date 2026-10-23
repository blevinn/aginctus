{ self, system }:
{ pkgs, ... }:
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
  environment.systemPackages = with pkgs; [
    incus
    jq
    iproute2
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
}
