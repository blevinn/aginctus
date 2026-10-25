# Real Incus Compose gateway and packaged Herdr/OpenCode deployment.
# This is explicitly heavyweight and may need OCI registry access for
# image caching; it never registers or calls an external model provider.
{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-vm-harness-gateway-topology";
  nodes.machine =
    { lib, ... }:
    {
      imports = [ (import ./guest.nix { inherit self system; }) ];
      virtualisation.memorySize = lib.mkForce 8192;
      virtualisation.diskSize = lib.mkForce 32768;
      # Compose needs OCI network gateway controls absent from the default LTS daemon.
      virtualisation.incus.package = pkgs.incus;
      networking.dhcpcd.allowInterfaces = [ "eth0" ];
      services.aginctus.settings.incus.management.network.ipv4.address = "10.77.0.1/24";
      # Keep same-bridge database traffic at L2; Incus still rejects routed management traffic.
      boot.kernel.sysctl = {
        "net.bridge.bridge-nf-call-iptables" = 0;
        "net.bridge.bridge-nf-call-ip6tables" = 0;
      };
      # OCI guests obtain addresses and bridge-local names from Incus dnsmasq.
      networking.firewall.interfaces = {
        aginctus-mgmt = {
          allowedUDPPorts = [
            53
            67
          ];
          allowedTCPPorts = [ 53 ];
        };
        aginctus-egress = {
          allowedUDPPorts = [
            53
            67
          ];
          allowedTCPPorts = [ 53 ];
        };
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
    # The pinned incus-compose driver uses local HTTPS for image caching.
    # Bind only loopback on the disposable NixOS guest; no public daemon.
    machine.succeed("incus config set core.https_address 127.0.0.1:8443")
    assert machine.succeed("incus config get core.https_address").strip() == "127.0.0.1:8443"

    machine.succeed("systemctl restart aginctus-image-herdr-client.service")
    machine.succeed("systemctl restart aginctus-image-opencode-workload.service")
    machine.succeed("incus image info aginctus-herdr-client")
    machine.succeed("incus image info aginctus-opencode-workload")

    cli = "aginctus --configuration-file /etc/aginctus/config.json"
    # Actually execute the real gateway Apply+Compose engine. Successful
    # rendering/status without application readiness does NOT pass this test.
    machine.succeed(f"{cli} gateway up")
    machine.succeed("incus project show aginctus-gateway")
    machine.succeed("incus network show aginctus-mgmt")
    machine.succeed("incus network show aginctus-egress")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.nat").strip() == "false"
    assert machine.succeed("incus network get aginctus-mgmt ipv4.routing").strip() == "false"
    assert machine.succeed("incus network get aginctus-mgmt ipv6.address").strip() == "none"

    # Real Incus OCI containers must start and respond, not merely exist.
    machine.wait_until_succeeds(
      "timeout 15 incus exec --disable-stdin --project aginctus-gateway postgres-1 -- pg_isready -U litellm -d litellm </dev/null",
      timeout=180,
    )
    machine.wait_until_succeeds(
      "timeout 15 incus exec --disable-stdin --project aginctus-gateway litellm-1 -- python -c "
      "\"import urllib.request; assert urllib.request.urlopen("
      "'http://127.0.0.1:4000/health/liveliness', timeout=5).status == 200\" </dev/null",
      timeout=180,
    )
    machine.succeed(f"{cli} gateway status")

    # The other real Aginctus infrastructure must coexist with the gateway.
    machine.succeed(f"{cli} network ensure")
    machine.succeed(f"{cli} herdr client apply")
    machine.succeed(f"{cli} workload apply dev")
    machine.succeed("incus info aginctus-herdr")
    machine.succeed("incus info aginctus-dev")

    # Verify management-only attachments on non-gateway workloads.
    machine.succeed(
      "incus list --format json | jq -e "
      "'[.[] | select(.name == \"aginctus-herdr\" or .name == \"aginctus-dev\")] | length == 2'"
    )
    # Never register a model here: application health is not synthetic
    # Responses inference, and no real provider should be contacted.
  '';
}
