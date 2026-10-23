{ self, nixpkgs, system }:
let
  pkgs = import nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-vm-harness-smoke";

  nodes.machine = import ./guest.nix { inherit self system; };

  testScript = ''
    start_all()
    machine.wait_for_unit("incus.service")
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus info")
    machine.succeed("test -r /etc/aginctus/config.json")
    machine.succeed("systemctl reset-failed aginctus-image-herdr-client.service || true")
    machine.succeed("systemctl restart aginctus-image-herdr-client.service")
    machine.succeed("systemctl is-active --quiet aginctus-image-herdr-client.service")
    machine.succeed("incus image info aginctus-herdr-client")
    machine.succeed("incus image alias list --format json | jq -e '[.[] | select(.name == \"aginctus-herdr-client\")] | length == 1'")
    # Services use the store-pinned executable directly, but the test shell
    # must also resolve the same packaged Aginctus CLI from PATH.
    machine.succeed("command -v aginctus")
    machine.succeed("aginctus --configuration-file /etc/aginctus/config.json network ensure --dry-run")
  '';
}
