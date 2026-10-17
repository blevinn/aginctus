{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-firewall-iptables";
  nodes.machine = { lib, pkgs, ... }: {
    imports = [ self.nixosModules.aginctus ];
    system.stateVersion = "26.05";
    virtualisation.memorySize = 3072;
    virtualisation.diskSize = 8192;

    networking.firewall = {
      enable = true;
      backend = "iptables";
      allowedTCPPorts = [ ];
      allowedUDPPorts = [ ];
    };
    networking.nftables.enable = false;
    services.firewalld.enable = false;
    virtualisation.incus.enable = true;

    services.aginctus = {
      enable = true;
      package = self.packages.${system}.aginctus;
      firewall.enable = true;
      images.herdrClient.enable = false;
      images.opencodeWorkload.enable = false;
      settings.incus.management.network = {
        name = "aginctus-test";
        ipv4.address = "10.247.81.1/24";
        ipv4.nat = false;
        ipv4.routing = false;
        ipv6.address = "none";
      };
    };

    environment.systemPackages = with pkgs; [
      iproute2
      iptables
      nftables
      incus
      dnsutils
      netcat-openbsd
    ];
  };

  testScript = ''
    import time

    start_all()
    machine.wait_for_unit("multi-user.target")
    machine.wait_for_unit("incus.service")
    machine.succeed("incus admin init --minimal")
    machine.succeed(
      "incus network create aginctus-test "
      "ipv4.address=10.247.81.1/24 ipv4.nat=false ipv4.routing=false "
      "ipv6.address=none user.aginctus.managed=true "
      "user.aginctus.resource=management-network"
    )
    machine.wait_until_succeeds("ip -4 address show aginctus-test | grep 10.247.81.1")
    machine.succeed("systemctl restart firewall.service")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")

    def owned_rules():
        return machine.succeed("iptables -S AGINCTUS_INPUT")

    machine.succeed("iptables -S nixos-fw | grep -F -- '-j AGINCTUS_INPUT'")
    rules = owned_rules()
    assert rules.count("--dport 53") == 2, rules
    assert rules.count("--dport 67") == 1, rules
    assert "--dport 22" not in rules, rules

    # The second transaction must replace the chain, not append duplicates.
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert owned_rules() == rules

    # The test client is a kernel network namespace attached to the real
    # Incus-owned bridge, not a manually provisioned external VM.
    machine.succeed("ip netns add test-guest")
    machine.succeed("ip link add guest-host type veth peer name guest0")
    machine.succeed("ip link set guest-host master aginctus-test")
    machine.succeed("ip link set guest-host up")
    machine.succeed("ip link set guest0 netns test-guest")
    machine.succeed("ip -n test-guest addr add 10.247.81.20/24 dev guest0")
    machine.succeed("ip -n test-guest link set lo up")
    machine.succeed("ip -n test-guest link set guest0 up")
    machine.wait_until_succeeds("ip netns exec test-guest dig +time=1 +tries=1 @10.247.81.1 aginctus-test.incus A")

    # With an actual listening port, a failed connect is proof of input
    # filtering rather than an absent service.
    machine.succeed("systemd-run --unit=aginctus-test-tcp --collect "
                    " ${pkgs.python3}/bin/python3 -m http.server 18080 --bind 10.247.81.1")
    machine.wait_until_succeeds("ss -lnt | grep -F ':18080'")
    machine.fail("ip netns exec test-guest nc -z -w 2 10.247.81.1 18080")

    # Fail closed when ownership is revoked; the chain must be empty.
    machine.succeed("incus network set aginctus-test user.aginctus.managed false")
    machine.fail("systemctl start aginctus-firewall-reconcile.service")
    empty = owned_rules()
    assert "--dport" not in empty, empty

    machine.succeed("incus network set aginctus-test user.aginctus.managed true")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert owned_rules() == rules

    machine.succeed("systemctl restart firewall.service")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert owned_rules() == rules
    machine.succeed("iptables -S nixos-fw | grep -F -- '-j AGINCTUS_INPUT'")

    machine.reboot()
    machine.wait_for_unit("multi-user.target")
    machine.wait_until_succeeds(
        "iptables -S AGINCTUS_INPUT | grep -F -- '--dport 53'"
    )
    machine.succeed("iptables -S nixos-fw | grep -F -- '-j AGINCTUS_INPUT'")
  '';
}
