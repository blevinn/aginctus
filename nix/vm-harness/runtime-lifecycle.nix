{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-vm-harness-runtime-lifecycle";

  nodes.machine =
    { lib, ... }:
    {
      imports = [ (import ./guest.nix { inherit self system; }) ];
      # Two real NixOS Incus workloads may run together.
      virtualisation.memorySize = lib.mkForce 6144;
      virtualisation.diskSize = lib.mkForce 24576;
      networking.dhcpcd.allowInterfaces = [ "eth0" ];
      networking.firewall.interfaces.aginctus-mgmt = {
        allowedUDPPorts = [
          53
          67
        ];
        allowedTCPPorts = [ 53 ];
      };
      boot.kernel.sysctl = {
        "net.bridge.bridge-nf-call-iptables" = 0;
        "net.bridge.bridge-nf-call-ip6tables" = 0;
      };
      services.aginctus.images.opencodeWorkload = {
        enable = lib.mkForce true;
        source = self.packages.${system}.opencode-workload;
      };
    };

  testScript = ''
    start_all()
    machine.wait_for_unit("incus.service")
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus info")

    machine.succeed("systemctl reset-failed aginctus-image-herdr-client.service || true")
    machine.succeed("systemctl restart aginctus-image-herdr-client.service")
    machine.succeed("systemctl reset-failed aginctus-image-opencode-workload.service || true")
    machine.succeed("systemctl restart aginctus-image-opencode-workload.service")
    machine.succeed("incus image info aginctus-herdr-client")
    machine.succeed("incus image info aginctus-opencode-workload")

    cli = "aginctus --configuration-file /etc/aginctus/config.json"
    machine.succeed(f"{cli} network ensure")
    machine.succeed(f"{cli} herdr client apply")
    machine.succeed(f"{cli} workload apply dev")
    machine.succeed("incus info aginctus-herdr")
    machine.succeed("incus info aginctus-dev")

    import runpy
    verify_routes = runpy.run_path("${../../scripts/acceptance/opencode-pilot-preflight.py}")["verify_ipv4_routes"]
    for instance in ("aginctus-dev", "aginctus-herdr"):
        machine.wait_until_succeeds(f"timeout 15 incus exec --disable-stdin {instance} -- ip -4 addr show dev eth0 | grep -q 'inet '", timeout=120)
        verify_routes(machine.succeed(f"timeout 15 incus exec --disable-stdin {instance} -- cat /proc/net/route"))
    # DNS and Herdr-to-workload TCP must survive the DHCP policy change.
    machine.wait_until_succeeds("timeout 15 incus exec --disable-stdin aginctus-herdr -- ssh-keyscan -T 5 aginctus-dev.incus >/run/workload-host-key", timeout=120)

    # Repeat the exact same reconciliation against real Incus resources.
    machine.succeed(f"{cli} network ensure")
    machine.succeed(f"{cli} herdr client apply")
    machine.succeed(f"{cli} workload apply dev")
    machine.succeed("incus list --format json | jq -e '[.[] | select(.name == \"aginctus-herdr\")] | length == 1'")
    machine.succeed("incus list --format json | jq -e '[.[] | select(.name == \"aginctus-dev\")] | length == 1'")

    # Gateway rendering uses the *real* packaged CLI, but does not launch
    # OCI services or claim authenticated LiteLLM/provider inference.
    machine.succeed(f"{cli} gateway render >/run/vm-gateway-plan.yaml")
    machine.succeed("test -s /run/vm-gateway-plan.yaml")
  '';
}
