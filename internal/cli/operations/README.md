# CLI application operations

This directory holds typed CLI-specific execution handlers as they are extracted from the flat `internal/cli` package. Subpackages may depend on domain packages and receive explicitly injected side-effect capabilities; they must not depend on the parent CLI package, Cobra, or `commands/*`.

See [`../ARCHITECTURE.md`](../ARCHITECTURE.md) and issue #725.
