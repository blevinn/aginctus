{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  tests = pkgs.buildGoModule {
    pname = "aginctus-native-instance-tests";
    version = "0";
    src = self;
    vendorHash = "sha256-KUyM9QZ11nS6I1V2Abkdpxp5L6DSKLpmFfNvGBaqMWA=";
    doCheck = false;
    buildPhase = ''
      runHook preBuild
      go test -c -o native-instance-tests ./internal/orchestration/native
      runHook postBuild
    '';
    installPhase = ''
      mkdir -p "$out/bin"
      cp native-instance-tests "$out/bin/"
    '';
  };
  image = nixpkgs.lib.nixosSystem {
    inherit system;
    modules = [
      "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
      {
        system.stateVersion = "26.05";
        networking.useDHCP = false;
      }
    ];
  };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-native-instance";
  nodes.machine = { ... }: {
    virtualisation = {
      memorySize = 4096;
      diskSize = 16384;
      cores = 2;
      incus.enable = true;
      incus.package = pkgs.incus;
    };
    environment.systemPackages = [
      tests
      pkgs.incus
    ];
    networking.nftables.enable = true;
    networking.dhcpcd.allowInterfaces = [ "eth0" ];
    system.stateVersion = "26.05";
  };
  testScript = ''
    start_all()
    machine.wait_for_unit("incus.service")
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("printf 'native-instance-vm\\n' >/run/aginctus-native-instance-disposable")
    command = "AGINCTUS_NATIVE_INSTANCE_VM=disposable-vm native-instance-tests -test.v -test.timeout=8m"
    machine.succeed(command + " -test.run '^TestNativeInstanceVMProvision$'")
    machine.succeed("incus image import ${image.config.system.build.metadata}/tarball/* ${image.config.system.build.tarball}/tarball/* --alias fixture --project native-instance-vm", timeout=180)
    machine.succeed("incus network create native-mgmt ipv4.address=none ipv6.address=none")
    machine.succeed("incus storage volume create default protected --project native-instance-vm")
    machine.succeed("mkdir -p /srv/workspace; printf 'workspace-sentinel' >/srv/workspace/sentinel")
    machine.succeed(command + " -test.run '^TestNativeInstanceVMAcceptance$'", timeout=600)
    machine.succeed("test \"$(cat /srv/workspace/sentinel)\" = workspace-sentinel")
    machine.succeed("incus storage volume show default protected --project native-instance-vm")
    machine.succeed("incus list --project default --format json | ${pkgs.jq}/bin/jq -e 'length == 0'")
  '';
}
