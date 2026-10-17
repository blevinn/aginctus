{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-firewalld-incus";
  nodes.machine = { pkgs, ... }: {
    imports = [ self.nixosModules.aginctus ];
    system.stateVersion = "26.05";
    virtualisation = {
      memorySize = 4096;
      diskSize = 8192;
      cores = 2;
    };
    networking.nftables.enable = true;
    networking.firewall.enable = true;
    services.firewalld.enable = true;
    virtualisation.incus.enable = true;

    services.aginctus = {
      enable = true;
      package = self.packages.${system}.aginctus;
      firewall.enable = true;
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

    environment.systemPackages = with pkgs; [
      incus
      iproute2
      nftables
      dnsutils
      netcat-openbsd
      python3
      jq
    ];
  };

  testScript = ''
    start_all()
    machine.wait_for_unit("multi-user.target")
    machine.wait_for_unit("incus.service")
    machine.wait_for_unit("firewalld.service")
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
    # Fail with actionable nft diagnostics from this disposable VM. Do not
    # expose raw nft output in the production service journal.
    status, service_output = machine.execute(
        "systemctl start aginctus-firewall-reconcile.service"
    )
    if status != 0:
        print("Aginctus firewall service failed: " + service_output)
        for command in [
            "systemctl show aginctus-firewall-reconcile.service -p ExecMainStatus -p Result",
            "nft -a list chain inet firewalld filter_INPUT",
            "nft -a list chain inet firewalld aginctus_dns_input",
            "aginctus --configuration-file /etc/aginctus/config.json network firewall policy",
        ]:
            code, output = machine.execute(command)
            print(f"DIAG {command}: exit={code} output={output}")
        machine.succeed("""cat > /tmp/aginctus-firewalld-preflight.nft <<'NFT'
add chain inet firewalld aginctus_dns_input
insert rule inet firewalld filter_INPUT jump aginctus_dns_input comment "aginctus:guest-host-dns-dhcp"
add rule inet firewalld aginctus_dns_input iifname "aginctus-test" ip saddr 10.247.82.0/24 ip daddr 10.247.82.1 udp dport 53 accept
add rule inet firewalld aginctus_dns_input iifname "aginctus-test" ip saddr 10.247.82.0/24 ip daddr 10.247.82.1 tcp dport 53 accept
add rule inet firewalld aginctus_dns_input iifname "aginctus-test" udp sport 68 udp dport 67 accept
NFT""")
        code, output = machine.execute(
            "nft --check -f /tmp/aginctus-firewalld-preflight.nft"
        )
        print(f"DIAG nft batch preflight: exit={code} output={output}")
        raise AssertionError("Aginctus nft reconciliation failed; see DIAG lines above")


    def owned_rules():
        return machine.succeed("nft list chain inet firewalld aginctus_dns_input")

    def hook():
        return machine.succeed("nft -a list chain inet firewalld filter_INPUT")

    assert "jump aginctus_dns_input" in hook()
    rules = owned_rules()
    assert rules.count("dport 53") == 2, rules
    assert "dport 67" in rules, rules
    assert "dport 22" not in rules, rules

    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert owned_rules() == rules, "reconciliation duplicated rules"
    assert hook().count("jump aginctus_dns_input") == 1, hook()

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
    assert "dport 53" not in owned_rules(), owned_rules()
    machine.succeed("incus network set aginctus-test user.aginctus.managed true")
    machine.succeed("systemctl reset-failed aginctus-firewall-reconcile.service")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert "dport 53" in owned_rules()

    machine.succeed("systemctl restart firewalld.service")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert hook().count("jump aginctus_dns_input") == 1, hook()
    assert "dport 53" in owned_rules()

    machine.shutdown()
    machine.start()
    machine.wait_for_unit("multi-user.target")
    machine.wait_until_succeeds(
        "nft list chain inet firewalld aginctus_dns_input | grep -F 'dport 53'"
    )
  '';
}
