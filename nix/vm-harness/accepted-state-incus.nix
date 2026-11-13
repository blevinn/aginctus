# Opt-in, heavyweight disposable Incus state-service feasibility test.
# It never connects to a host/pilot Incus daemon. The guest runs only a
# same-UID, read-only service; acceptance is an explicit local CLI operation.
{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  stateDir = "/var/lib/aginctus-accepted/local";
  # Socket lifetime is the service lifetime, not the accepted records' lifetime.
  # A forced instance stop must not persist an unowned stale listener path.
  socket = "/run/aginctus-state-readonly/status.sock";
  stateLocal = self.packages.${system}.state-local;
  stateService = self.packages.${system}.state-service;
  guestImage = nixpkgs.lib.nixosSystem {
    inherit system;
    modules = [
      "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
      {
        system.stateVersion = "26.05";
        networking.useDHCP = false;
        users.groups.aginctusstate = { };
        users.users.aginctusstate = {
          isSystemUser = true;
          group = "aginctusstate";
        };
        environment.systemPackages = [
          pkgs.curl
          pkgs.jq
          pkgs.util-linux
          pkgs.coreutils
          stateLocal
          stateService
        ];
        # Deliberately no WantedBy: state initialization and service start
        # require explicit test actions after the volume has been attached.
        systemd.services.aginctus-state-readonly = {
          description = "Experimental read-only same-UID accepted-state service";
          serviceConfig = {
            Type = "simple";
            User = "aginctusstate";
            RuntimeDirectory = "aginctus-state-readonly";
            RuntimeDirectoryMode = "0700";
            ExecStart = "${stateService}/bin/aginctus-state --state-dir ${stateDir} --socket ${socket}";
            Restart = "on-failure";
          };
        };
      }
    ];
  };
  candidate = pkgs.writeText "accepted-state-empty-selection.json" (
    builtins.toJSON {
      apiVersion = "aginctus.dev/accepted/v1alpha1";
      kind = "WorkloadDeployment";
      selection = {
        apiVersion = "aginctus.dev/accepted/v1alpha1";
        kind = "SelectionManifest";
        deploymentID = "probe";
        incusProject = "accepted-state-probe";
        selectedWorkloads = [ ];
      };
      workloads = [ ];
    }
  );
in
pkgs.testers.runNixOSTest {
  name = "aginctus-accepted-state-incus-persistence";
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
      pkgs.incus
      pkgs.jq
      pkgs.coreutils
    ];
  };

  testScript = ''
    import json
    start_all()
    machine.wait_for_unit("incus.service")
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus project create accepted-state-probe --config features.images=true --config features.profiles=true --config features.storage.volumes=true")
    machine.succeed("incus image import ${guestImage.config.system.build.metadata}/tarball/* ${guestImage.config.system.build.tarball}/tarball/* --alias state-fixture --project accepted-state-probe", timeout=180)
    machine.succeed("incus storage volume create default accepted-data --project accepted-state-probe")
    machine.succeed("incus init state-fixture accepted-service --project accepted-state-probe --storage default", timeout=180)
    machine.succeed("incus config device add accepted-service accepted-data disk pool=default source=accepted-data path=/var/lib/aginctus-accepted --project accepted-state-probe")
    machine.succeed("incus start accepted-service --project accepted-state-probe", timeout=180)

    guest = "timeout 20 incus exec accepted-service --project accepted-state-probe --disable-stdin --"
    runuser = guest + " /run/current-system/sw/bin/runuser -u aginctusstate --"
    machine.wait_until_succeeds(guest + " /run/current-system/sw/bin/id aginctusstate", timeout=120)
    machine.succeed(guest + " /run/current-system/sw/bin/chown aginctusstate:aginctusstate /var/lib/aginctus-accepted")
    machine.succeed(runuser + " ${stateLocal}/bin/aginctus-state-local init --state-dir ${stateDir}")
    machine.succeed("incus file push ${candidate} accepted-service/tmp/pinned.json --project accepted-state-probe")
    machine.succeed(
      runuser + " ${stateLocal}/bin/aginctus-state-local accept" +
      " --state-dir ${stateDir} --deployment probe --project accepted-state-probe" +
      " --candidate /tmp/pinned.json --expected-revision none --confirm"
    )

    def accepted_status():
        output = machine.succeed(
          runuser + " ${stateLocal}/bin/aginctus-state-local status" +
          " --state-dir ${stateDir} --deployment probe --project accepted-state-probe"
        )
        report = json.loads(output)
        assert report["hasAccepted"] and report["revision"]["generation"] == 1, "accepted revision missing"
        return report["revision"]["id"]

    first_revision = accepted_status()
    def record_hashes():
        return machine.succeed(
          runuser + " /run/current-system/sw/bin/sha256sum ${stateDir}/active.json" +
          " ${stateDir}/revisions/" + first_revision + ".json"
        )

    first_records = record_hashes()
    machine.succeed(guest + " /run/current-system/sw/bin/systemctl start aginctus-state-readonly.service")

    def read_socket():
        # Type=simple start/restart returning does not imply HTTP readiness.
        machine.wait_until_succeeds(
          runuser + " /run/current-system/sw/bin/curl --silent --show-error --fail --max-time 5" +
          " --unix-socket ${socket} http://localhost/v1/health", timeout=60
        )
        report = machine.succeed(
          runuser + " /run/current-system/sw/bin/curl --silent --show-error --fail --max-time 5" +
          " --unix-socket ${socket} 'http://localhost/v1/active?deployment=probe'"
        )
        parsed = json.loads(report)
        assert parsed["revision"]["id"] == first_revision
        assert parsed["spec"]["selection"]["deploymentID"] == "probe"

    read_socket()
    # A different process UID must not read the socket even within the guest.
    denial = machine.succeed(
      guest + " /run/current-system/sw/bin/curl --silent --show-error --max-time 5 --output /dev/null --write-out '%{http_code}'" +
      " --unix-socket ${socket} 'http://localhost/v1/active?deployment=probe'"
    )
    assert denial.strip() == "403", "cross-UID probe did not reach the authorization boundary"
    write_denial = machine.succeed(
      runuser + " /run/current-system/sw/bin/curl --silent --show-error --max-time 5 --request POST --output /dev/null --write-out '%{http_code}'" +
      " --unix-socket ${socket} 'http://localhost/v1/active?deployment=probe'"
    )
    assert write_denial.strip() == "405", "read-only service accepted a write method"
    machine.succeed(guest + " /run/current-system/sw/bin/systemctl restart aginctus-state-readonly.service")
    read_socket()

    # Destroy the disposable service INSTANCE, not the external owned volume.
    # A recreated instance attaches the same protected storage and observes
    # the same accepted revision without any client-local state or auto-import.
    machine.succeed("incus stop accepted-service --project accepted-state-probe --force", timeout=180)
    machine.succeed("incus delete accepted-service --project accepted-state-probe", timeout=180)
    machine.succeed("incus storage volume show default accepted-data --project accepted-state-probe")
    machine.succeed("incus init state-fixture accepted-service --project accepted-state-probe --storage default", timeout=180)
    machine.succeed("incus config device add accepted-service accepted-data disk pool=default source=accepted-data path=/var/lib/aginctus-accepted --project accepted-state-probe")
    machine.succeed("incus start accepted-service --project accepted-state-probe", timeout=180)
    machine.wait_until_succeeds(guest + " /run/current-system/sw/bin/id aginctusstate", timeout=120)
    machine.succeed(guest + " /run/current-system/sw/bin/test ! -e /tmp/pinned.json")
    assert accepted_status() == first_revision, "accepted revision lost on service instance recreation"
    assert record_hashes() == first_records, "persistent revision record bytes changed"
    machine.succeed(guest + " /run/current-system/sw/bin/systemctl start aginctus-state-readonly.service")
    read_socket()

    # No host Incus administration socket or gateway credentials are passed
    # into the state-service guest. An explicit host operator must own setup.
    machine.succeed(guest + " /run/current-system/sw/bin/test ! -e /var/lib/incus/unix.socket")
    machine.succeed("incus storage volume show default accepted-data --project accepted-state-probe")
    machine.succeed("incus list --project default --format json | jq -e 'length == 0'")
    print("ACCEPTED STATE INCUS: external volume survived service instance recreation, read-only same-UID Unix inspection and explicit CAS persisted")
  '';
}
