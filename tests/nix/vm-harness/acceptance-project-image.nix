# Gate C: public CLI project creation and scoped image import against guest Incus.
# This test is a fixed Nix derivation, never a host Incus or pilot mutation.
{ self, nixpkgs, system }:
let
  pkgs = import nixpkgs { inherit system; };
  image = self.packages.${system}.vm-harness-opencode-workload;
  leaseHolder = pkgs.writeText "aginctus-acceptance-image-lease.py" ''
    import hashlib
    import pathlib
    import socket
    import time
    name = "\0aginctus-image-write-v1-" + hashlib.sha256(b"aginctus").hexdigest()
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.bind(name)
    pathlib.Path("/run/aginctus-acceptance-lease-ready").touch()
    time.sleep(90)
  '';
in
pkgs.testers.runNixOSTest {
  name = "aginctus-acceptance-project-image";
  nodes.machine = { ... }: {
    virtualisation = {
      memorySize = 4096;
      diskSize = 16384;
      cores = 2;
      incus.enable = true;
      incus.package = pkgs.incus;
    };
    environment.systemPackages = [ pkgs.incus pkgs.jq pkgs.python3 self.packages.${system}.aginctus ];
    system.stateVersion = "26.05";
  };
  testScript = ''
    import json

    start_all()
    machine.wait_for_unit("incus.service")
    machine.succeed("incus admin init --auto --storage-backend dir")

    # No ambient Incus project mutation; ensure is explicit, create-only.
    assert machine.succeed("incus project show default")
    machine.fail("aginctus operator project-ensure --project default --confirm-create")
    machine.fail("aginctus operator project-ensure --project aginctus")
    machine.succeed("aginctus operator project-ensure --project aginctus --confirm-create")
    machine.succeed("aginctus operator project-ensure --project aginctus --confirm-create")
    plan = json.loads(machine.succeed("aginctus operator project-plan --project aginctus"))
    assert plan["status"] == "compatible", plan
    project = json.loads(machine.succeed("incus --force-local project list --format json"))
    owned = [p for p in project if p["name"] == "aginctus"]
    assert len(owned) == 1, project
    assert owned[0]["config"]["user.aginctus.resource"] == "deployment-project"
    assert owned[0]["config"]["features.networks"] == "false"
    assert machine.succeed("incus project list --format csv -c n").count("aginctus") == 1
    assert machine.succeed("incus project get default features.images").strip() != "false"

    # A foreign same-name project must not be adopted or modified.
    machine.succeed("incus project create foreign")
    foreign_before = machine.succeed("incus project show foreign")
    machine.fail("aginctus operator project-ensure --project foreign --confirm-create")
    assert machine.succeed("incus project show foreign") == foreign_before
    machine.fail("aginctus operator project-plan --project foreign")

    # Import the pinned image artifacts with a self-declared ownership claim.
    # The claim is not caller authentication; exact-project checks still apply.
    cli = "aginctus --config=incus.project=aginctus"
    # Hold the actual Linux abstract socket from another guest process.
    # The CLI must refuse without reading or changing the project inventory.
    images_before_lease = json.loads(machine.succeed("incus --force-local --project aginctus image list --format json"))
    machine.succeed("systemd-run --unit=aginctus-acceptance-image-lease --collect ${pkgs.python3}/bin/python3 ${leaseHolder}")
    machine.wait_until_succeeds("test -e /run/aginctus-acceptance-lease-ready")
    code, output = machine.execute(f"{cli} images import-owned --project aginctus --owner system accept-image {image} --confirm-create 2>&1")
    assert code == 1 and "writer lease unavailable" in output, output
    assert json.loads(machine.succeed("incus --force-local --project aginctus image list --format json")) == images_before_lease
    machine.succeed("systemctl stop aginctus-acceptance-image-lease.service")

    machine.fail(f"{cli} images import-owned --project aginctus --owner system accept-image {image}")
    machine.succeed(f"{cli} images import-owned --project aginctus --owner system accept-image {image} --confirm-create", timeout=240)
    inventory = json.loads(machine.succeed("incus --force-local --project aginctus image list --format json"))
    matches = [i for i in inventory if any(a["name"] == "accept-image" for a in i.get("aliases", []))]
    assert len(matches) == 1, inventory
    fingerprint = matches[0]["fingerprint"]
    assert len(fingerprint) == 64
    # No implicit adoption/overwrite even with the same source and alias.
    machine.fail(f"{cli} images import-owned --project aginctus --owner system accept-image {image} --confirm-create")
    assert json.loads(machine.succeed("incus --force-local --project aginctus image list --format json")) == inventory
    machine.fail(f"{cli} images import-owned --project foreign --owner system reject-image {image} --confirm-create")
    machine.fail(f"{cli} images import-owned --project default --owner system reject-image {image} --confirm-create")
    assert json.loads(machine.succeed("incus --force-local --project aginctus image list --format json")) == inventory
    assert machine.succeed("incus project get foreign user.aginctus.managed").strip() == ""
    assert machine.succeed("incus project get default user.aginctus.managed").strip() == ""
  '';
}
