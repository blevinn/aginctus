# Repository ownership and layout

The source tree is organized by responsibility, not by file format or language.
See [the development guide](README.md) for the canonical Just and Nix entry points.
This is a structure convention, not a new plugin or authorization contract.

| Root | Ownership |
| --- | --- |
| `cmd/` | Go executable entry points, with behavior delegated to packages |
| `pkg/` | Deliberately public Go APIs and contracts, not an overflow directory |
| `internal/` | Private Go implementation and shared infrastructure |
| `plugins/` | Concrete integrations, including their runtime scripts, embeddings, and tests |
| `nix/` | Nix modules, derivations, images, and Nix-owned packaged helpers |
| `tests/` | Cross-package validation, fixtures, VM harnesses, acceptance and evidence readers |
| `tools/` | Maintainer and operator tooling, not production payloads |
| `docs/` | Current documentation, design records, and explicitly labeled historical records |
| `.just/`, `justfile` | The one developer-facing task interface and its modules |

Place resources and language-specific files next to the component that owns
their lifecycle. In particular, production plugins own their embedded Python,
JavaScript, YAML and other runtime inputs; Nix may reference them for packaging
without taking ownership of their behavior. Keep package-local Go tests beside
their Go source and cross-component tests under `tests/`.

Do **not** reintroduce root-level `deploy/`, `guest/`, `scripts/`, `assets/`,
or singular `test/` catch-alls. Do not introduce a root `Makefile` alongside
Just. The tracked-source guard (`just check layout`) checks precisely these
obsolete top-level paths; it does not ban legitimate nested names such as
`nix/guest/` or historical prose discussing removed paths.

`tools/` is not a new `scripts/`: put test-only helpers in `tests/`,
production behavior with its domain owner, and operator commands under a
specific tools subdirectory. Just orchestrates developer tasks; Nix supplies
packaging and reproducible environments. Avoid parallel task interfaces.

When relocating files, update their Nix paths, Go imports, Python imports,
test discovery, documentation, and embedded-resource paths together. Run
appropriate targeted tests and security-negative cases; a directory rename is
not evidence of preserved behavior. The structural guard protects ownership
from regressing, but does not replace package or authorization tests.
