{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-egress-bootstrap-denial";
  nodes.machine = { ... }: {
    imports = [ self.nixosModules.aginctus ];
    system.stateVersion = "26.05";
    networking.nftables.enable = true;
    services.firewalld.enable = true;
    services.aginctus = {
      enable = true;
      firewall.egress.enable = true;
      settings.gateway.egress.network.name = "aginctus-egress";
      images.herdrClient.enable = false;
      images.opencodeWorkload.enable = false;
    };
    environment.systemPackages = with pkgs; [
      iproute2
      firewalld
      netcat-openbsd
      python3
      nftables
    ];
  };
  testScript = ''
    start_all()
    machine.wait_for_unit("firewalld.service")
    machine.succeed("ip link add aginctus-egress type bridge; ip addr add 10.247.83.1/24 dev aginctus-egress; ip link set aginctus-egress up")
    machine.succeed("ip netns add guest; ip link add guest-host type veth peer name guest-eth; ip link set guest-host master aginctus-egress; ip link set guest-host up; ip link set guest-eth netns guest; ip -n guest link set lo up; ip -n guest link set guest-eth up; ip -n guest addr add 10.247.83.25/24 dev guest-eth; ip -n guest route add default via 10.247.83.1")
    machine.succeed("ip netns add world; ip link add wan-host type veth peer name wan-eth; ip link set wan-eth netns world; ip addr add 192.0.2.1/30 dev wan-host; ip link set wan-host up; ip -n world link set lo up; ip -n world link set wan-eth up; ip -n world addr add 192.0.2.2/30 dev wan-eth; ip -n world route add default via 192.0.2.1; ip -n world addr add 8.8.4.4/32 dev lo; ip route add 8.8.4.4/32 via 192.0.2.2")
    machine.succeed("sysctl -w net.ipv4.ip_forward=1")
    machine.succeed("firewall-cmd --zone=trusted --add-interface=wan-host")
    machine.succeed("systemd-run --unit=world-listener ip netns exec world ${pkgs.python3}/bin/python3 -m http.server 443 --bind 0.0.0.0")
    machine.succeed("systemd-run --unit=guest-listener ip netns exec guest ${pkgs.python3}/bin/python3 -m http.server 443 --bind 0.0.0.0")
    machine.succeed("systemd-run --unit=host-listener ${pkgs.python3}/bin/python3 -m http.server 443 --bind 0.0.0.0")
    machine.wait_until_succeeds("ip netns exec world ss -lnt | grep ':443'")
    machine.wait_until_succeeds("ip netns exec guest ss -lnt | grep ':443'")
    machine.wait_until_succeeds("ss -lnt | grep ':443'")
    def denied():
        machine.fail("ip netns exec guest nc -z -w 1 8.8.4.4 443")
        machine.fail("ip netns exec guest nc -z -w 1 192.0.2.2 443")
        machine.fail("ip netns exec guest nc -z -w 1 10.247.83.1 443")
        machine.fail("ip netns exec world nc -z -w 1 10.247.83.25 443")
    def dormant():
        assert "target: DROP" in machine.succeed("firewall-cmd --zone=aginctus-egress --list-all")
        for policy in ["aginctus-egress-https", "aginctus-egress-host"]:
            assert not machine.succeed(f"firewall-cmd --policy={policy} --list-rich-rules").strip()
        assert not machine.succeed("firewall-cmd --policy=aginctus-egress-https --list-egress-zones").strip()
    dormant()
    denied()
    machine.succeed("firewall-cmd --zone=trusted --add-source=10.247.83.25/32")
    denied()
    machine.succeed("firewall-cmd --reload")
    machine.succeed("firewall-cmd --zone=trusted --add-interface=wan-host")
    dormant()
    denied()
    machine.succeed("systemctl stop firewalld")
    denied()
    machine.succeed("systemctl start firewalld")
    machine.succeed("firewall-cmd --zone=trusted --add-interface=wan-host")
    dormant()
    denied()
  '';
}
