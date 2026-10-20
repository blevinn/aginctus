{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  testZone = "aginctus-vm";
  conntrackProbe = pkgs.writeText "egress-conntrack-probe.py" ''
    import pathlib, socket, time
    client = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    client.bind(("10.247.83.25", 45000))
    client.connect(("8.8.4.4", 443))
    pathlib.Path("/tmp/egress-connected").touch()
    while not pathlib.Path("/tmp/egress-send-after-revoke").exists():
        time.sleep(0.1)
    client.settimeout(2)
    client.sendall(b"GET / HTTP/1.0\r\n\r\n")
    try:
        client.recv(4096)
        raise RuntimeError("revoked connection still receives traffic")
    except TimeoutError:
        pathlib.Path("/tmp/egress-isolated").touch()
  '';
  egressTest = self.packages.${system}.aginctus.overrideAttrs (_: {
    buildPhase = ''
      runHook preBuild
      go test -c ./internal/firewall -o aginctus-firewall-test
      runHook postBuild
    '';
    installPhase = ''
      mkdir -p $out/bin
      cp aginctus-firewall-test $out/bin/
    '';
    postInstall = "";
    doCheck = false;
  });

in
pkgs.testers.runNixOSTest {
  name = "aginctus-firewalld-incus";
  nodes.machine = { pkgs, lib, ... }: {
    imports = [ self.nixosModules.aginctus ];
    system.stateVersion = "26.05";
    virtualisation = {
      memorySize = 4096;
      diskSize = 8192;
      cores = 2;
    };
    networking.nftables.enable = true;
    networking.dhcpcd.allowInterfaces = [ "eth0" ];
    networking.firewall.enable = true;
    services.firewalld.enable = true;
    virtualisation.incus.enable = true;
    environment.etc."aginctus/disposable-firewall-vm".text = "YES";

    services.aginctus = {
      enable = true;
      package = self.packages.${system}.aginctus;
      firewall.enable = true;
      settings.firewall.zone = testZone;
      firewall.egress.enable = true;
      settings.gateway.egress.network.name = "aginctus-egress";
      images.herdrClient.enable = false;
      images.opencodeWorkload.enable = false;
      settings.incus.management.network = {
        name = "aginctus-test";
        ipv4.address = "10.247.82.1/24";
        ipv4.nat = false;
        ipv4.routing = false;
        ipv6.address = "none";
      };
    };

    # Synthetic discovery avoids external OCI images; the actual reconciler,
    # D-Bus, firewalld, nftables, Incus bridges and packet paths remain real.
    systemd.services.aginctus-egress-firewall.serviceConfig = {
      ExecStart = lib.mkForce "${egressTest}/bin/aginctus-firewall-test -test.run=^TestEgressRealFirewalld$ -test.v";
      Environment = [ "AGINCTUS_DISPOSABLE_FIREWALL_PHASE=apply" ];
    };
    boot.kernel.sysctl = {
      "net.bridge.bridge-nf-call-iptables" = 0;
      "net.bridge.bridge-nf-call-ip6tables" = 0;
    };

    environment.systemPackages = with pkgs; [
      incus
      iproute2
      nftables
      firewalld
      dnsutils
      netcat-openbsd
      python3
      jq
      conntrack-tools
    ];
  };

  testScript = ''
    start_all()
    machine.wait_for_unit("multi-user.target")
    machine.wait_for_unit("incus.service")
    machine.wait_for_unit("firewalld.service")
    machine.succeed("systemctl stop aginctus-egress-firewall.timer aginctus-egress-firewall.service")
    machine.succeed("AGINCTUS_DISPOSABLE_FIREWALL_PHASE=revoke ${egressTest}/bin/aginctus-firewall-test -test.run=^TestEgressRealFirewalld$ -test.v")
    # The NixOS service must select its intended backend explicitly, even if
    # firewalld's table temporarily disappears during a restart.
    machine.succeed(
        "systemctl show -p ExecStart aginctus-firewall-reconcile.service"
        " | grep -F firewall.backend=firewalld"
    )
    machine.wait_until_succeeds(
        "nft -a list table inet firewalld | grep -F 'flags owner,persist'"
    )
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed(
        "incus network create aginctus-test "
        "ipv4.address=10.247.82.1/24 ipv4.nat=false ipv4.routing=false "
        "ipv6.address=none raw.dnsmasq=address=/fixture.incus/10.247.82.1 user.aginctus.managed=true "
        "user.aginctus.resource=management-network"
    )
    machine.wait_until_succeeds(
        "ip -4 address show aginctus-test | grep -F 10.247.82.1"
    )
    machine.succeed("systemctl reset-failed aginctus-firewall-reconcile.service || true")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")

    # Custom name proves the integration does not rely on aginctus-dns.
    machine.succeed("test -r /etc/firewalld/zones/${testZone}.xml")
    zone = "--zone=${testZone}"
    def firewall(*args):
        return machine.succeed("firewall-cmd " + " ".join(args))

    assert firewall("--permanent", zone, "--get-target").strip() == "DROP"
    assert any(
        line.strip() == "target: DROP"
        for line in firewall(zone, "--list-all").splitlines()
    )
    assert "aginctus-test" in firewall(zone, "--list-interfaces")

    # A second real Incus bridge has durable, default-deny zone binding.
    machine.succeed("incus network create aginctus-egress ipv4.address=10.247.83.1/24 ipv4.nat=true ipv4.routing=true ipv4.dhcp.ranges=10.247.83.100-10.247.83.199 ipv6.address=none raw.dnsmasq=address=/fixture.incus/10.247.83.1 user.aginctus.managed=true user.aginctus.resource=gateway-egress-network")

    def peer(ns, bridge, addr):
        machine.succeed(f"ip netns add {ns}; ip link add {ns}-host type veth peer name {ns}-eth; ip link set {ns}-host master {bridge}; ip link set {ns}-host up; ip link set {ns}-eth netns {ns}; ip -n {ns} link set lo up; ip -n {ns} link set {ns}-eth up")
        if addr:
            machine.succeed(f"ip -n {ns} address add {addr} dev {ns}-eth")

    peer("litellm", "aginctus-egress", "10.247.83.25/24")
    machine.succeed("ip -n litellm address add 10.247.83.26/24 dev litellm-eth; ip -n litellm route add default via 10.247.83.1")
    machine.succeed("ip netns add world; ip link add wan-host type veth peer name wan-eth; ip link set wan-eth netns world; ip addr add 192.0.2.1/30 dev wan-host; ip link set wan-host up; ip -n world link set lo up; ip -n world link set wan-eth up; ip -n world addr add 192.0.2.2/30 dev wan-eth; ip -n world route add default via 192.0.2.1")
    for destination in ["8.8.4.4", "10.66.0.2", "11.12.13.2"]:
        machine.succeed(f"ip -n world addr add {destination}/32 dev lo; ip route add {destination}/32 via 192.0.2.2 dev wan-host")
    firewall("--zone=public", "--add-interface=wan-host")
    firewall("--permanent", "--zone=public", "--add-interface=wan-host")
    machine.succeed("AGINCTUS_DISPOSABLE_FIREWALL_PHASE=zone-fallback ${egressTest}/bin/aginctus-firewall-test -test.run=^TestEgressRealFirewalld$ -test.v")
    machine.succeed("systemd-run --unit=world-https --collect ip netns exec world ${pkgs.python3}/bin/python3 -m http.server 443 --bind 0.0.0.0")
    machine.succeed("systemd-run --unit=world-http --collect ip netns exec world ${pkgs.python3}/bin/python3 -m http.server 80 --bind 0.0.0.0")
    machine.wait_until_succeeds("ip netns exec world ss -lnt | grep -F ':443'")

    def connects(destination, port=443, source="10.247.83.25"):
        return f"ip netns exec litellm nc -z -w 1 -s {source} {destination} {port}"

    machine.fail(connects("8.8.4.4"))
    machine.succeed("systemctl reset-failed aginctus-egress-firewall.service; systemctl start aginctus-egress-firewall.service")
    machine.succeed(connects("8.8.4.4"))
    machine.succeed("AGINCTUS_DISPOSABLE_FIREWALL_PHASE=reload-race ${egressTest}/bin/aginctus-firewall-test -test.run=^TestEgressRealFirewalld$ -test.v")
    machine.fail(connects("8.8.4.4"))
    machine.succeed("systemctl start aginctus-egress-firewall.service; systemctl start aginctus-firewall-reconcile.service")
    machine.succeed(connects("8.8.4.4"))
    machine.fail(connects("8.8.4.4", 80))
    machine.fail(connects("8.8.4.4", source="10.247.83.26"))
    # Interface classification outranks overlapping source-based trusted zones.
    firewall("--zone=trusted", "--add-source=10.247.83.25/32")
    machine.fail(connects("8.8.4.4", 80))
    machine.succeed(connects("8.8.4.4"))
    firewall("--zone=trusted", "--remove-source=10.247.83.25/32")
    firewall("--policy=aginctus-egress-https", "--add-port=8443/tcp")
    machine.fail("AGINCTUS_DISPOSABLE_FIREWALL_PHASE=apply ${egressTest}/bin/aginctus-firewall-test -test.run=^TestEgressRealFirewalld$ -test.v")
    machine.fail(connects("8.8.4.4"))
    firewall("--policy=aginctus-egress-https", "--remove-port=8443/tcp")
    machine.succeed("systemctl start aginctus-egress-firewall.service")
    machine.succeed(connects("8.8.4.4"))
    machine.fail(connects("10.66.0.2"))
    machine.fail(connects("11.12.13.2"))
    machine.succeed("systemd-run --unit=host-denied-listener --collect ${pkgs.python3}/bin/python3 -m http.server 18082 --bind 0.0.0.0")
    machine.wait_until_succeeds("ss -lnt | grep -F ':18082'")
    machine.fail(connects("10.247.83.1", 18082))
    machine.fail(connects("10.247.82.1", 18082))
    machine.succeed("ip netns exec litellm dig +time=1 +tries=1 @10.247.83.1 fixture.incus A | grep -F 10.247.83.1")
    machine.succeed("ip netns exec litellm dig +tcp +time=1 +tries=1 @10.247.83.1 fixture.incus A | grep -F 10.247.83.1")
    machine.fail("ip netns exec litellm dig +time=1 +tries=1 @10.247.82.1 fixture.incus A")
    for ns, bridge, subnet in [("egdhcp", "aginctus-egress", "10.247.83."), ("mgdhcp", "aginctus-test", "10.247.82.")]:
        peer(ns, bridge, None)
        machine.succeed(f"ip netns exec {ns} ${pkgs.util-linux}/bin/unshare --mount --propagation private sh -c 'mount -t tmpfs tmpfs /run; export PATH=${
          pkgs.lib.makeBinPath [
            pkgs.coreutils
            pkgs.gnugrep
            pkgs.gnused
          ]
        }; exec ${pkgs.dhcpcd}/bin/dhcpcd --config /dev/null -4 --oneshot --timeout 20 --nohook resolv.conf {ns}-eth'")
        machine.succeed(f"ip -n {ns} -4 address show dev {ns}-eth | grep -F 'inet {subnet}'")

    machine.succeed("sysctl -w net.ipv6.conf.aginctus-egress.disable_ipv6=0; ip -6 addr add fd00:83::1/64 dev aginctus-egress; ip -n litellm -6 addr add fd00:83::25/64 dev litellm-eth; ip -n litellm -6 route add default via fd00:83::1; ip -6 addr add fd00:face::1/64 dev wan-host; ip -n world -6 addr add fd00:face::2/64 dev wan-eth; ip -n world -6 addr add 2001:4860::8888/128 dev lo; ip -6 route add 2001:4860::8888/128 via fd00:face::2; ip -n world -6 route add default via fd00:face::1")
    machine.succeed("systemd-run --unit=world-ipv6 --collect ip netns exec world ${pkgs.python3}/bin/python3 -m http.server 443 --bind 2001:4860::8888")
    machine.wait_until_succeeds("ip netns exec world ss -lnt6 | grep -F ':443'")
    machine.fail("ip netns exec litellm nc -6 -z -w 1 2001:4860::8888 443")

    # Trusted ingress/egress zones still cannot bypass the negative-priority
    # directional guards. Check fresh connections, not established replies.
    firewall("--zone=trusted", "--change-interface=wan-host")
    machine.fail(connects("8.8.4.4"))
    machine.succeed("systemd-run --unit=litellm-listener --collect ip netns exec litellm ${pkgs.python3}/bin/python3 -m http.server 18081 --bind 0.0.0.0")
    machine.wait_until_succeeds("ip netns exec litellm ss -lnt | grep -F ':18081'")
    machine.fail("ip netns exec world nc -z -w 1 10.247.83.25 18081")
    machine.succeed("nft add table ip fixture_nat; nft 'add chain ip fixture_nat prerouting { type nat hook prerouting priority dstnat; policy accept; }'; nft 'add rule ip fixture_nat prerouting iifname \"wan-host\" tcp dport 4444 dnat to 10.247.83.25:18081'")
    machine.fail("ip netns exec world nc -z -w 1 192.0.2.1 4444")
    firewall("--zone=public", "--change-interface=wan-host")
    machine.succeed(connects("8.8.4.4"))

    # Dual-homed LiteLLM retains its same-L2 PostgreSQL dependency. Routed
    # egress never provides an alternative path into management.
    peer("postgres", "aginctus-test", "10.247.82.30/24")
    machine.succeed("ip link add lm-mgmt-host type veth peer name lm-mgmt; ip link set lm-mgmt-host master aginctus-test; ip link set lm-mgmt-host up; ip link set lm-mgmt netns litellm; ip -n litellm link set lm-mgmt up; ip -n litellm addr add 10.247.82.25/24 dev lm-mgmt")
    machine.succeed("systemd-run --unit=postgres-listener --collect ip netns exec postgres ${pkgs.python3}/bin/python3 -m http.server 5432 --bind 0.0.0.0")
    machine.wait_until_succeeds("ip netns exec litellm nc -z -w 1 10.247.82.30 5432")
    machine.succeed("ip -n postgres route add default via 10.247.82.1")
    machine.fail("ip netns exec postgres nc -z -w 1 8.8.4.4 443")

    # Preserve evidence of the actual generated decision path, including the
    # ACCEPT inside firewalld preceding the default-deny policy.
    machine.succeed("nft list table inet firewalld > /tmp/egress-nftables.txt")
    machine.copy_from_vm("/tmp/egress-nftables.txt")

    machine.succeed("systemd-run --unit=egress-conntrack-probe --collect ip netns exec litellm ${pkgs.python3}/bin/python3 ${conntrackProbe}")
    machine.wait_until_succeeds("test -e /tmp/egress-connected")
    machine.succeed("conntrack -L -f ipv4 -p tcp --orig-src 10.247.83.25 --orig-port-src 45000 --orig-port-dst 443 | grep ESTABLISHED")
    machine.succeed("AGINCTUS_DISPOSABLE_FIREWALL_PHASE=revoke ${egressTest}/bin/aginctus-firewall-test -test.run=^TestEgressRealFirewalld$ -test.v")
    machine.fail("conntrack -L -f ipv4 -p tcp --orig-src 10.247.83.25 --orig-port-src 45000 --orig-port-dst 443 | grep ESTABLISHED")
    machine.succeed("touch /tmp/egress-send-after-revoke")
    machine.wait_until_succeeds("test -e /tmp/egress-isolated")
    machine.fail(connects("8.8.4.4"))
    machine.succeed("systemctl start aginctus-egress-firewall.timer")
    machine.wait_until_succeeds(connects("8.8.4.4"))
    firewall("--reload")
    machine.wait_until_succeeds(connects("8.8.4.4"))
    machine.succeed("systemctl stop aginctus-egress-firewall.timer; systemctl stop firewalld.service")
    machine.fail(connects("10.66.0.2"))
    machine.succeed("systemctl start firewalld.service; systemctl start aginctus-egress-firewall.timer")
    machine.wait_until_succeeds("firewall-cmd --state")
    machine.wait_until_succeeds("nft list table inet firewalld | grep -F aginctus-egress")
    machine.wait_until_succeeds(connects("8.8.4.4"))
    machine.fail(connects("10.66.0.2"))
    machine.fail(connects("11.12.13.2"))

    machine.succeed("systemctl reset-failed aginctus-firewall-reconcile.service; systemctl start aginctus-firewall-reconcile.service")

    def owned_rules():
        return firewall(zone, "--list-rich-rules")

    rules = owned_rules()
    assert rules.count('port="53"') == 2, rules
    assert 'port="67"' in rules, rules
    assert 'port="22"' not in rules, rules

    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert owned_rules() == rules, "reconciliation duplicated rules"
    assert firewall(zone, "--list-interfaces").split().count("aginctus-test") == 1

    machine.succeed("ip netns add test-guest")
    machine.succeed("ip link add guest-host type veth peer name guest0")
    machine.succeed("ip link set guest-host master aginctus-test")
    machine.succeed("ip link set guest-host up")
    machine.succeed("ip link set guest0 netns test-guest")
    machine.succeed("ip -n test-guest address add 10.247.82.20/24 dev guest0")
    machine.succeed("ip -n test-guest link set lo up")
    machine.succeed("ip -n test-guest link set guest0 up")

    machine.wait_until_succeeds(
        "ip netns exec test-guest dig +time=1 +tries=1 @10.247.82.1 fixture.incus A"
        " | grep -F 10.247.82.1"
    )
    machine.wait_until_succeeds(
        "ip netns exec test-guest dig +tcp +time=1 +tries=1 @10.247.82.1 fixture.incus A"
        " | grep -F 10.247.82.1"
    )

    machine.succeed("systemd-run --unit=aginctus-test-tcp --collect "
                    " ${pkgs.python3}/bin/python3 -m http.server 18080 --bind 10.247.82.1")
    machine.wait_until_succeeds("ss -lnt | grep -F ':18080'")
    machine.fail("ip netns exec test-guest nc -z -w 2 10.247.82.1 18080")

    machine.succeed("incus network set aginctus-test user.aginctus.managed false")
    machine.fail("systemctl start aginctus-firewall-reconcile.service")
    assert 'port="53"' not in owned_rules(), owned_rules()
    machine.succeed("incus network set aginctus-test user.aginctus.managed true")
    machine.succeed("systemctl reset-failed aginctus-firewall-reconcile.service")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert 'port="53"' in owned_rules()

    # Do not wait in the driver after restart: the service must handle the
    # daemon becoming active before its nftables INPUT table is ready.
    machine.succeed("systemctl restart firewalld.service")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert firewall("--permanent", zone, "--get-target").strip() == "DROP"
    assert any(
        line.strip() == "target: DROP"
        for line in firewall(zone, "--list-all").splitlines()
    )
    assert 'port="53"' in owned_rules()
    assert "aginctus-test" in firewall(zone, "--list-interfaces")

    machine.shutdown()
    machine.start()
    machine.wait_for_unit("multi-user.target")
    machine.wait_until_succeeds(
        "firewall-cmd --zone=${testZone} --list-rich-rules | grep -F 'port='"
    )
    machine.wait_until_succeeds(
        "firewall-cmd --policy=aginctus-egress-https --list-rich-rules | grep -F 'port=\"443\"'"
    )
  '';
}
