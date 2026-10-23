{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-vm-harness-ownership-recovery";

  nodes.machine =
    { lib, ... }:
    {
      imports = [ (import ./guest.nix { inherit self system; }) ];
      virtualisation.memorySize = lib.mkForce 6144;
      virtualisation.diskSize = lib.mkForce 24576;
      services.aginctus.images.opencodeWorkload = {
        enable = lib.mkForce true;
        source = self.packages.${system}.opencode-workload;
      };
    };

  testScript = ''
    start_all()
    machine.wait_for_unit("incus.service")
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("systemctl reset-failed aginctus-image-herdr-client.service || true")
    machine.succeed("systemctl restart aginctus-image-herdr-client.service")
    machine.succeed("systemctl reset-failed aginctus-image-opencode-workload.service || true")
    machine.succeed("systemctl restart aginctus-image-opencode-workload.service")

    cli = "aginctus --configuration-file /etc/aginctus/config.json"
    machine.succeed(f"{cli} network ensure")

    # A same-named, unowned instance MUST NOT be adopted or removed.
    machine.succeed("incus init aginctus-opencode-workload aginctus-dev --network aginctus-mgmt")
    machine.fail(f"{cli} workload apply dev")
    machine.succeed("incus info aginctus-dev")
    machine.succeed("incus delete aginctus-dev")
    machine.succeed("incus init aginctus-herdr-client aginctus-herdr --network aginctus-mgmt")
    machine.fail(f"{cli} herdr client apply")
    machine.succeed("incus info aginctus-herdr")
    machine.succeed("incus delete aginctus-herdr")

    # The same configuration succeeds when colliding foreign resources are gone.
    machine.succeed(f"{cli} herdr client apply")
    machine.succeed(f"{cli} workload apply dev")
    machine.succeed("incus info aginctus-herdr")
    machine.succeed("incus info aginctus-dev")

    # Stop socket activation too so the CLI cannot restart the guest daemon
    # and turn the intended outage into a successful reconciliation.
    machine.succeed("systemctl stop incus.socket incus.service")
    machine.fail("systemctl is-active --quiet incus.socket")
    machine.fail("systemctl is-active --quiet incus.service")
    machine.fail(f"{cli} workload apply dev", timeout=60)
    machine.fail("systemctl is-active --quiet incus.service")
    machine.succeed("systemctl start incus.socket incus.service")
    machine.wait_for_unit("incus.service")
    machine.succeed(f"{cli} herdr client apply")
    machine.succeed(f"{cli} workload apply dev")
    machine.succeed("incus info aginctus-herdr")
    machine.succeed("incus info aginctus-dev")
  '';
}
