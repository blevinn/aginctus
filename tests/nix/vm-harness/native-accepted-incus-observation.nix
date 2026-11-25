# Focused read-only observation with a same-UID pin and a real restricted TLS session.
{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  tests = pkgs.buildGoModule {
    pname = "aginctus-native-accepted-incus-observation-tests";
    version = "0";
    src = self;
    vendorHash = "sha256-0k7OkHckPVEZEbFkQ1ojBbIEc7Lj0nvfWsYWOy18AdI=";
    doCheck = false;
    buildPhase = ''
      runHook preBuild
      go test -c -o accepted-observation-tests ./internal/orchestration/native
      runHook postBuild
    '';
    installPhase = ''
      mkdir -p "$out/bin"
      cp accepted-observation-tests "$out/bin/"
    '';
  };
  # Same offline NixOS fixture image as native-instance; nothing is started.
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
  name = "aginctus-native-accepted-incus-observation";
  nodes.machine = { ... }: {
    system.stateVersion = "26.05";
    networking.nftables.enable = true;
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
      pkgs.jq
      pkgs.openssl
      pkgs.util-linux
    ];
    users.users.operator = {
      uid = 1000;
      isNormalUser = true;
      # No incus or incus-admin group: the observer only gets loopback TLS.
      extraGroups = [ ];
    };
  };
  testScript = ''
    import json
    import shlex

    start_all()
    machine.wait_for_unit("incus.service", timeout=60)
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus version")
    for project in ["team", "unrelated"]:
        machine.succeed(f"incus project create {project} -c restricted=true -c features.images=true")
    machine.succeed("incus image import ${image.config.system.build.metadata}/tarball/* ${image.config.system.build.tarball}/tarball/* --alias fixture --project team", timeout=120)
    fingerprint = machine.succeed("incus image list --project team --format json | jq -er '.[0].fingerprint'").strip()

    def seed(name, workload, owner):
        machine.succeed(f"incus init fixture {name} --project team --storage default"
                        " -c user.aginctus.managed=true -c user.aginctus.resource=workload"
                        " -c user.aginctus.role=agent-workload"
                        f" -c user.aginctus.workload={workload} -c user.aginctus.environment-id={owner}", timeout=90)

    owner = "7c82e6d1-1212-4123-8123-0123456789ab"
    peer = "7c82e6d1-1212-4123-8123-0123456789ac"
    seed("worker-1", "worker", owner)
    seed("drift-1", "drift", owner)
    seed("sibling-1", "sibling", owner)
    seed("foreign-1", "worker", peer)
    # The missing name really exists elsewhere; root provides the existence
    # control only. The observer never gets an administrator connection.
    machine.succeed("incus init missing-1 --empty --project unrelated --storage default")

    operator = "runuser -u operator -- env HOME=/home/operator USER=operator LOGNAME=operator"
    machine.succeed("incus config set core.https_address 127.0.0.1:8443")
    machine.succeed(operator + " incus remote generate-certificate")
    machine.succeed("test $(stat -c '%a' /home/operator/.config/incus/client.key) = 600")
    machine.succeed("incus config trust add-certificate /home/operator/.config/incus/client.crt --restricted --projects team")
    machine.fail(operator + " incus --force-local project list")
    machine.succeed("install -m 0644 /var/lib/incus/server.crt /run/aginctus-observation-server.crt")
    pin = machine.succeed("openssl x509 -in /run/aginctus-observation-server.crt -outform DER | sha256sum").split()[0]
    machine.succeed("test ! -e /run/aginctus-accepted-observation-disposable && touch /run/aginctus-accepted-observation-disposable")

    def snapshot(project):
        return sorted(json.loads(machine.succeed(f"incus list --project {project} --format json")), key=lambda i: i["name"])

    before = {project: snapshot(project) for project in ["team", "unrelated"]}
    assert [i["name"] for i in before["team"]] == ["drift-1", "foreign-1", "sibling-1", "worker-1"]
    assert [i["name"] for i in before["unrelated"]] == ["missing-1"]
    assert all(i["status"] == "Stopped" for instances in before.values() for i in instances)
    output = machine.succeed(
        operator + " AGINCTUS_ACCEPTED_OBSERVATION_VM=disposable-vm"
        + " AGINCTUS_OBSERVATION_SERVER_PIN=" + shlex.quote(pin)
        + " AGINCTUS_OBSERVATION_IMAGE=" + shlex.quote(fingerprint)
        + " accepted-observation-tests -test.v -test.run '^TestObservePinnedAcceptedWorkloadsVM$' -test.timeout=90s",
        timeout=100,
    )
    print(output)
    assert "--- PASS: TestObservePinnedAcceptedWorkloadsVM " in output, "guest Go test did not run"
    for project, instances in before.items():
        assert snapshot(project) == instances, f"observation changed {project} instances"
    print("INCUS OBSERVATION: both projects unchanged, including unselected and foreign fixtures")
  '';
}
