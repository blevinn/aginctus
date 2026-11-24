# Real Incus Compose gateway and packaged Herdr/OpenCode deployment.
# This is explicitly heavyweight and may need OCI registry access for
# image caching; it never registers or calls an external model provider.
{
  self,
  nixpkgs,
  system,
  synthetic ? false,
  toolTransport ? false,
  fullStack ? false,
}:
let
  pkgs = import nixpkgs { inherit system; };
  lib = nixpkgs.lib;
  # Bundle relative imports in one immutable derivation. The synthetic
  # upstream imports tool_sequence.py even in default text-only mode.
  # Keep script files side-by-side; they are not chosen from a checkout.
  syntheticSources = pkgs.runCommand "aginctus-vm-synthetic-sources" { } ''
    mkdir -p "$out"
    cp ${../../../tests/vm_harness/synthetic_responses.py} "$out/synthetic_responses.py"
    cp ${../../../tests/vm_harness/synthetic_gateway_probe.py} "$out/synthetic_gateway_probe.py"
    cp ${../../../tests/vm_harness/tool_sequence.py} "$out/tool_sequence.py"
    cp ${../../../tests/vm_harness/gateway_tool_transport.py} "$out/gateway_tool_transport.py"
    ${lib.optionalString fullStack ''
      cp ${../../../tests/vm_harness/full_stack.py} "$out/full_stack.py"
      cp ${../../../tests/vm_harness/evidence.py} "$out/evidence.py"
      cp ${../../acceptance/herdr-lifecycle.py} "$out/herdr-lifecycle.py"
    ''}
  '';
  syntheticProbe = pkgs.writeShellScriptBin "aginctus-vm-synthetic-probe" ''
    exec ${pkgs.python3}/bin/python3 ${syntheticSources}/${
      if fullStack then
        "full_stack.py"
      else
        "synthetic_gateway_probe.py ${syntheticSources}/synthetic_responses.py"
  }${if toolTransport then " --tool-sequence" else ""}
  '';
  nft = "${pkgs.nftables}/bin/nft";
in
assert (!toolTransport || synthetic);
assert (!fullStack || (synthetic && !toolTransport));
pkgs.testers.runNixOSTest {
  name =
    if fullStack then
      "aginctus-vm-harness-full-stack"
    else if synthetic then
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
      imports = [
        (import ./guest.nix { inherit self system; })
      ]
      ++ lib.optional fullStack (import ./oci-staging.nix { inherit self system; });
      virtualisation.memorySize = lib.mkForce 8192;
      virtualisation.diskSize = lib.mkForce 32768;
      virtualisation.cores = lib.mkIf fullStack 2;
      boot.supportedFilesystems = lib.optionals fullStack [ "btrfs" ];
      services.timesyncd.enable = lib.mkIf fullStack false;
      environment.systemPackages =
        lib.optionals synthetic [ syntheticProbe ] ++ lib.optionals fullStack [ pkgs.go ];
      # Compose needs OCI network gateway controls absent from the default LTS daemon.
      virtualisation.incus.package = pkgs.incus;
      networking.dhcpcd.allowInterfaces = [ "eth0" ];
      networking.networkmanager.enable = false;
      services.openssh.enable = lib.mkIf fullStack false;
      systemd.network.wait-online.enable = lib.mkIf fullStack false;
      networking.nameservers = lib.mkIf fullStack [ "10.0.2.3" ];
      services.aginctus.images.herdrClient.enable = lib.mkIf fullStack false;
      networking.dhcpcd.extraConfig = lib.mkIf fullStack ''
        nohook resolv.conf
        static domain_name_servers=10.0.2.3
      '';
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
        source =
          if fullStack then
            self.packages.${system}.vm-harness-opencode-workload
          else
            self.packages.${system}.opencode-workload;
      };
    };
  testScript = ''
    import json
    import sys
    start_all()
    machine.wait_for_unit("incus.service")
  ''
  + (
    if fullStack then
      ''
        # Guest-local loopback Btrfs, as in storage-network.nix. Use it for all
        # workload roots, gateway roots and retained custom volumes, not just a probe.
        # One preseed transaction: --minimal creates a conflicting dir pool.
        # No network is provisioned; runtime networking is reconciled later.
        machine.succeed(
            "incus admin init --preseed <<'EOF'\n"
            "config: {}\n"
            "networks: []\n"
            "storage_pools:\n"
            "- name: default\n"
            "  driver: btrfs\n"
            "  config:\n"
            "    size: 20GiB\n"
            "profiles:\n"
            "- name: default\n"
            "  config: {}\n"
            "  devices:\n"
            "    root:\n"
            "      type: disk\n"
            "      path: /\n"
            "      pool: default\n"
            "EOF\n"
        )
        def inspect_storage():
            pool = json.loads(machine.succeed("incus query /1.0/storage-pools/default"))
            assert pool["driver"] == "btrfs", "full-stack storage is not Btrfs"
            assert pool["config"]["source"].startswith("/var/lib/incus/disks/"), "expected guest-local loop backing"
            machine.succeed("test -f " + pool["config"]["source"])
            volumes = json.loads(machine.succeed("incus storage volume list default --all-projects --format json"))
            assert isinstance(volumes, list), "storage inventory unavailable"
            return volumes
        inspect_storage()
      ''
    else
      ''
        machine.succeed("incus admin init --auto --storage-backend dir")
      ''
  )
  + ''
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
  ''
  + lib.optionalString fullStack ''
    # Provision only inside the disposable guest, then close external egress
    # before Compose deployment. Private bridge traffic remains available.
    machine.succeed("systemctl start aginctus-test-oci-stage.service")
    machine.succeed("oci-stage check")
    machine.succeed("nft add table inet aginctus_full_stack")
    machine.succeed("nft 'add chain inet aginctus_full_stack output { type filter hook output priority -10; policy accept; }'")
    machine.succeed("nft 'add chain inet aginctus_full_stack forward { type filter hook forward priority -10; policy accept; }'")
    # Mark only candidate external packets for nft tracing before the
    # unchanged terminal reject rules. No payload capture is enabled.
    for chain in ("output", "forward"):
        machine.succeed(
            f'nft add rule inet aginctus_full_stack {chain} '
            'oifname eth0 meta nftrace set 1'
        )
        machine.succeed(f'nft add rule inet aginctus_full_stack {chain} oifname eth0 counter reject')
    # The monitor output stays in the disposable guest. nftrace is
    # set solely on eth0 egress candidates above; tracing reports packet
    # headers/metadata but never captures payloads.
    machine.succeed("${nft} monitor trace >/run/aginctus-packet-trace.log 2>&1 & echo $! >/run/aginctus-packet-trace.pid")
    machine.wait_until_succeeds(
        "kill -0 $(cat /run/aginctus-packet-trace.pid) && "
        "test -f /run/aginctus-packet-trace.log"
    )
    def packet_guard_counters():
        result = {}
        for chain in ("output", "forward"):
            rows = json.loads(machine.succeed(
                f"nft -j list chain inet aginctus_full_stack {chain}"
            ))["nftables"]
            rules = [row["rule"] for row in rows if "rule" in row]
            matches = []
            for rule in rules:
                expressions = rule.get("expr", [])
                has_eth0_egress = any(
                    expr.get("match", {}).get("left", {}).get("meta", {}).get("key") == "oifname"
                    and expr.get("match", {}).get("right") == "eth0"
                    for expr in expressions
                )
                is_reject = any("reject" in expr for expr in expressions)
                counters = [expr["counter"].get("packets") for expr in expressions
                            if "counter" in expr]
                if has_eth0_egress and is_reject:
                    matches.append({"rule": rule, "counters": counters})
            result[chain] = {"rules": rules, "matches": matches}
        return result
    def assert_no_external_packet_attempts(context):
        trace_size = int(machine.succeed("wc -c </run/aginctus-packet-trace.log"))
        trace = machine.succeed("dd if=/run/aginctus-packet-trace.log bs=65536 count=1 status=none")
        rules = machine.succeed("${nft} -a list table inet aginctus_full_stack")
        print(f"{context}: bounded packet-header trace (max=65536 bytes)", file=sys.stderr)
        print(f"trace-log-bytes={trace_size}", file=sys.stderr)
        print(trace[:65536], file=sys.stderr)
        print(f"{context}: protected reject rules", file=sys.stderr)
        print(rules, file=sys.stderr)
        observed = packet_guard_counters()
        failures = {
            chain: details for chain, details in observed.items()
            if len(details["matches"]) != 1
            or details["matches"][0]["counters"] != [0]
        }
        print(f"{context}: output matches={len(observed['output']['matches'])} "
              f"counters={[match['counters'] for match in observed['output']['matches']]}; "
              f"forward matches={len(observed['forward']['matches'])} "
              f"counters={[match['counters'] for match in observed['forward']['matches']]}")
        if failures:
            raise AssertionError(
                f"{context}: external packet guard mismatch; per-chain rules and counters="
                f"{json.dumps(observed, sort_keys=True)}"
            )
        return observed
  ''
  + ''
    machine.succeed(f"XDG_STATE_HOME=/run/aginctus-vm-gateway-state {cli} gateway up")
  ''
  + lib.optionalString fullStack ''
    machine.succeed("oci-stage check")
    assert_no_external_packet_attempts("staged deployment")
  ''
  + ''
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
    verify_routes = runpy.run_path("${../../acceptance/opencode-pilot-preflight.py}")["verify_ipv4_routes"]
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
  ''
  + (
    if fullStack then
      ''
        storage_before = inspect_storage()
        assert storage_before, "full-stack must actually use the Btrfs pool"
        driver_gates = dict.fromkeys(("storage_preserved", "external_traffic", "external_deny"), "not_verified")
        try:
            machine.succeed("aginctus-vm-synthetic-probe")
            driver_gates["external_traffic"] = "fail"
            assert_no_external_packet_attempts("synthetic agent execution")
            driver_gates["external_traffic"] = "pass"
            # Narrow packet control, not fleet isolation or live inference.
            driver_gates["external_deny"] = "fail"
            def denied_packets():
                rules = json.loads(machine.succeed("nft -j list chain inet aginctus_full_stack forward"))["nftables"]
                return sum(
                    expr["counter"]["packets"]
                    for row in rules if "rule" in row
                    for expr in row["rule"].get("expr", [])
                    if "counter" in expr
                    and any("reject" in candidate for candidate in row["rule"].get("expr", []))
                    and any(
                        candidate.get("match", {}).get("left", {}).get("meta", {}).get("key") == "oifname"
                        and candidate.get("match", {}).get("right") == "eth0"
                        for candidate in row["rule"].get("expr", [])
                    )
                )
            before = denied_packets()
            machine.succeed(
              "timeout 15 incus exec --disable-stdin --project aginctus-gateway litellm-1 -- python -c "
              "\"import socket; s=socket.socket(); s.settimeout(3); "
              "assert s.connect_ex(('1.1.1.1', 443)) != 0; s.close()\""
            )
            assert denied_packets() > before, "gateway external TCP attempt did not hit the deny counter"
            machine.succeed("oci-stage check")
            driver_gates["external_deny"] = "pass"
        finally:
            try:
                driver_gates["storage_preserved"] = "fail"
                storage_after = inspect_storage()
                # Preservation is NOT volume, snapshot or loop-image removal.
                assert {(v["project"], v["type"], v["name"]) for v in storage_before}.issubset(
                    {(v["project"], v["type"], v["name"]) for v in storage_after}
                ), "scoped workspace cleanup unexpectedly removed retained storage"
                driver_gates["storage_preserved"] = "pass"
            finally:
                # Fixed immutable reader emits only a bounded allowlisted record.
                # Revalidate on the driver side; never copy raw guest files/logs.
                contract = runpy.run_path("${syntheticSources}/evidence.py")
                code, payload = machine.execute("${pkgs.python3}/bin/python3 ${syntheticSources}/evidence.py")
                assert code == 0 and len(payload.encode()) <= contract["MAX_BYTES"], "guest evidence unavailable"
                record = contract["normalize"](json.loads(payload))
                record["gates"].update(driver_gates)
                contract["write_artifact"](machine.out_dir, record)
      ''
    else
      lib.optionalString synthetic ''
        machine.succeed("aginctus-vm-synthetic-probe")
      ''
  );
}
