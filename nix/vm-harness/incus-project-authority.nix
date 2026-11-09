# Explicit, heavyweight disposable NixOS test. No pilot/host Incus daemon.
# Provision and use an actually project-restricted TLS principal, not an
# administrator selecting a project with --project.
{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-incus-project-authority";
  nodes.machine = { ... }: {
    system.stateVersion = "26.05";
    networking.nftables.enable = true;
    networking.useDHCP = false;
    virtualisation = {
      memorySize = 4096;
      diskSize = 16384;
      cores = 2;
      incus.enable = true;
      incus.package = pkgs.incus;
    };
    environment.systemPackages = [ pkgs.incus pkgs.jq pkgs.coreutils pkgs.util-linux ];
    users.users.operator = {
      uid = 1000;
      isNormalUser = true;
      home = "/home/operator";
      createHome = true;
      # Deliberately no incus or incus-admin membership: confined TLS only.
      extraGroups = [ ];
    };
  };

  testScript = ''
    start_all()
    machine.wait_for_unit("incus.service")
    # The test driver supplies an independent disposable QEMU disk and Incus
    # daemon. Storage is initialized inside the guest, never on the host.
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus info")
    machine.succeed("incus version")
    machine.succeed("incus project create aginctus-authz --config restricted=true --config features.images=true --config features.profiles=true --config features.storage.volumes=true --config features.networks=false")
    machine.succeed("incus project create unrelated --config restricted=true")
    machine.succeed("incus network create aginctus-authz-mgmt ipv4.address=none ipv6.address=none")
    # Explicitly bound access to one host-admin managed shared bridge/pool.
    machine.succeed("incus project set aginctus-authz restricted.devices.nic=managed restricted.networks.access=aginctus-authz-mgmt restricted.storage-pools.access=default")
    machine.succeed("incus project get aginctus-authz restricted | grep -qx true")
    machine.succeed("incus project get aginctus-authz features.networks | grep -qx false")
    machine.succeed("incus project get aginctus-authz restricted.networks.access | grep -qx aginctus-authz-mgmt")

    # A VM-only loopback HTTPS endpoint. No host networking or public listener.
    machine.succeed("incus config set core.https_address 127.0.0.1:8443")
    machine.wait_until_succeeds("curl --silent --insecure https://127.0.0.1:8443/1.0 >/dev/null", timeout=60)

    # The user creates its OWN ephemeral key. Root only imports the PUBLIC
    # certificate as restricted to the explicitly provisioned project.
    operator = "runuser -u operator -- env HOME=/home/operator USER=operator LOGNAME=operator"
    machine.succeed(operator + " incus remote generate-certificate")
    machine.succeed("test -f /home/operator/.config/incus/client.crt")
    machine.succeed("test -f /home/operator/.config/incus/client.key")
    machine.succeed("incus config trust add-certificate /home/operator/.config/incus/client.crt --restricted --projects aginctus-authz")
    machine.succeed(operator + " incus remote add scoped https://127.0.0.1:8443 --accept-certificate --project aginctus-authz", timeout=60)

    # Positive read must work through the TLS remote; direct local Unix
    # administration must remain denied to this principal.
    machine.succeed(operator + " incus list scoped: --project aginctus-authz --format json | jq -e 'length == 0'")
    machine.fail(operator + " incus --force-local project list")
    machine.succeed("incus list --project unrelated --format json | jq -e 'length == 0'")
    machine.succeed("incus project get aginctus-authz restricted | grep -qx true")
    machine.succeed("test $(stat -c '%a' /home/operator/.config/incus/client.key) = 600")

    # This initial fixture proves only setup/transport mechanics when run.
    # Denial matrix and isolation proof arrive in the next stacked PR.
  '';
}
