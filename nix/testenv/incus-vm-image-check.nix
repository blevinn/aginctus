{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  guest = (import ./incus-vm-image.nix { inherit self nixpkgs system; }).guest;
  cfg = guest.config;
in
assert cfg.virtualisation.incus.enable;
assert cfg.networking.nftables.enable;
assert cfg.virtualisation.incus.agent.enable;
assert !cfg.services.openssh.enable;
assert !cfg.systemd.network.enable;
assert !cfg.networking.useDHCP;
assert !cfg.networking.networkmanager.enable;
assert cfg.fileSystems."/".fsType == "ext4";
# This is a pure module evaluation guard. It does not boot a VM, import
# anything into a host Incus daemon, or attest network isolation.
pkgs.runCommand "aginctus-incus-vm-image-policy-check" { } ''
  mkdir -p "$out"
  printf '%s\n' 'image policy evaluation PASS; KVM/Incus import not verified' > "$out/result"
''
