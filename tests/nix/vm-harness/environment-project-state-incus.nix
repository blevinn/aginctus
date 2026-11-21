# Opt-in heavyweight REAL Incus project-owned volume/service instance test.
# Uses an isolated NixOS VM/Incus daemon; never connects to the host daemon.
# The guest state CLI and Unix service are deliberately same-UID test fixtures,
# NOT an authenticated project promotion or reconciliation service.
{ self, nixpkgs, system }:
let
  pkgs = import nixpkgs { inherit system; };
  project = "accepted-environments";
  serverID = "disposable-incus-test-server";
  stateRoot = "/var/lib/aginctus-project-state/environments";
  socket = "/run/aginctus-project-state/accepted.sock";
  stateFixture = self.packages.${system}.state-project-fixture;
  envA = "11111111-1111-4111-8111-111111111111";
  envB = "22222222-2222-4222-8222-222222222222";
  candidate = id: name:
    pkgs.writeText "environment-${name}.json" (builtins.toJSON {
      apiVersion = "aginctus.dev/environment/v1alpha1";
      kind = "EnvironmentCandidate";
      environment = {
        inherit id name;
        binding = { serverId = serverID; inherit project; };
      };
      selectedWorkloads = [ ];
      workloads = [ ];
    });
  candidateA = candidate envA "development";
  candidateB = candidate envB "testing";
  guestImage = nixpkgs.lib.nixosSystem {
    inherit system;
    modules = [
      "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
      {
        system.stateVersion = "26.05";
        networking.useDHCP = false;
        users.groups.aginctusproject = { };
        users.users.aginctusproject = {
          isSystemUser = true;
          group = "aginctusproject";
        };
        environment.systemPackages = [
          pkgs.curl
          pkgs.jq
          pkgs.util-linux
          pkgs.coreutils
          stateFixture
        ];
        # No WantedBy: operator starts the service only after the external
        # volume was attached, inspected and initialized as that UID.
        systemd.services.aginctus-project-state-readonly = {
          description = "Disposable read-only environment state fixture";
          serviceConfig = {
            Type = "simple";
            User = "aginctusproject";
            RuntimeDirectory = "aginctus-project-state";
            RuntimeDirectoryMode = "0700";
            ExecStart = "${stateFixture}/bin/aginctus-project-state-fixture serve --state-dir ${stateRoot} --server-id ${serverID} --project ${project} --socket ${socket}";
            Restart = "on-failure";
          };
        };
      }
    ];
  };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-environment-project-state-incus";
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
    environment.systemPackages = [ pkgs.incus pkgs.jq pkgs.coreutils ];
  };

  testScript = ''
    import json

    start_all()
    machine.wait_for_unit("incus.service")
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus project create ${project} --config features.images=true --config features.profiles=true --config features.storage.volumes=true")
    machine.succeed("incus project create foreign-environment --config features.storage.volumes=true")
    machine.succeed("incus image import ${guestImage.config.system.build.metadata}/tarball/* ${guestImage.config.system.build.tarball}/tarball/* --alias environment-state-fixture --project ${project}", timeout=180)

    # Explicit administrator-owned bootstrap, not environment acceptance:
    # create durable storage independently of any accepted revision or
    # state service instance. No storage pool/network daemon edits by guest.
    machine.succeed("incus storage volume create default environment-accepted-data --project ${project}")
    machine.fail("incus storage volume show default environment-accepted-data --project foreign-environment")
    machine.succeed("incus init environment-state-fixture environment-state-service --project ${project} --storage default", timeout=180)
    machine.succeed("incus config device add environment-state-service accepted-data disk pool=default source=environment-accepted-data path=/var/lib/aginctus-project-state --project ${project}")
    machine.succeed("incus start environment-state-service --project ${project}", timeout=180)

    guest = "timeout 20 incus exec environment-state-service --project ${project} --disable-stdin --"
    owner = guest + " /run/current-system/sw/bin/runuser -u aginctusproject --"
    fixture = "${stateFixture}/bin/aginctus-project-state-fixture"
    base = " --state-dir ${stateRoot} --server-id ${serverID} --project ${project}"
    machine.wait_until_succeeds(guest + " /run/current-system/sw/bin/id aginctusproject", timeout=120)
    machine.succeed(guest + " /run/current-system/sw/bin/chown aginctusproject:aginctusproject /var/lib/aginctus-project-state")
    machine.succeed(owner + " " + fixture + " init" + base)

    machine.succeed("incus file push ${candidateA} environment-state-service/tmp/candidate-a.json --project ${project}")
    machine.succeed("incus file push ${candidateB} environment-state-service/tmp/candidate-b.json --project ${project}")

    def accepted_status(env):
        raw = machine.succeed(owner + " " + fixture + " status" + base + " --environment " + env)
        return json.loads(raw)

    first_empty = accepted_status("${envA}")
    assert not first_empty["hasAccepted"], "fresh service inferred an accepted revision"

    def accept(env, file):
        return json.loads(machine.succeed(
            owner + " " + fixture + " accept" + base +
            " --environment " + env +
            " --candidate " + file + " --expected-revision none --confirm"
        ))

    a = accept("${envA}", "/tmp/candidate-a.json")
    b = accept("${envB}", "/tmp/candidate-b.json")
    assert a["revision"]["generation"] == 1
    assert b["revision"]["generation"] == 1
    assert a["revision"]["id"] != b["revision"]["id"], "environments collapsed into one active history"
    machine.fail(
        owner + " " + fixture + " accept" + base +
        " --environment ${envA} --candidate /tmp/candidate-a.json --expected-revision none --confirm"
    )
    assert accepted_status("${envA}")["revision"]["id"] == a["revision"]["id"]
    assert accepted_status("${envB}")["revision"]["id"] == b["revision"]["id"]

    def record_digests():
        return machine.succeed(
            owner + " /run/current-system/sw/bin/sha256sum" +
            " ${stateRoot}/environments/${envA}/active.json" +
            " ${stateRoot}/environments/${envA}/revisions/" + a["revision"]["id"] + ".json" +
            " ${stateRoot}/environments/${envB}/active.json" +
            " ${stateRoot}/environments/${envB}/revisions/" + b["revision"]["id"] + ".json"
        )

    before = record_digests()
    machine.succeed(guest + " /run/current-system/sw/bin/systemctl start aginctus-project-state-readonly.service")

    def read_socket(env):
        machine.wait_until_succeeds(
            owner + " /run/current-system/sw/bin/curl --silent --show-error --fail --max-time 5" +
            " --unix-socket ${socket} 'http://localhost/v1/health'", timeout=60
        )
        raw = machine.succeed(
            owner + " /run/current-system/sw/bin/curl --silent --show-error --fail --max-time 5" +
            " --unix-socket ${socket} 'http://localhost/v1/accepted?environment=" + env + "'"
        )
        return json.loads(raw)

    assert read_socket("${envA}")["revision"]["id"] == a["revision"]["id"]
    assert read_socket("${envB}")["revision"]["id"] == b["revision"]["id"]
    denial = machine.succeed(
        guest + " /run/current-system/sw/bin/curl --silent --show-error --max-time 5" +
        " --output /dev/null --write-out '%{http_code}' --unix-socket ${socket}" +
        " 'http://localhost/v1/accepted?environment=${envA}'"
    )
    assert denial.strip() == "403", "cross-UID peer was permitted"
    post = machine.succeed(
        owner + " /run/current-system/sw/bin/curl --silent --show-error --max-time 5" +
        " --request POST --output /dev/null --write-out '%{http_code}'" +
        " --unix-socket ${socket} 'http://localhost/v1/accepted?environment=${envA}'"
    )
    assert post.strip() == "405", "read-only service accepted POST"
    machine.succeed(guest + " /run/current-system/sw/bin/systemctl restart aginctus-project-state-readonly.service")
    assert read_socket("${envA}")["revision"]["id"] == a["revision"]["id"]

    # Delete only the guest INSTANCE. The custom project volume remains
    # external and is reattached after instance recreation.
    machine.succeed("incus stop environment-state-service --project ${project} --force", timeout=180)
    machine.succeed("incus delete environment-state-service --project ${project}", timeout=180)
    machine.succeed("incus storage volume show default environment-accepted-data --project ${project}")
    machine.succeed("incus init environment-state-fixture environment-state-service --project ${project} --storage default", timeout=180)
    machine.succeed("incus config device add environment-state-service accepted-data disk pool=default source=environment-accepted-data path=/var/lib/aginctus-project-state --project ${project}")
    machine.succeed("incus start environment-state-service --project ${project}", timeout=180)
    machine.wait_until_succeeds(guest + " /run/current-system/sw/bin/id aginctusproject", timeout=120)
    machine.succeed(guest + " /run/current-system/sw/bin/test ! -e /tmp/candidate-a.json")
    assert accepted_status("${envA}")["revision"]["id"] == a["revision"]["id"]
    assert accepted_status("${envB}")["revision"]["id"] == b["revision"]["id"]
    assert record_digests() == before, "accepted revision records changed on service recreation"
    machine.succeed(guest + " /run/current-system/sw/bin/systemctl start aginctus-project-state-readonly.service")
    assert read_socket("${envA}")["revision"]["id"] == a["revision"]["id"]
    machine.succeed(guest + " /run/current-system/sw/bin/test ! -e /var/lib/incus/unix.socket")
    machine.succeed("incus storage volume show default environment-accepted-data --project ${project}")
    machine.succeed("incus list --project default --format json | jq -e 'length == 0'")
    print("ENVIRONMENT PROJECT STATE: two independent CAS histories survived service restart and instance recreation with project-owned external volume")
  '';
}
