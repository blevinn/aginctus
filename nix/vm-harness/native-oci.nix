# Feasibility only: SDK OCI pulls in a disposable Btrfs-backed daemon.
{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  spike = pkgs.buildGoModule {
    pname = "aginctus-native-oci-spike";
    version = "0";
    src = self;
    vendorHash = "sha256-nOdS1XPJCl/knG3nRUu8m+/AEpG9BmniQ3GT5/RXpP0=";
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
  name = "aginctus-native-oci-spike";
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
    machine.wait_for_unit("incus.service")
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus storage create spike btrfs size=20GiB")
    machine.succeed("printf 'native-oci-vm\\n' >/run/aginctus-native-oci-disposable")
    machine.succeed("AGINCTUS_NATIVE_OCI_SPIKE=disposable-vm native-oci-spike -test.v -test.timeout=25m", timeout=1500)
  '';
}
