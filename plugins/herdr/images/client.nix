{
  nixpkgs,
  system,
  pkgs,
  herdrPackage,
  herdrClientCommand,
  sshClientIdentityHelper,
  sshHostTrustHelper,
  sshMachineConfigHelper,
  guestStateBindings,
}:
let
  guestModule = import ./client-guest.nix {
    inherit
      herdrPackage
      herdrClientCommand
      sshClientIdentityHelper
      sshHostTrustHelper
      sshMachineConfigHelper
      guestStateBindings
      ;
  };
  guest = nixpkgs.lib.nixosSystem {
    inherit system;
    modules = [
      "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
      guestModule
    ];
  };
in
{
  inherit guest guestModule;
  image = pkgs.runCommand "aginctus-herdr-client-image" { } ''
    mkdir -p "$out"
    ln -s ${guest.config.system.build.squashfs} "$out/rootfs"
    ln -s ${guest.config.system.build.metadata} "$out/metadata"
  '';
}
