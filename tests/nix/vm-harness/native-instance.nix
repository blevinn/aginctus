{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  fixture = import ./native-instance-fixture.nix { inherit self nixpkgs system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-native-instance";
  nodes.machine = {
    imports = [ fixture.module ];
    virtualisation = {
      memorySize = 4096;
      diskSize = 16384;
      cores = 2;
    };
    networking.dhcpcd.allowInterfaces = [ "eth0" ];
  };
  testScript = ''
    start_all()
    ${fixture.script}
  '';
}
