{
  self,
  nixpkgs,
  system,
}:
let
  # This derives an interactive/retained VM script from the SAME pinned
  # guest module as the disposable batch tests. The supervisor will launch
  # the result from a trusted store closure, not editable checkout Python.
  configuration = nixpkgs.lib.nixosSystem {
    inherit system;
    modules = [
      (import ./guest.nix { inherit self system; })
      (
        { lib, ... }:
        {
          # Persistent VM transport proof does not need Herdr images at boot;
          # the guest remains real NixOS + Incus, with later scenario setup.
          services.aginctus.images.herdrClient.enable = lib.mkForce false;

          virtualisation.vmVariant = {
            virtualisation = {
              graphics = false;
              cores = lib.mkForce 2;
              memorySize = lib.mkForce 4096;
              # No 9p/virtiofs access to the host Nix store. Build a separate
              # guest-local read-only Nix store image instead.
              mountHostNixStore = lib.mkForce false;
              useNixStoreImage = true;
              sharedDirectories = lib.mkForce { };
              forwardPorts = lib.mkForce [ ];
              qemu = {
                # No SLIRP, TAP, bridge, direct Internet or host ports. The
                # supervisor must also strip inherited QEMU_* overrides.
                networkingOptions = lib.mkForce [ "-nic none" ];
                guestAgent.enable = true;
              };
            };
          };
        }
      )
    ];
  };
in
configuration.config.system.build.vm
