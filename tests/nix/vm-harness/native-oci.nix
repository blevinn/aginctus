# Test-only: SDK OCI feasibility and a separate offline stopped-pair gate.
{
  self,
  nixpkgs,
  system,
  stoppedPairOnly ? false,
}:
let
  pkgs = import nixpkgs { inherit system; };
  spike = pkgs.buildGoModule {
    pname = "aginctus-native-oci-spike";
    version = "0";
    src = self;
    vendorHash = "sha256-0k7OkHckPVEZEbFkQ1ojBbIEc7Lj0nvfWsYWOy18AdI=";
    doCheck = false;
    buildPhase = ''
      runHook preBuild
      go test -c -o native-oci-spike ./internal/orchestration/nativeoci
      runHook postBuild
    '';
    installPhase = ''
      mkdir -p "$out/bin"
      cp native-oci-spike "$out/bin/"
    '';
  };
in
pkgs.testers.runNixOSTest {
  name =
    if stoppedPairOnly then "aginctus-native-gateway-stopped-pair" else "aginctus-native-oci-spike";
  nodes.machine = { ... }: {
    virtualisation = {
      memorySize = 8192;
      diskSize = 32768;
      cores = 2;
      incus.enable = true;
      incus.package = pkgs.incus;
    };
    environment.systemPackages = [
      spike
      pkgs.incus
      pkgs.btrfs-progs
    ];
    networking.useDHCP = pkgs.lib.mkIf stoppedPairOnly (pkgs.lib.mkForce false);
    networking.nftables.enable = true;
    networking.firewall.interfaces.spike-mgmt = {
      allowedUDPPorts = [
        53
        67
      ];
      allowedTCPPorts = [ 53 ];
    };
    system.stateVersion = "26.05";
  };
  testScript = ''
    start_all()
    machine.wait_for_unit("incus.service", timeout=60)
  ''
  + pkgs.lib.optionalString (!stoppedPairOnly) ''
    machine.succeed("incus admin init --auto --storage-backend dir")
  ''
  + ''
    machine.succeed("incus storage create spike btrfs size=20GiB", timeout=60)
    machine.succeed("printf 'native-oci-vm\\n' >/run/aginctus-native-oci-disposable")
  ''
  + (
    if stoppedPairOnly then
      ''
        print(machine.succeed("AGINCTUS_NATIVE_OCI_SPIKE=disposable-vm native-oci-spike -test.v -test.run '^TestNativeGatewayStoppedPairVM$' -test.timeout=2m", timeout=130))
      ''
    else
      ''
        with subtest("environment volume and attachment audit"):
            print(machine.succeed("AGINCTUS_NATIVE_OCI_SPIKE=disposable-vm native-oci-spike -test.v -test.run '^TestNativeGatewayAttachmentAuditVM$' -test.timeout=2m", timeout=120))
        with subtest("networked OCI lifecycle"):
            machine.succeed("AGINCTUS_NATIVE_OCI_SPIKE=disposable-vm native-oci-spike -test.v -test.run '^TestDaemonOCI$' -test.timeout=25m", timeout=1500)
      ''
  );
}
