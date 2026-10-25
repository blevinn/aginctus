# QEMU/NixOS test-driver wrapper around the shared Aginctus host.
{ self, system }:
{ ... }:
{
  imports = [ (import ./aginctus-host.nix { inherit self system; }) ];

  virtualisation = {
    cores = 2;
    memorySize = 4096;
    diskSize = 16384;
  };
}
