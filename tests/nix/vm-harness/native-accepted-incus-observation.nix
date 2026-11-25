# Focused read-only observation with a same-UID pin and a real restricted TLS session.
{
  self,
  nixpkgs,
  system,
  joinedLifecycle ? false,
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
      ${pkgs.lib.optionalString joinedLifecycle ''
        go test -c -o joined-lifecycle-tests ./internal/nativelifecycle
      ''}
      runHook postBuild
    '';
    installPhase = ''
      mkdir -p "$out/bin"
      cp accepted-observation-tests "$out/bin/"
      ${pkgs.lib.optionalString joinedLifecycle ''
        cp joined-lifecycle-tests "$out/bin/"
      ''}
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
  name =
    if joinedLifecycle then
      "aginctus-native-joined-lifecycle"
    else
      "aginctus-native-accepted-incus-observation";
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
    print(machine.succeed("incus version"))
    for project in ["team", "unrelated"]:
        machine.succeed(f"incus project create {project} -c restricted=true -c features.images=true")
    ${pkgs.lib.optionalString joinedLifecycle ''
      machine.succeed("incus network create fixture-mgmt --project default ipv4.address=10.79.37.1/24"
                      " ipv4.dhcp.gateway=none ipv4.nat=false ipv4.routing=false ipv6.address=none"
                      " user.aginctus.managed=true user.aginctus.resource=management-network")
      machine.succeed("incus project set team features.storage.volumes=true features.profiles=true features.networks=false"
                      " user.aginctus.managed=true user.aginctus.resource=deployment-project user.aginctus.deployment=team"
                      " restricted.devices.nic=managed restricted.devices.disk=managed"
                      " restricted.networks.access=fixture-mgmt restricted.storage-pools.access=default")
    ''}
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
    machine.succeed("incus config set core.https_address=127.0.0.1:8443")
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
  ''
  + pkgs.lib.optionalString joinedLifecycle ''
    machine.succeed("incus network list --project default --format=json | jq -e '.[] | select(.name == \"fixture-mgmt\")' >/run/aginctus-joined-bridge.json")
    machine.succeed("touch /run/aginctus-joined-lifecycle-disposable")
    output = machine.succeed(
        operator + " AGINCTUS_JOINED_LIFECYCLE_VM=disposable-vm"
        + " AGINCTUS_OBSERVATION_SERVER_PIN=" + shlex.quote(pin)
        + " AGINCTUS_OBSERVATION_IMAGE=" + shlex.quote(fingerprint)
        + " joined-lifecycle-tests -test.v -test.run '^TestLocalNativeJoinedWorkloadHerdrVM$' -test.timeout=200s",
        timeout=210,
    )
    print(output)
    assert "--- PASS: TestLocalNativeJoinedWorkloadHerdrVM " in output, "joined Go test did not run"
    assert snapshot("team") == before["team"]
    assert snapshot("unrelated") == before["unrelated"]
    machine.succeed("incus storage volume show default joined-herdr-state-system --project team")
    machine.succeed("incus network list --project default --format=json | jq -e '.[] | select(.name == \"fixture-mgmt\") | .config' > /run/aginctus-joined-bridge-config-after.json"
                    " && jq .config /run/aginctus-joined-bridge.json | diff - /run/aginctus-joined-bridge-config-after.json")
    print("JOINED LIFECYCLE: restricted SDK create/start/stop/removal and retained Herdr guest data verified; host bridge policy and unselected instances unchanged")
  '';
}
