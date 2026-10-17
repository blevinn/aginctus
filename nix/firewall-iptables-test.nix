{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  # NixOS's Incus module rejects the iptables firewall backend. This VM
  # deliberately tests the iptables integration in isolation. The read-only
  # Incus ownership policy is independently unit-tested in Go.
  fixtureCli = pkgs.writeShellScriptBin "aginctus" ''
    set -eu
    if [ "${1:-}" = "--configuration-file" ]; then
      shift 2
    fi
    if [ "${1:-}" != "network" ] || [ "${2:-}" != "firewall" ] || [ "${3:-}" != "reconcile" ]; then
      exit 2
    fi
    if [ -e /run/aginctus-test/revoked ]; then
      allowed=false
    else
      allowed=true
    fi
    {
      echo '*filter'
      echo ':AGINCTUS_INPUT - [0:0]'
      echo '-F AGINCTUS_INPUT'
      if [ "$allowed" = true ]; then
        echo '-A AGINCTUS_INPUT -i aginctus-test -s 10.247.81.0/24 -d 10.247.81.1 -p udp --dport 53 -j ACCEPT'
        echo '-A AGINCTUS_INPUT -i aginctus-test -s 10.247.81.0/24 -d 10.247.81.1 -p tcp --dport 53 -j ACCEPT'
        echo '-A AGINCTUS_INPUT -i aginctus-test -p udp --sport 68 --dport 67 -j ACCEPT'
      fi
      echo COMMIT
    } | ${pkgs.iptables}/bin/iptables-restore -w --noflush
    if [ "$allowed" = false ]; then
      exit 1
    fi
  '';
in
pkgs.testers.runNixOSTest {
  name = "aginctus-firewall-iptables";
  nodes.machine = { pkgs, ... }: {
    imports = [ self.nixosModules.aginctus ];
    system.stateVersion = "26.05";
    virtualisation.memorySize = 2048;

    networking.firewall = {
      enable = true;
      backend = "iptables";
      allowedTCPPorts = [ ];
      allowedUDPPorts = [ ];
    };
    networking.nftables.enable = false;
    services.firewalld.enable = false;

    # The network is intentionally a synthetic bridge. Starting NixOS Incus
    # here is impossible because its module requires native nftables.
    services.aginctus = {
      enable = true;
      package = fixtureCli;
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

    systemd.services.aginctus-test-bridge = {
      description = "Disposable firewall test bridge";
      wantedBy = [ "multi-user.target" ];
      before = [ "aginctus-firewall-reconcile.service" ];
      serviceConfig.Type = "oneshot";
      serviceConfig.RemainAfterExit = true;
      path = [ pkgs.iproute2 pkgs.coreutils ];
      script = ''
        mkdir -p /run/aginctus-test
        ip link show aginctus-test >/dev/null 2>&1 || ip link add aginctus-test type bridge
        ip address show dev aginctus-test | grep -q 10.247.81.1 ||
          ip address add 10.247.81.1/24 dev aginctus-test
        ip link set aginctus-test up
      '';
    };

    services.dnsmasq = {
      enable = true;
      settings = {
        interface = "aginctus-test";
        bind-dynamic = true;
        no-resolv = true;
        address = "/fixture.test/10.247.81.1";
      };
    };

    environment.systemPackages = with pkgs; [
      iproute2
      iptables
      nftables
      dnsutils
      netcat-openbsd
      python3
    ];
  };

  testScript = ''
    start_all()
    machine.wait_for_unit("multi-user.target")
    machine.wait_for_unit("aginctus-test-bridge.service")
    machine.wait_until_succeeds("ip -4 address show aginctus-test | grep -F 10.247.81.1")
    machine.wait_for_unit("dnsmasq.service")

    # The hook must be in the effective NixOS decision chain.
    machine.succeed("iptables -S nixos-fw | grep -F -- '-j AGINCTUS_INPUT'")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")

    def owned_rules():
        return machine.succeed("iptables -S AGINCTUS_INPUT")

    rules = owned_rules()
    assert rules.count("--dport 53") == 2, rules
    assert rules.count("--dport 67") == 1, rules
    assert "--dport 22" not in rules, rules

    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert owned_rules() == rules, "reconciliation duplicated rules"

    # Network namespace is an isolated client connected to the bridge.
    machine.succeed("ip netns add test-guest")
    machine.succeed("ip link add guest-host type veth peer name guest0")
    machine.succeed("ip link set guest-host master aginctus-test")
    machine.succeed("ip link set guest-host up")
    machine.succeed("ip link set guest0 netns test-guest")
    machine.succeed("ip -n test-guest address add 10.247.81.20/24 dev guest0")
    machine.succeed("ip -n test-guest link set lo up")
    machine.succeed("ip -n test-guest link set guest0 up")

    machine.wait_until_succeeds(
        "ip netns exec test-guest dig +time=1 +tries=1 @10.247.81.1 fixture.test A"
        " | grep -F 10.247.81.1"
    )
    machine.wait_until_succeeds(
        "ip netns exec test-guest dig +tcp +time=1 +tries=1 @10.247.81.1 fixture.test A"
        " | grep -F 10.247.81.1"
    )

    # Probe an open service, so failure is genuinely firewall enforcement.
    machine.succeed("systemd-run --unit=aginctus-test-tcp --collect "
                    " ${pkgs.python3}/bin/python3 -m http.server 18080 --bind 10.247.81.1")
    machine.wait_until_succeeds("ss -lnt | grep -F ':18080'")
    machine.fail("ip netns exec test-guest nc -z -w 2 10.247.81.1 18080")

    machine.succeed("touch /run/aginctus-test/revoked")
    machine.fail("systemctl start aginctus-firewall-reconcile.service")
    assert "--dport" not in owned_rules()
    machine.succeed("rm /run/aginctus-test/revoked")
    machine.succeed("systemctl reset-failed aginctus-firewall-reconcile.service")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert owned_rules() == rules

    machine.succeed("systemctl restart firewall.service")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    assert owned_rules() == rules

    machine.reboot()
    machine.wait_for_unit("multi-user.target")
    machine.wait_until_succeeds("iptables -S AGINCTUS_INPUT | grep -F -- '--dport 53'")
    machine.succeed("iptables -S nixos-fw | grep -F -- '-j AGINCTUS_INPUT'")
  '';
}
