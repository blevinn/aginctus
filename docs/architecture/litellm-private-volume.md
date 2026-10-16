# LiteLLM private credential volume attachment

Aginctus uses LiteLLM's built-in `chatgpt/` Responses provider. This step prepares an Incus storage boundary **without enabling OAuth login or inference**.

## Important: Incus project ownership

The earlier `gateway credential-storage ensure` command creates a retained, owned custom volume in the **default** Incus project. The gateway's `incus-compose` deploys service instances into a separate Incus project, normally `aginctus-gateway`, with project-isolated custom volumes. Incus does not implicitly let this instance attach an unrelated default-project volume.

`gateway credential-storage attach` therefore checks the gateway Compose project has `features.storage.volumes=true` and ensures a **separate, project-local managed custom filesystem volume** with the same logical name. It does **not** copy credentials, migrate, mount, reconfigure or delete the legacy default-project volume. In the present pilot that volume should remain empty and unattached. Migration/cleanup is a separate explicit decision.

**Do not** express this sensitive volume as a regular incus-compose named external volume: current incus-compose treats missing external volumes as createable and may delete them through volume removal. Aginctus keeps lifecycle ownership independently.

## Command (explicit live Incus mutation)

```sh
aginctus gateway credential-storage attach
```

Requires an existing owned `litellm-1` guest under the configured Compose project, matching `user.aginctus.managed=true`, `user.aginctus.resource=compose-instance` and `user.aginctus.deployment=<project>`. The private volume is created in that project if missing, ownership-checked, and required to have no consumers except the intended LiteLLM instance. The only added disk device is `aginctus_chatgpt_credentials` at `/var/lib/aginctus/chatgpt`. Disk mutation is concurrency-protected with the instance ETag. Repeating the command is idempotent; collisions or foreign ownership fail closed.

This command **does not** change the service environment, set `CHATGPT_TOKEN_DIR`, import tokens, run a login, or activate an inference route. Before using the mount for authentication, a separate increment must prove the guest's runtime user can write to the mounted directory, adjust private ownership safely, and configure LiteLLM to point its native authenticator there. Do not manually copy host OAuth state or perform device-code authentication before those preconditions are met.

## Read-only host preflight

```sh
incus project show aginctus-gateway
incus --project aginctus-gateway config show litellm-1 --expanded
incus --project default storage volume show default aginctus-gateway-local-credentials
```

The project must show `features.storage.volumes: "true"`. Absence of `aginctus-gateway` or `litellm-1` means the gateway has not been deployed; do not invoke attachment until ordinary gateway provisioning is ready.

## Live attachment inspection (only after deliberate opt-in)

```sh
nix develop -c go run ./cmd/aginctus gateway credential-storage attach
incus --project aginctus-gateway config show litellm-1 --expanded
incus --project aginctus-gateway storage volume show default aginctus-gateway-local-credentials
```

Expected: only `litellm-1` references the project-local volume, not PostgreSQL or any workload. **Do not log in or store credentials yet.** Avoid `incus-compose down --volumes` for managed credential storage. The next increment will test actual writable directory ownership and safely enable `CHATGPT_TOKEN_DIR`.
