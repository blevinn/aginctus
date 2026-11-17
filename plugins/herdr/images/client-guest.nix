{
  herdrPackage,
  herdrClientCommand,
  sshClientIdentityHelper,
  sshHostTrustHelper,
  sshMachineConfigHelper,
  guestStateBindings,
}:
{ pkgs, ... }:
{
  imports = [
    (guestStateBindings {
      bindings = [
        {
          class = "system";
          subdir = "herdr";
          target = "/var/lib/herdr";
        }
        {
          class = "system";
          subdir = "ssh";
          target = "/var/lib/aginctus/ssh";
        }
      ];
    })
  ];

  system.stateVersion = "26.05";
  environment.systemPackages = [
    herdrClientCommand
    pkgs.openssh
    sshClientIdentityHelper
    sshHostTrustHelper
    sshMachineConfigHelper
  ];

  # Only the managed Herdr client consumes this guest-owned persistent SSH
  # config. Identity and host-key settings live on protected system state.
  programs.ssh.extraConfig = ''
    Include /var/lib/aginctus/ssh/machines/*.conf
  '';
  environment.variables = {
    XDG_CONFIG_HOME = "/var/lib/herdr/config";
    XDG_RUNTIME_DIR = "/run/herdr";
    XDG_STATE_HOME = "/var/lib/herdr";
  };

  systemd.services.herdr = {
    description = "Herdr headless server";
    wantedBy = [ "multi-user.target" ];
    after = [
      "network-online.target"
      "var-lib-herdr.mount"
      "var-lib-aginctus-ssh.mount"
    ];
    wants = [ "network-online.target" ];
    requires = [
      "var-lib-herdr.mount"
      "var-lib-aginctus-ssh.mount"
    ];
    serviceConfig = {
      Environment = [
        "HOME=/root"
        "XDG_CONFIG_HOME=/var/lib/herdr/config"
        "XDG_RUNTIME_DIR=/run/herdr"
        "XDG_STATE_HOME=/var/lib/herdr"
      ];
      ExecStart = "${herdrPackage}/bin/herdr server";
      Restart = "on-failure";
      RestartSec = "2s";
      RuntimeDirectory = "herdr";
      StateDirectory = "herdr";
    };
  };
}
