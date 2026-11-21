# Nix source ownership and flake boundaries

Status: implemented; final validation and closeout pending (tracking: [#879](https://github.com/blevinn/aginctus/issues/879)).

## Principle

Own Nix expressions by the feature or operational boundary they implement, not by their file extension. Keep `flake.nix` as the stable composition/export interface, rather than the place where package/image implementations accumulate. Avoid catch-all directories and extracted functions that obscure rather than clarify.

## Production expressions

Keep shared production Nix implementations under `nix/`, with cohesive groups;
product-specific image specifications belong to their plugin owner:

- `nix/modules/`: the public NixOS and Home Manager modules, together with shared settings schema, MCP settings rendering, runtime-secret guards, and firewall module logic.
- `nix/packages/`: independently useful package derivations (CLI, gateway and state tools). Share build settings only where doing so clearly prevents drift.
- `plugins/herdr/images/`: the managed Herdr client guest specification and Incus image assembly. The flake injects shared command wrappers, SSH helpers and state bindings explicitly; the socket VM reuses the production client module.
- `nix/workloads/`: reusable guest support and the OpenCode and isolated OAuth gateway workload image definitions. Keep the scoped OpenCode patch with its consumer. Image construction is invoked from the flake, not implemented inline there.

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

## Final ownership map and closeout evidence

The current owner paths are:

| Concern | Canonical location |
| --- | --- |
| Product NixOS/Home Manager modules and shared settings | `nix/modules/` |
| Shared Go executable derivations | `nix/packages/go.nix` |
| OpenCode and isolated OAuth image definitions | `nix/workloads/` |
| Herdr client image | `plugins/herdr/images/client.nix` |
| General module/parity checks and the Herdr socket integration scenario | `tests/nix/` |
| Firewall VM integration cases | `tests/nix/firewall/` |
| VM scenarios and shared host/guest/OCI staging fixtures | `tests/nix/vm-harness/` |
| Testenv worker, recovery and disposable Incus image definitions | `internal/testing/testenv/nix/` |

The shared `aginctus-host.nix` fixture is imported by both the NixOS
VM test driver and the disposable Incus VM image; neither should have a
separate fork. Public flake names and the separation of `packages`, routine
`checks`, and opt-in `vmTests` remain the API boundary.

An offline regression guard in `tests/test_nix_source_ownership.py` checks
tracked ownership locations, local file imports from `flake.nix`, key public
output names, and the exclusion of VM harness outputs from routine checks.
It runs in the standard Python suite (`python3 -m unittest discover -s tests
-p 'test_*.py'`). This is intentionally **not** a replacement for Nix evaluation.

For final validation on a suitable NixOS development host, run:

```sh
python3 -m unittest discover -s tests -p 'test_nix_source_ownership.py' -v
nix flake check --no-update-lock-file
nix build --no-link .#aginctus
nix build --no-link .#opencode-runtime
nix build --no-link .#opencode-workload
nix build --no-link .#oauth-gateway
nix build --no-link .#testenv-incus-vm-image
nix build --no-link .#vmTests.x86_64-linux.accepted-state-incus
```

The last two builds are explicit, potentially heavyweight x86_64-linux
operator validations and are **not** added to the default PR gate.
Complete a public output-name/platform comparison against the pre-refactor
baseline before closing #879. This record does not assert that unrun KVM or
live guest validations have passed.
