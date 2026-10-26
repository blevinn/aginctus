# Real Incus Compose gateway and packaged Herdr/OpenCode deployment.
# This is explicitly heavyweight and may need OCI registry access for
# image caching; it never registers or calls an external model provider.
{
  self,
  nixpkgs,
  system,
  synthetic ? false,
  toolTransport ? false,
}:
let
  pkgs = import nixpkgs { inherit system; };
  lib = nixpkgs.lib;
  # Bundle relative imports in one immutable derivation. The synthetic
  # upstream imports tool_sequence.py even in default text-only mode.
  # Keep script files side-by-side; they are not chosen from a checkout.
  syntheticSources = pkgs.runCommand "aginctus-vm-synthetic-sources" { } ''
    mkdir -p "$out"
    cp ${../../scripts/vm_harness/synthetic_responses.py} "$out/synthetic_responses.py"
    cp ${../../scripts/vm_harness/synthetic_gateway_probe.py} "$out/synthetic_gateway_probe.py"
    cp ${../../scripts/vm_harness/tool_sequence.py} "$out/tool_sequence.py"
    cp ${../../scripts/vm_harness/gateway_tool_transport.py} "$out/gateway_tool_transport.py"
  '';
  syntheticProbe = pkgs.writeShellScriptBin "aginctus-vm-synthetic-probe" ''
    exec ${pkgs.python3}/bin/python3 ${syntheticSources}/synthetic_gateway_probe.py ${syntheticSources}/synthetic_responses.py${
      if toolTransport then " --tool-sequence" else ""
    }
  '';
in
assert (!toolTransport || synthetic);
pkgs.testers.runNixOSTest {
  name =
    if synthetic then
      (
        if toolTransport then
          "aginctus-vm-harness-synthetic-tool-gateway"
        else
          "aginctus-vm-harness-synthetic-gateway"
      )
    else
      "aginctus-vm-harness-gateway-topology";
  nodes.machine =
    { lib, ... }:
    {
      imports = [ (import ./guest.nix { inherit self system; }) ];
      virtualisation.memorySize = lib.mkForce 8192;
      virtualisation.diskSize = lib.mkForce 32768;
      environment.systemPackages = lib.optionals synthetic [ syntheticProbe ];
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
          allowedTCPPorts = [ 53 ] ++ lib.optionals synthetic [ 9080 ];
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
    machine.succeed(f"XDG_STATE_HOME=/run/aginctus-vm-gateway-state {cli} gateway up")
    machine.succeed("incus project show aginctus-gateway")
    machine.succeed("incus network show aginctus-mgmt")
    machine.succeed("incus network show aginctus-egress")
    assert machine.succeed("incus network get aginctus-mgmt ipv4.nat").strip() == "false"
    assert machine.succeed("incus network get aginctus-mgmt ipv4.routing").strip() == "false"
    assert machine.succeed("incus network get aginctus-mgmt ipv4.dhcp.gateway").strip() == "none"
    assert machine.succeed("incus network get aginctus-mgmt ipv6.address").strip() == "none"

    # Real Incus OCI containers must start and respond, not merely exist.
    machine.wait_until_succeeds(
      "timeout 15 incus exec --disable-stdin --project aginctus-gateway postgres-1 -- pg_isready -U litellm -d litellm </dev/null",
      timeout=180,
    )
    try:
        machine.wait_until_succeeds(
          "timeout 15 incus exec --disable-stdin --project aginctus-gateway litellm-1 -- python -c "
          "\"import urllib.request; assert urllib.request.urlopen("
          "'http://127.0.0.1:4000/health/liveliness', timeout=5).status == 200\" </dev/null",
          timeout=180,
        )
    except BaseException:
        import sys
        print(machine.execute("incus console --project aginctus-gateway litellm-1 --show-log")[1], file=sys.stderr)
        raise
    machine.succeed(f"{cli} gateway status")

    # The other real Aginctus infrastructure must coexist with the gateway.
    machine.succeed(f"{cli} network ensure")
    machine.succeed(f"{cli} herdr client apply")
    machine.succeed(f"{cli} workload apply dev")
    machine.succeed("incus info aginctus-herdr")
    machine.succeed("incus info aginctus-dev")

    # Reuse the strict pilot route parser against the real DHCP guest.
    # A route assertion is not packet-filter or provider-inference evidence.
    import runpy
    verify_routes = runpy.run_path("${../../scripts/acceptance/opencode-pilot-preflight.py}")["verify_ipv4_routes"]
    verify_routes(machine.succeed("timeout 15 incus exec --disable-stdin --project aginctus-gateway postgres-1 -- cat /proc/net/route"))
    for instance in ("aginctus-dev", "aginctus-herdr"):
        machine.wait_until_succeeds(f"timeout 15 incus exec --disable-stdin {instance} -- ip -4 addr show dev eth0 | grep -q 'inet '", timeout=120)
        verify_routes(machine.succeed(f"timeout 15 incus exec --disable-stdin {instance} -- cat /proc/net/route"))

    # DNS and private TCP must work without a management default gateway.
    machine.wait_until_succeeds("timeout 15 incus exec --disable-stdin aginctus-herdr -- ssh-keyscan -T 5 aginctus-dev.incus >/run/workload-host-key", timeout=120)
    machine.succeed(
      "timeout 15 incus exec --disable-stdin --project aginctus-gateway litellm-1 -- python -c "
      "\"import socket; socket.create_connection(('postgres-1', 5432), timeout=5).close()\""
    )
    import json
    gateway = json.loads(machine.succeed("incus list --project aginctus-gateway litellm-1 --format json"))[0]
    addresses = [addr["address"] for nic in gateway["state"]["network"].values()
                 for addr in nic.get("addresses", [])
                 if addr["family"] == "inet" and addr["address"].startswith("10.77.0.")]
    assert len(addresses) == 1, "expected one LiteLLM management IPv4 address"
    machine.succeed(f"timeout 15 incus exec --disable-stdin aginctus-dev -- runuser -u agent -- aginctus-egress-probe gateway {addresses[0]}")

    # Verify management-only attachments on non-gateway workloads.
    machine.succeed(
      "incus list --format json | jq -e "
      "'[.[] | select(.name == \"aginctus-herdr\" or .name == \"aginctus-dev\")] | length == 2'"
    )
    # Never register a model here: application health is not synthetic
    # Responses inference, and no real provider should be contacted.
  ''
  + lib.optionalString synthetic ''
    # Unlike topology-only acceptance, this calls real LiteLLM APIs and
    # proves scoped-key routing to the private, test-only Responses server.
    machine.succeed("aginctus-vm-synthetic-probe")
  '';
}
