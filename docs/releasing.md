# Release process

This is the **release operator runbook**, not proof that any Aginctus version is ready.
The v0.1 acceptance checklist is [#118](https://github.com/blevinn/aginctus/issues/118).
Its five functional criteria have live pilot evidence. On 2026-10-31, the
owner explicitly deferred the **unrun** fresh-host deployment/smoke/cleanup/repeat
to [v0.2 #580](https://github.com/blevinn/aginctus/issues/580), rather than
asserting it passed. The [deployment and acceptance handoff](v0.1-deployment-acceptance.md)
distinguishes this limitation from the verified live pilot and accepted residual risks.
Merging release tooling does not pass acceptance or authorize publication.

## Version and Git standards

- Use SemVer for public releases: `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-rc.N` (positive `N`).
  Tags always include `v`; the on-disk version omits it. No build metadata in tags.
- `internal/cli/VERSION` is the **single version source** for the Go CLI and Nix
  packages. Keep `0.0.0-dev` on development `main` until an approved release
  preparation PR deliberately sets the release version. Go's `--version`
  reports that version plus revision (not a build timestamp).
- `main` is the integration line. Normal work is through topic PR branches;
  use `chore/release-vX.Y.Z` to prepare a version. Optional
  `release/vMAJOR.MINOR` stabilization branches require explicit agreement,
  backport reviews, and an ancestor check before tagging; by default tag only
  commits already merged into `main`.
- Release tags are **annotated**, preferably signed (`git tag -s`).
  Never move, reuse, delete or force-push a published tag; a correction is a
  subsequent version. Configure GitHub tag-protection rulesets for `v*`
  where available: restrict creation to authorized release operators and
  prohibit deletion/update. Workflow checks cannot substitute for those
  administrative controls.
- Keep `CHANGELOG.md` sections `## [X.Y.Z]` and reviewed
  `docs/releases/vX.Y.Z.md` notes in the release PR. Use
  [release note template](releases/TEMPLATE.md). The release workflow requires
  all three to agree for a tagged version.
- Do not include private pilot addresses, keys, tokens or credentials in notes,
  command output, CI logs or release assets.

## 1. Prepare and approve the release PR

1. Confirm the target milestone and the release roll-up issue's acceptance
   checklist. The human owner must sign off any live pilot/inference evidence
   and deferred risks. See [development validation tiers](development.md#validation-tiers).
2. From current `main`, open `chore/release-vX.Y.Z`. Set
   `internal/cli/VERSION` to `X.Y.Z` (or `X.Y.Z-rc.N`); promote reviewed
   release notes from the template to `docs/releases/vX.Y.Z.md`;
   move shipped entries from the changelog's Unreleased section into
   `## [X.Y.Z]`. Do not claim features merely because PRs merged.
3. Run the hermetic tier and the preview guard on the release PR checkout:

   ```sh
   nix --quiet develop --no-update-lock-file -c just check-all
   nix --quiet build --no-update-lock-file --no-link .#aginctus
   python3 scripts/release/check.py --tag vX.Y.Z
   git diff --check origin/main...HEAD
   ```

4. Record passing CI links, `just test-nix-firewall` VM results on a suitable
   x86_64-linux host, and any disposable Incus acceptance. Operator-only live
   acceptance is explicitly authorized separately; it is **not** run by
   release automation. Include outcomes and deferrals in notes, and get
   human sign-off before merging the release PR to `main`.

## 2. Tag an approved commit on main

Do this **only after** the release PR is merged and all release gates pass.
The release automation never creates or moves tags. Example for `v0.1.0`:

```sh
git fetch origin --tags
git switch main
git pull --ff-only
python3 scripts/release/check.py --tag v0.1.0
# If configured with signing, prefer: git tag -s v0.1.0 -m "Aginctus v0.1.0"
git tag -a v0.1.0 -m "Aginctus v0.1.0"
git push origin refs/tags/v0.1.0
```

The ordinary `hermetic` CI workflow also builds and verifies the Linux amd64
archive with `scripts/release/package.sh` using the current source version
(`0.0.0-dev` on the development branch). This is a **non-publishing**
preview: it checks the CLI's embedded version and exact Git revision, archive
contents, and SHA256 checksum without creating a tag or GitHub release.
The tagged workflow invokes the same packaging script after its release
collateral and hermetic validation gate.

The `release` GitHub Actions workflow checks that the **annotated** tag
points to a commit reachable from `origin/main`, that the version/changelog/
notes match, and that pinned hermetic tests, gateway parity and CLI packaging
pass. It creates a **draft** GitHub Release and attaches a Linux amd64 archive
and `SHA256SUMS`. A draft is not a published release. The uploaded Linux
binary is a standalone Go CLI and expects supported host/Incus services;
the Nix package is the preferred NixOS distribution path. ARM release binaries
and signed/SLSA provenance are not provided yet.

## 3. Inspect and publish manually

Review the successful workflow logs, tagged commit, generated draft notes and
assets, and check artifact integrity:

```sh
gh release view v0.1.0 --json isDraft,tagName,assets
gh release download v0.1.0 --pattern 'aginctus-*' --pattern SHA256SUMS
sha256sum --check SHA256SUMS
```

Confirm that the acceptance issue and GitHub milestone reflect the evidence
and that release approval is recorded. An authorized human then publishes
the existing draft in GitHub's Releases UI (or runs
`gh release edit v0.1.0 --draft=false`). Do not enable automatic publication
or mark a release approved solely because the workflow was green.

Nix consumers should pin `github:blevinn/aginctus/vX.Y.Z` (or a commit SHA)
and update their `flake.lock` intentionally; do not silently update pilot
hosts. Confirm both CLI version and pinned revision after deployment. Retain
the old pin to allow source rollback; runtime state migrations require an
independent rollback plan.

After tagging, promptly open a normal PR to advance `internal/cli/VERSION`
on `main` to the next **development** version (for example `0.2.0-dev`),
and leave a fresh `[Unreleased]` changelog section. This is a separate,
reviewed commit, never an edit to the release tag. Until it merges, builds
of current `main` will still identify as the just-released version;
consumers should pin immutable tags or commit SHAs instead.

## Failures and corrections

The workflow rejects a mismatch, a lightweight/off-main tag, a failed
validation, or an existing GitHub Release for the same tag. It does not repair
or overwrite releases. Inspect the failure, fix on `main` through a new PR,
and issue a **new** version/tag (e.g. next RC or patch); never rewrite a
pushed tag. If publication has not occurred, leave the draft unpublished
while investigating. If a release is found unsafe after publication,
document impact, notify consumers, and publish a corrected version;
never replace previously distributed bits in place.

## Automation boundary

The workflow can attest only to its executed checks and the tagged source.
It does **not** validate policy/host readiness, upstream inference, sandboxing
on a real pilot host, artifact signatures, or security audit. Release approval
and pilot evidence remain human-controlled. No CI release secrets or pilot
credentials are required.
