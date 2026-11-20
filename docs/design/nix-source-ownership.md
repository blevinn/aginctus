# Nix source ownership and flake boundaries

Status: accepted for incremental implementation (tracking: [#879](https://github.com/blevinn/aginctus/issues/879)).

## Principle

Own Nix expressions by the feature or operational boundary they implement, not by their file extension. Keep `flake.nix` as the stable composition/export interface, rather than the place where package/image implementations accumulate. Avoid catch-all directories and extracted functions that obscure rather than clarify.

## Production expressions

Keep shared production Nix implementations under `nix/`, with cohesive groups;
product-specific image specifications belong to their plugin owner:

- `nix/modules/`: the public NixOS and Home Manager modules, together with shared settings schema, MCP settings rendering, runtime-secret guards, and firewall module logic.
- `nix/packages/`: independently useful package derivations (CLI, gateway and state tools). Share build settings only where doing so clearly prevents drift.
- `plugins/herdr/images/`: the managed Herdr client guest specification and Incus image assembly. The flake injects shared command wrappers, SSH helpers and state bindings explicitly; the socket VM reuses the production client module.
- `nix/workloads/`: shared workload support (including reusable guest state bindings and command wrappers). The helper relocation in #889 is additive to product image ownership: update injected imports, rather than moving those helpers into the plugin. OpenCode image extraction remains a separate slice; keep its scoped patch with its consumer.

Do not fragment each module into many small expressions merely to achieve a target line count. Keep the production NixOS and Home Manager interfaces separate but preserve their common typed settings and secret-exclusion contract.

## Tests and test environments

Integration tests, including module/parity checks, firewall backend tests, Herdr/OpenCode tests, and VM scenarios, belong to the integration-test domain. Distinguish reusable VM fixtures from individual scenarios; the shared `aginctus-host` definition must remain a single source of truth for NixOS test-driver guests and Incus VM images.

The installed testenv worker, recovery module, testenv VM image, and their validation expressions belong to the testenv implementation rather than the product's production module set. Retain their public flake exports for callers. Their trust, isolation, opt-in and recovery policy are not changed by relocation.

**Top-level coordination:** the separate repository-wide cleanup is reconciling `test/`, `tests/`, `scripts/`, and `guest/`. Do not create a second conflicting top-level layout here; land test and testenv relocations only against the canonical owner directories agreed by that work. In the meantime, prefer leaving files in place over relocating them twice.

## Stable public contract

Preserve the names and shape of existing flake outputs, including:
- `nixosModules` and `homeManagerModules` with their `default` aliases;
- `packages` and platform-specific exports;
- `checks` as safe, routinely evaluated checks;
- explicitly selected heavyweight VM packages; and
- `vmTests` as a distinct namespace for tests whose evaluation may require import-from-derivation.

Do not move heavyweight VM cases into `checks` or accidentally introduce import-from-derivation in routine `nix flake check`. Preserve supported systems and `x86_64-linux` conditions, package/image identities, secrets protections, and operator authorization.

`justfile` and `.just/` remain the developer-facing task entry points. Do not add Makefiles or a competing Nix task runner.

## Implementation sequence

1. Land this decision record and preserve the starting flake surface.
2. Relocate the cohesive production module family, keeping relative imports and references consistent.
3. Extract meaningful package and workload construction from `flake.nix` in a separate reviewable change.
4. Relocate integration checks and VM scenarios, separating fixtures from scenarios.
5. Consolidate testenv worker, recovery and image definitions with the testenv owner, aligned with the broader top-level refactor.

Each slice must be reviewable, have no intentional runtime behavior changes, and update direct imports, CI, docs, Just recipes and scripts impacted by the move.

## Verification

Compare output attribute names, platform conditions and alias targets against the parent branch. Run Nix formatting, parsing, flake evaluation and `nix flake check --no-update-lock-file`, plus selected explicit VM derivation evaluations/builds where the worker supports them. Record any unavailable Nix/KVM validation explicitly; never describe an unrun test as passing. Scan for stale references after every relocation. Preserve the existing security policy and do not claim that a structural move validates VM isolation.
