# Dedicated credential-manager daemon deployment

This increment adds an **explicit**, opt-in daemon deployment command. It does not run under `gateway apply` or aggregate `apply`.

## Prerequisites

1. Build `nix build .#oauth-gateway` and import it with `aginctus images update aginctus-oauth-gateway result`.
2. Existing owned management and egress bridges must be present.
3. The pool configured at `gateway.credentials.storage.pool` must be available.

`aginctus gateway credential-daemon apply` verifies or creates the owned private credential volume, then reconciles an Aginctus-owned instance named `aginctus-gateway-<id>-credential-daemon` with a dedicated Incus disk device attached **only** at `/var/lib/aginctus/oauth`. The daemon gets management and egress NICs; it listens only on loopback and refuses inference. Neither LiteLLM nor PostgreSQL nor workloads receive the volume. Existing foreign instance ownership fails closed.

The image's systemd service asserts the mountpoint and requires its mount unit. A root-only pre-start initializes mode `0700` and ownership of the mounted root for the unprivileged daemon. It does not recursively modify existing files. The daemon checks directory ownership and privacy. A missing mount must prevent service startup.

**Important:** `credential-daemon apply` mutates Incus: creates the instance and can start it, and creates the owned volume if absent. Run only after reviewing the rendered deployment plan and on a host where those resources are intended. This command does not transfer any tokens or enable inference. Future increments will implement authenticated handoff and service-to-service inference authentication.

Deployment is deliberately separate from the existing Compose gateway until runtime integration and safe removal/reconciliation semantics are verified.
