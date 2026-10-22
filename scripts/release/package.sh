#!/usr/bin/env bash
# Build the same release archive in hermetic CI and after an approved tag.
# Never pushes tags, calls GitHub, or publishes any artifacts.
set -euo pipefail

if [[ "$#" -ne 1 || -z "$1" ]]; then
  echo "Usage: scripts/release/package.sh OUTPUT_DIRECTORY" >&2
  exit 2
fi

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

# Do not advertise a clean commit when tracked source differs from HEAD.
git diff --quiet
git diff --cached --quiet
revision="$(git rev-parse --verify HEAD)"
if [[ ! "$revision" =~ ^[0-9a-f]{40}$ ]]; then
  echo "Expected a full Git commit SHA" >&2
  exit 1
fi
version="$(< internal/cli/VERSION)"
if [[ -z "$version" || "$version" == *[[:space:]]* || "$version" == *"/"* ]]; then
  echo "Invalid internal/cli/VERSION" >&2
  exit 1
fi

output_dir="$1"
mkdir -p -- "$output_dir"
archive="aginctus-v${version}-linux-amd64.tar.gz"
if [[ -e "$output_dir/$archive" || -e "$output_dir/SHA256SUMS" ]]; then
  echo "Refusing to overwrite existing release artifacts" >&2
  exit 1
fi

stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
  -ldflags "-buildid= -X=github.com/blevinn/aginctus/internal/cli.Revision=$revision" \
  -o "$stage/aginctus" ./cmd/aginctus
if [[ "$("$stage/aginctus" --version)" != "aginctus $version (revision $revision)" ]]; then
  echo "Packaged CLI version/revision does not match source" >&2
  exit 1
fi

cp LICENSE README.md "$stage/"
tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner \
  -C "$stage" -cf - . | gzip -n > "$output_dir/$archive"
(
  cd -- "$output_dir"
  sha256sum "$archive" > SHA256SUMS
  sha256sum --check SHA256SUMS
  for item in ./aginctus ./LICENSE ./README.md; do
    tar -tzf "$archive" | grep -Fx -- "$item" >/dev/null
  done
)
printf 'Release packaging preview verified: %s (%s)\n' "$archive" "$revision"
