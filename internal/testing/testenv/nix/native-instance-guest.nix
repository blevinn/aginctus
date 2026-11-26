# Opt-in direct-guest native-instance scenario image module.
# The fixture is never started automatically. Its commands are identical
# to the existing batch VM assertions, not caller-supplied shell strings.
{
  self,
  nixpkgs,
  system,
}:
let
  fixture = import ../../../../tests/nix/vm-harness/native-instance-fixture.nix {
    inherit self nixpkgs system;
  };
in
{ pkgs, ... }:
let
  script = pkgs.writeText "testenv-native-instance-fixture.py" (
    builtins.readFile ./direct-guest.py + "\n" + fixture.script
  );
in
{
  imports = [ fixture.module ];
  environment.systemPackages = [
    pkgs.bash
    pkgs.jq
    pkgs.util-linux
  ];
  systemd.services.aginctus-testenv-native-instance = {
    description = "Fixed native-instance disposable guest scenario";
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
      TimeoutStartSec = "30min";
      UMask = "0077";
    };
    # All mutations target the guest-local daemon. The admitted VM image
    # and outer worker are the boundary, not this service or DMI alone.
    script = ''
      export PATH=/run/current-system/sw/bin
      systemd-detect-virt --vm --quiet
      test -z "$(incus list --format csv)"
      exec ${pkgs.python3}/bin/python3 -I ${script}
    '';
  };
}
