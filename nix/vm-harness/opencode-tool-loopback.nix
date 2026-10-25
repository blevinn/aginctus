# Explicit NixOS VM test: real pinned OpenCode executes a synthetic-only
# read/edit/go-test tool sequence as an UNPRIVILEGED guest user.
# This does not use Herdr, LiteLLM or any provider and is not full E2E.
{ self, nixpkgs, system }:
let
  pkgs = import nixpkgs { inherit system; };
  toolsFixture = pkgs.runCommand "aginctus-opencode-tool-loopback" { } ''
    mkdir -p "$out/guest" "$out/scripts"
    cp -R ${../../guest/opencode} "$out/guest/opencode"
    cp -R ${../../scripts/vm_harness} "$out/scripts/vm_harness"
  '';
in
pkgs.testers.runNixOSTest {
  name = "aginctus-vm-harness-opencode-tool-loopback";
  nodes.machine =
    { ... }:
    {
      imports = [ (import ./guest.nix { inherit self system; }) ];

      # No external egress or guest SSH: the provider is an authenticated
      # loopback-only script, and no registry access is required.
      virtualisation.vlans = [ ];
      networking.useDHCP = false;
      services.openssh.enable = false;

      users.users.testenv-agent = {
        isNormalUser = true;
        createHome = true;
        home = "/home/testenv-agent";
      };
      environment.systemPackages = [
        pkgs.go
        pkgs.python3
        pkgs.opencode
      ];
    };
  testScript = ''
    start_all()
    machine.wait_for_unit("multi-user.target")
    # No default network route may escape this disposable VM.
    assert machine.succeed("ip -4 route show default").strip() == ""
    # The test launcher rejects root, unpinned executable/script paths,
    # and missing worker-only opt-in even *inside* the disposable VM.
    machine.succeed(
      "timeout 240 ${pkgs.util-linux}/bin/runuser -u testenv-agent -- "
      "env AGINCTUS_TESTENV_TOOL_WORKER=DISPOSABLE_VM_ONLY "
      "${pkgs.python3}/bin/python3 "
      "${toolsFixture}/guest/opencode/test_real_runtime.py "
      "${pkgs.opencode}/bin/opencode --execute-tools"
    )
  '';
}
