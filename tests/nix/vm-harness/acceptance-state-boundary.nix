# Gate E: local project state fixture and public CLI authority fail-closed behavior.
# No real provider login, Incus host socket or credentials are required.
{ self, nixpkgs, system }:
let
  pkgs = import nixpkgs { inherit system; };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-acceptance-state-boundary";
  nodes.machine = { ... }: {
    virtualisation = { memorySize = 3072; diskSize = 8192; cores = 2; };
    environment.systemPackages = [
      pkgs.jq self.packages.${system}.aginctus
      self.packages.${system}.state-project-fixture
    ];
    system.stateVersion = "26.05";
  };
  testScript = ''
    import json

    start_all()
    machine.wait_for_unit("multi-user.target")
    first = "11111111-1111-4111-8111-111111111111"
    second = "22222222-2222-4222-8222-222222222222"
    cli = "aginctus"
    # Lack of an authenticated environment reader must never become success
    # merely because project and environment selectors were supplied.
    machine.fail(f"{cli} --incus-project aginctus environment list --output=json")
    machine.fail(f"{cli} --incus-project aginctus environment show {first} --output=json")
    for environment in (first, second):
        machine.fail(f"{cli} --incus-project aginctus --environment {environment} state show --layer accepted --output=json")
        machine.fail(f"{cli} --incus-project aginctus --environment {environment} state show --layer configured --output=json")
        machine.fail(f"{cli} --incus-project aginctus --environment {environment} state diff --output=json")
    # Observed state may not be promoted to a writable accepted layer.
    machine.fail(f"{cli} --incus-project default --environment {first} state show --layer accepted --output=json")
    machine.fail(f"{cli} --incus-project aginctus --environment {first} state show --layer observed --output=json")

    # The explicit test-only state store models multiple environments within
    # one project. It is not authenticated authority or deployed control plane.
    machine.succeed("install -d -m 0700 /var/lib/aginctus-state-acceptance-parent")
    fixture = "aginctus-project-state-fixture"
    scope = "--state-dir /var/lib/aginctus-state-acceptance-parent/state --server-id guest-test --project aginctus"
    initialized = json.loads(machine.succeed(f"{fixture} init {scope}"))
    assert initialized["initialized"]
    for environment in (first, second):
        status = json.loads(machine.succeed(f"{fixture} status {scope} --environment {environment}"))
        assert status["hasAccepted"] is False, status
        machine.succeed(f"{fixture} audit {scope} --environment {environment}")
        machine.fail(f"{fixture} accept {scope} --environment {environment}")
    # A foreign/default project cannot reuse the project's local-state identity.
    machine.fail(f"{fixture} status --state-dir /var/lib/aginctus-state-acceptance-parent/state --server-id guest-test --project default --environment {first}")
    machine.fail(f"{fixture} status --state-dir /var/lib/aginctus-state-acceptance-parent/state --server-id guest-test --project foreign --environment {first}")

    # No gateway credentials are provisioned by this scenario. Read-only
    # discovery must work, but help cannot trigger registration or login.
    machine.succeed(f"{cli} gateway provider --help")
    machine.succeed(f"{cli} gateway credential-storage audit --help")
  '';
}
