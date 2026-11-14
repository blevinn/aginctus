# aginctus-oauth-gateway

Internal OAuth gateway service with a loopback-only HTTP liveness endpoint and a private credential-state mount. This is a service executable, not an interactive login CLI.

## Usage

```sh
go run ./cmd/aginctus-oauth-gateway [--listen 127.0.0.1:4081] [--state-dir /absolute/private/credential-volume]
```

`--listen` defaults to `127.0.0.1:4081`; the listener must be loopback. `--state-dir` defaults to the service's compiled credential-state path and must already be provisioned as a private volume. The process refuses an invalid state directory rather than creating a writable fallback. It shuts down on SIGINT/SIGTERM. See [OAuth gateway architecture](../../docs/architecture/native-chatgpt-oauth.md).
