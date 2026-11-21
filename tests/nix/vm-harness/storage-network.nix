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

    # The native Incus reconciler refuses the same colliding resource.
    native_cli = f"{cli} --config=incus.management.network.backend=native"
    machine.fail(f"{native_cli} network ensure")
    machine.succeed("incus network show aginctus-mgmt")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.address").strip() == "none"

    # The native reconciler creates and converges the bridge without the
    # incus-apply driver.
    machine.succeed("incus network delete aginctus-mgmt")
    machine.succeed(f"{native_cli} network ensure")
    machine.succeed(f"{native_cli} network ensure")  # idempotent
    assert machine.succeed("incus network get aginctus-mgmt user.aginctus.managed").strip() == "true"
    assert machine.succeed("incus network get aginctus-mgmt user.aginctus.resource").strip() == "management-network"
    assert machine.succeed("incus network get aginctus-mgmt ipv4.nat").strip() == "false"
    assert machine.succeed("incus network get aginctus-mgmt ipv4.routing").strip() == "false"
    assert machine.succeed("incus network get aginctus-mgmt ipv4.dhcp.gateway").strip() == "none"
    assert machine.succeed("incus network get aginctus-mgmt ipv6.address").strip() == "none"

    # Unmanaged Incus configuration survives a native reconcile.
    machine.succeed("incus network set aginctus-mgmt user.vendor-note=preserved")
    machine.succeed(f"{native_cli} network ensure")
    assert machine.succeed("incus network get aginctus-mgmt user.vendor-note").strip() == "preserved"

    # The operator-supplied network description survives a native reconcile.
    machine.succeed('incus network set aginctus-mgmt --property description="operator supplied"')
    machine.succeed("incus network set aginctus-mgmt ipv4.nat=true")
    machine.succeed(f"{native_cli} network ensure")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.nat").strip() == "false"
    assert "operator supplied" in machine.succeed("incus network show aginctus-mgmt")

    # A dry run plans the change without mutating it.
    machine.succeed("incus network set aginctus-mgmt ipv4.nat=true")
    machine.succeed(f"{native_cli} network ensure --dry-run")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.nat").strip() == "true"
    machine.succeed(f"{native_cli} network ensure")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.nat").strip() == "false"

    # The DHCP announcement is immutable policy. Not even an explicit
    # adoption authorization rewrites it.
    bridge_address = machine.succeed("incus network get aginctus-mgmt ipv4.address").strip().split("/")[0]
    machine.succeed(f"incus network set aginctus-mgmt ipv4.dhcp.gateway={bridge_address}")
    machine.fail(f"{native_cli} network ensure")
    machine.fail(f"{native_cli} network ensure --force")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.dhcp.gateway").strip() == bridge_address
    machine.succeed("incus network set aginctus-mgmt ipv4.dhcp.gateway=none")
    machine.succeed(f"{native_cli} network ensure")

    # A native removal deletes only the owned bridge.
    machine.succeed(f"{native_cli} network teardown")
    machine.fail("incus network show aginctus-mgmt")
    machine.succeed("incus network show vm-foreign")

    # A removal never targets a network of another type, even with the
    # Aginctus ownership markers and an explicit adoption authorization.
    machine.succeed("incus network create aginctus-mgmt --type=macvlan parent=eth0")
    machine.succeed("incus network set aginctus-mgmt user.aginctus.managed=true")
    machine.succeed("incus network set aginctus-mgmt user.aginctus.resource=management-network")
    machine.fail(f"{native_cli} network teardown")
    machine.fail(f"{native_cli} network teardown --force")
    machine.succeed("incus network show aginctus-mgmt")
    machine.succeed("incus network delete aginctus-mgmt")

    # Only an explicit adoption may take over a foreign bridge.
    machine.succeed("incus network create aginctus-mgmt ipv4.address=auto ipv6.address=none ipv4.dhcp.gateway=none")
    machine.fail(f"{native_cli} network ensure")
    machine.succeed("incus network show aginctus-mgmt")
    machine.succeed(f"{native_cli} network ensure --force")
    assert machine.succeed("incus network get aginctus-mgmt user.aginctus.managed").strip() == "true"
    assert machine.succeed("incus network get aginctus-mgmt user.aginctus.resource").strip() == "management-network"
    assert machine.succeed("incus network get aginctus-mgmt ipv4.dhcp.gateway").strip() == "none"
    assert machine.succeed("incus network get aginctus-mgmt ipv4.nat").strip() == "false"
    machine.succeed(f"{native_cli} network teardown")
    machine.fail("incus network show aginctus-mgmt")
    machine.succeed("incus network show vm-foreign")

    # Adoption authorization covers the ownership markers only. A foreign
    # bridge that announces a DHCP gateway is never adopted.
    machine.succeed("incus network create aginctus-mgmt ipv4.address=auto ipv6.address=none")
    foreign_address = machine.succeed("incus network get aginctus-mgmt ipv4.address").strip().split("/")[0]
    machine.succeed(f"incus network set aginctus-mgmt ipv4.dhcp.gateway={foreign_address}")
    machine.fail(f"{native_cli} network ensure --force")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.dhcp.gateway").strip() == foreign_address
    machine.succeed("incus network show aginctus-mgmt")
    machine.succeed("incus network delete aginctus-mgmt")
  '';
}
