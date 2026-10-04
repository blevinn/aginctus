# OpenCode workload

Status: initial NixOS workload image implemented; lifecycle orchestration and managed SSH identity remain follow-up work.

## Purpose

The OpenCode workload is the first concrete Aginctus agent workload.

It is a NixOS Incus container image containing:

- OpenCode from the pinned Nixpkgs revision;
- Herdr from the pinned Herdr flake;
- Git;
- OpenSSH server;
- a non-root `agent` account.

Herdr runs as the `agent` user so the Herdr session and agent runtime share the same workspace identity.

## Build

Build the image with:

```sh
just build opencode-workload
```

The `.#opencode-workload` flake target produces an Incus metadata tarball and squashfs root filesystem under `result-opencode-workload`.

Import or replace the local development image alias with:

```sh
just opencode-workload-image-update
```

The image is imported as:

```text
aginctus-opencode-workload
```

## Guest behavior

The image enables `sshd` but disables password and keyboard-interactive authentication. Root SSH login is disabled.

No SSH public key is baked into the image. Aginctus will provision the Herdr client identity when workload lifecycle orchestration is added.

The Herdr headless server starts at boot as the `agent` user with persistent state under `/var/lib/herdr` and runtime state under `/run/herdr`.

OpenCode is installed for interactive use by Herdr panes or direct workload sessions. It is not started as a system service.

## Model access

The image contains no model-provider credentials.

The intended path is for OpenCode to use a workload credential issued by the Aginctus AI gateway. Provider credentials remain in the gateway and never enter the workload image.

Until gateway credential delivery is implemented, this image only establishes the runtime environment.

## Lifecycle

This PR intentionally does not add another bespoke `ensure` implementation.

The declarative orchestration workstream will define how the image is instantiated, attached to the management network, given workspace storage, and provisioned with the Herdr SSH public key and gateway workload credential.

The image itself is kept independent of those host-specific values so it remains reproducible and reusable.
