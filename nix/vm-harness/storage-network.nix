{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-vm-harness-storage-network";

  nodes.machine =
    { lib, ... }:
    {
      imports = [ (import ./guest.nix { inherit self system; }) ];
      boot.supportedFilesystems = [ "btrfs" ];
      # This scenario tests network and storage without importing runtime images.
      services.aginctus.images.herdrClient.enable = lib.mkForce false;
    };

  testScript = ''
    start_all()
    machine.wait_for_unit("incus.service")
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus info")

    # A Btrfs loop pool is created wholly within the disposable guest disk.
    machine.succeed("incus storage create vm-harness-btrfs btrfs size=2GiB")
    machine.succeed("incus storage volume create vm-harness-btrfs state")
    machine.succeed("incus storage volume show vm-harness-btrfs state")
    machine.succeed("incus storage volume snapshot vm-harness-btrfs state checkpoint")
    machine.succeed("systemctl restart incus.service")
    machine.succeed("incus storage volume show vm-harness-btrfs state")
    machine.succeed("incus storage volume snapshot list vm-harness-btrfs state")

    # Exercise the actual Aginctus network path, not a hand-built test bridge.
    cli = "aginctus --configuration-file /etc/aginctus/config.json"
    machine.succeed(f"{cli} network ensure")
    machine.succeed(f"{cli} network ensure")  # idempotent
    machine.succeed("incus network show aginctus-mgmt")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.nat").strip() == "false"
    assert machine.succeed("incus network get aginctus-mgmt ipv4.routing").strip() == "false"
    assert machine.succeed("incus network get aginctus-mgmt ipv6.address").strip() == "none"

    # Removing an Aginctus-owned bridge must not remove an unrelated network.
    machine.succeed("incus network create vm-harness-foreign ipv4.address=none ipv6.address=none")
    machine.succeed(f"{cli} network teardown")
    machine.fail("incus network show aginctus-mgmt")
    machine.succeed("incus network show vm-harness-foreign")
    # A colliding but unowned resource must be left untouched.
    machine.succeed("incus network create aginctus-mgmt ipv4.address=none ipv6.address=none")
    machine.fail(f"{cli} network ensure")
    machine.succeed("incus network show aginctus-mgmt")
  '';
}
