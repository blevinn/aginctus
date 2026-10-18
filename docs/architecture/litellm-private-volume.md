# LiteLLM private credential volume attachment

Aginctus uses LiteLLM's built-in `chatgpt/` Responses provider. This step prepares an Incus storage boundary **without enabling OAuth login or inference**.

## Important: Incus project ownership

`gateway credential-storage ensure` now creates or verifies a retained, owned custom filesystem volume **inside the configured gateway Compose project**, normally `aginctus-gateway`; it no longer silently provisions the default Incus project. Before any volume mutation, it requires the project to exist with the expected Aginctus Compose ownership markers and `features.storage.volumes=true`. It creates no project or instance, attaches no disk, and imports no credentials.

`gateway credential-storage attach` uses the same project ownership/isolation policy, and may ensure the same project-local volume before attaching it to the owned LiteLLM instance. Prior versions of `ensure` may have created a **legacy default-project** volume with the same logical name. Neither command adopts, copies, migrates, reconfigures nor deletes that legacy volume. It should remain unattached and may require an explicit, separately reviewed operator cleanup after verifying its contents and consumers.

**Do not** express this sensitive volume as a regular incus-compose named external volume: current incus-compose treats missing external volumes as createable and may delete them through volume removal. Aginctus keeps lifecycle ownership independently.

## Command (explicit live Incus mutation)

```sh
aginctus gateway credential-storage ensure
aginctus gateway credential-storage attach
```

`ensure` requires the isolated, owned Compose project but does not require the LiteLLM instance or add a disk device. `attach` requires an existing owned `litellm-1` guest under the configured Compose project, matching `user.aginctus.managed=true`, `user.aginctus.resource=compose-instance` and `user.aginctus.deployment=<project>`. The private volume is created in that project if missing, ownership-checked, and required to have no consumers except the intended LiteLLM instance. The only added disk device is `aginctus_chatgpt_credentials` at `/var/lib/aginctus/chatgpt`. Disk mutation is concurrency-protected with the instance ETag. Repeating the command is idempotent; collisions or foreign ownership fail closed.

This command **does not** change the service environment, set `CHATGPT_TOKEN_DIR`, import tokens, run a login, or activate an inference route. Before using the mount for authentication, a separate increment must prove the guest's runtime user can write to the mounted directory, adjust private ownership safely, and configure LiteLLM to point its native authenticator there. Do not manually copy host OAuth state or perform device-code authentication before those preconditions are met.

## Read-only host preflight

```sh
incus project show aginctus-gateway
incus --project aginctus-gateway config show litellm-1 --expanded
incus --project aginctus-gateway storage volume show default aginctus-gateway-local-credentials
# Optional read-only legacy check (do not remove automatically):
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
