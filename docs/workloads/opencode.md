# OpenCode workload

Status: OpenCode image and declarative workload instance lifecycle implemented; remote SSH registration, gateway credential delivery and runtime readiness remain integration work.

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

No SSH public key is baked into the image. The image exposes a fixed `aginctus-ssh-authorize` helper and a dedicated root-owned authorization path for Aginctus-managed client keys; host-side orchestration still needs to deliver and reconcile those keys. See [SSH bootstrap workflow](../ssh-bootstrap-workflow.md) for the exact client-helper → host → workload-helper handoff.

The Herdr headless server starts at boot as the `agent` user with persistent state under `/var/lib/herdr` and runtime state under `/run/herdr`.

OpenCode is installed for interactive use by Herdr panes or direct workload sessions. It is not started as a system service.

## Model access

The image contains no model-provider credentials.

The intended path is for OpenCode to use a workload credential issued by the Aginctus AI gateway. Provider credentials remain in the gateway and never enter the workload image.

Until gateway credential delivery is implemented, this image only establishes the runtime environment.

## Lifecycle

Run `aginctus workload dev ensure` to reconcile the configured management network and workload instance through the Apply driver, or `aginctus workload dev teardown` to delete only the workload instance. Use `--dry-run` to inspect actions, and see [workload lifecycle](lifecycle.md) for ownership and isolation rules.

This lifecycle does not yet provision gateway workload credentials or complete runtime-specific readiness. The image remains independent of host-specific values.
