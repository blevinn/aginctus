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
  socket = "${stateDir}/status.sock";
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
    environment.systemPackages = [ pkgs.incus pkgs.jq ];
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

    guest = "incus exec accepted-service --project accepted-state-probe --"
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
    machine.succeed(guest + " /run/current-system/sw/bin/systemctl start aginctus-state-readonly.service")
    machine.wait_until_succeeds(guest + " /run/current-system/sw/bin/systemctl is-active --quiet aginctus-state-readonly.service", timeout=60)

    def read_socket():
        report = machine.succeed(
          runuser + " /run/current-system/sw/bin/curl --silent --show-error --fail" +
          " --unix-socket ${socket} 'http://localhost/v1/active?deployment=probe'"
        )
        parsed = json.loads(report)
        assert parsed["revision"]["id"] == first_revision
        assert parsed["spec"]["selection"]["deploymentID"] == "probe"

    read_socket()
    # A different process UID must not read the socket even within the guest.
    machine.fail(
      guest + " /run/current-system/sw/bin/curl --silent --show-error --fail" +
      " --unix-socket ${socket} 'http://localhost/v1/active?deployment=probe'"
    )
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
    assert accepted_status() == first_revision, "accepted revision lost on service instance recreation"
    machine.succeed(guest + " /run/current-system/sw/bin/systemctl start aginctus-state-readonly.service")
    read_socket()

    # No host Incus administration socket or gateway credentials are passed
    # into the state-service guest. An explicit host operator must own setup.
    machine.fail(guest + " /run/current-system/sw/bin/test -e /var/lib/incus/unix.socket")
    machine.succeed("incus storage volume show default accepted-data --project accepted-state-probe")
    machine.succeed("incus list --project default --format json | jq -e 'length == 0'")
    print("ACCEPTED STATE INCUS: external volume survived service instance recreation, read-only same-UID Unix inspection and explicit CAS persisted")
  '';
}
