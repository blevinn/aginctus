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
    machine.succeed("incus storage volume snapshot create vm-harness-btrfs state checkpoint")
    machine.succeed("systemctl restart incus.service")
    machine.succeed("incus storage volume show vm-harness-btrfs state")
    assert machine.succeed("incus storage volume snapshot list vm-harness-btrfs state --format csv -c n").strip() == "checkpoint"

    # Exercise the actual Aginctus network path, not a hand-built test bridge.
    cli = "aginctus --configuration-file /etc/aginctus/config.json"
    machine.succeed(f"{cli} network ensure")
    machine.succeed(f"{cli} network ensure")  # idempotent
    machine.succeed("incus network show aginctus-mgmt")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.nat").strip() == "false"
    assert machine.succeed("incus network get aginctus-mgmt ipv4.routing").strip() == "false"
    assert machine.succeed("incus network get aginctus-mgmt ipv4.dhcp.gateway").strip() == "none"
    assert machine.succeed("incus network get aginctus-mgmt ipv6.address").strip() == "none"

    # Legacy DHCP policy is immutable drift, not an implicit live migration.
    machine.succeed("incus network unset aginctus-mgmt ipv4.dhcp.gateway")
    machine.fail(f"{cli} network ensure")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.dhcp.gateway").strip() == ""
    legacy_gateway = machine.succeed("incus network get aginctus-mgmt ipv4.address").strip().split("/")[0]
    machine.succeed(f"incus network set aginctus-mgmt ipv4.dhcp.gateway={legacy_gateway}")
    machine.fail(f"{cli} network ensure")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.dhcp.gateway").strip() == legacy_gateway
    machine.succeed("incus network set aginctus-mgmt ipv4.dhcp.gateway=none")
    machine.succeed(f"{cli} network ensure")

    # Removing an Aginctus-owned bridge must not remove an unrelated network.
    machine.succeed("incus network create vm-foreign ipv4.address=none ipv6.address=none")
    machine.succeed(f"{cli} network teardown")
    machine.fail("incus network show aginctus-mgmt")
    machine.succeed("incus network show vm-foreign")
    # A colliding but unowned resource must be left untouched.
    machine.succeed("incus network create aginctus-mgmt ipv4.address=none ipv6.address=none")
    machine.fail(f"{cli} network ensure")
    machine.succeed("incus network show aginctus-mgmt")
  '';
}
