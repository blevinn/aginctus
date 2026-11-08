{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  controllerFixture = pkgs.writeText "native-instance-controller-denial.service" ''
    [Service]
    Type=oneshot
    ExecStart=${pkgs.coreutils}/bin/true
  '';
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
      self.packages.${system}.default
    ];
    users.users.operator = {
      isNormalUser = true;
      uid = 1000;
      extraGroups = [ "incus-admin" ];
    };
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

    # Exercise the packaged CLI with real system/current-user controller evidence.
    machine.succeed("loginctl enable-linger operator")
    machine.wait_for_unit("user@1000.service")
    operator = "runuser -u operator -- env XDG_RUNTIME_DIR=/run/user/1000"
    cli = operator + " aginctus --config=incus.project=native-instance-vm --config=incus.management.network.name=native-mgmt --config=workloads.selected.instance.name=selected --config=workloads.selected.runtime.type=opencode --config=workloads.selected.isolation=container --config=workloads.selected.image.alias=fixture --config=workloads.selected.storage.pool=default"
    before = machine.succeed("incus config show selected --project native-instance-vm")
    import json
    preview = json.loads(machine.succeed(cli + " workload apply --project native-instance-vm selected --dry-run"))
    assert preview["action"] == "would-update" and not preview["writeAuthorized"]
    assert machine.succeed("incus config show selected --project native-instance-vm") == before
    machine.succeed(cli + " workload apply --project native-instance-vm selected --confirm-apply")
    machine.wait_until_succeeds("timeout 15 incus exec --disable-stdin selected --project native-instance-vm -- /run/current-system/sw/bin/systemctl is-system-running", timeout=90)
    machine.succeed(cli + " workload apply --project native-instance-vm selected --confirm-apply")
    preview = json.loads(machine.succeed(cli + " workload apply --project native-instance-vm selected --dry-run"))
    assert preview["action"] == "unchanged" and preview["changeCount"] == 0
    machine.succeed(cli + " workload stop --project native-instance-vm selected --confirm-stop")
    machine.succeed(cli + " workload stop --project native-instance-vm selected --confirm-stop")

    # A loaded but inactive controller is sufficient to deny manual apply.
    machine.succeed("cp ${controllerFixture} /run/systemd/system/aginctus-reconcile.service && systemctl daemon-reload")
    before = machine.succeed("incus config show selected --project native-instance-vm")
    machine.fail(cli + " workload apply --project native-instance-vm selected --confirm-apply")
    assert machine.succeed("incus config show selected --project native-instance-vm") == before
    machine.succeed("rm /run/systemd/system/aginctus-reconcile.service && systemctl daemon-reload")
    machine.fail(cli + " --config=workloads.selected.instance.name=absent workload apply --project native-instance-vm selected --confirm-apply")
    machine.succeed("incus list --project native-instance-vm --format json | ${pkgs.jq}/bin/jq -e 'length == 1 and .[0].name == \"selected\" and .[0].status == \"Stopped\"'")
    machine.succeed("test \"$(cat /srv/workspace/sentinel)\" = workspace-sentinel")
    machine.succeed("incus storage volume show default protected --project native-instance-vm")
    machine.succeed("incus list --project default --format json | ${pkgs.jq}/bin/jq -e 'length == 0'")
  '';
}
