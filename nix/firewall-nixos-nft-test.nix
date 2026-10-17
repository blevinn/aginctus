{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-nixos-native-nft-incus";
  nodes.machine = { pkgs, ... }: {
    imports = [ self.nixosModules.aginctus ];
    system.stateVersion = "26.05";
    virtualisation = {
      memorySize = 4096;
      diskSize = 8192;
      cores = 2;
    };
    networking.nftables.enable = true;
    networking.firewall = {
      enable = true;
      backend = "nftables";
      allowedTCPPorts = [ ];
      allowedUDPPorts = [ ];
    };
    services.firewalld.enable = false;
    virtualisation.incus.enable = true;

    services.aginctus = {
      enable = true;
      package = self.packages.${system}.aginctus;
      firewall.enable = true;
      images.herdrClient.enable = false;
      images.opencodeWorkload.enable = false;
      settings.incus.management.network = {
        name = "aginctus-native";
        ipv4.address = "10.247.83.1/24";
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
    ];
  };

  testScript = ''
    start_all()
    machine.wait_for_unit("multi-user.target")
    machine.wait_for_unit("incus.service")
    machine.wait_for_unit("nftables.service")
    machine.succeed(
        "systemctl show -p ExecStart aginctus-firewall-reconcile.service"
        " | grep -F firewall.backend=nixos-nft"
    )
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed(
        "incus network create aginctus-native "
        "ipv4.address=10.247.83.1/24 ipv4.nat=false ipv4.routing=false "
        "ipv6.address=none raw.dnsmasq=address=/fixture.incus/10.247.83.1 "
        "user.aginctus.managed=true user.aginctus.resource=management-network"
    )
    machine.wait_until_succeeds(
        "ip -4 address show aginctus-native | grep -F 10.247.83.1"
    )
    machine.wait_until_succeeds(
        "nft list chain inet nixos-fw input-allow | grep -F 'jump aginctus_dns_input'"
    )
    machine.succeed("systemctl reset-failed aginctus-firewall-reconcile.service || true")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")

    def rules():
        return machine.succeed("nft list chain inet nixos-fw aginctus_dns_input")

    chain = rules()
    assert chain.count("dport 53") == 2, chain
    assert "dport 67" in chain, chain
    assert "ct state invalid return" in chain, chain
    assert "dport 22" not in chain, chain
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert chain == rules(), "native nftables reconcile duplicated rules"

    machine.succeed("ip netns add test-guest")
    machine.succeed("ip link add guest-host type veth peer name guest0")
    machine.succeed("ip link set guest-host master aginctus-native")
    machine.succeed("ip link set guest-host up")
    machine.succeed("ip link set guest0 netns test-guest")
    machine.succeed("ip -n test-guest address add 10.247.83.20/24 dev guest0")
    machine.succeed("ip -n test-guest link set lo up")
    machine.succeed("ip -n test-guest link set guest0 up")
    machine.wait_until_succeeds(
        "ip netns exec test-guest dig +time=1 +tries=1 @10.247.83.1 fixture.incus A"
        " | grep -F 10.247.83.1"
    )
    machine.wait_until_succeeds(
        "ip netns exec test-guest dig +tcp +time=1 +tries=1 @10.247.83.1 fixture.incus A"
        " | grep -F 10.247.83.1"
    )

    machine.succeed(
        "systemd-run --unit=aginctus-native-negative --collect "
        " ${pkgs.python3}/bin/python3 -m http.server 18080 --bind 10.247.83.1"
    )
    machine.wait_until_succeeds("ss -lnt | grep -F ':18080'")
    machine.fail("ip netns exec test-guest nc -z -w 2 10.247.83.1 18080")

    machine.succeed("incus network set aginctus-native user.aginctus.managed false")
    machine.fail("systemctl start aginctus-firewall-reconcile.service")
    assert "dport 53" not in rules(), rules()

    machine.succeed("incus network set aginctus-native user.aginctus.managed true")
    machine.succeed("systemctl reset-failed aginctus-firewall-reconcile.service")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert "dport 53" in rules()

    machine.succeed("systemctl restart nftables.service")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert "dport 53" in rules()

    machine.shutdown()
    machine.start()
    machine.wait_for_unit("multi-user.target")
    machine.wait_until_succeeds(
        "nft list chain inet nixos-fw aginctus_dns_input | grep -F 'dport 53'"
    )
  '';
}
