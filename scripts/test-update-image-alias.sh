#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"
cat >"$tmp/bin/incus" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
: "${MOCK_DIR:?}"
case "$1 $2 $3" in
  "image import "*)
    if [[ "${FAIL_IMPORT:-}" == "1" ]]; then exit 1; fi
    alias="${@: -1}"
    printf '%s\n' "new-image" >"$MOCK_DIR/${alias}"
    ;;
  "image alias list")
    for path in "$MOCK_DIR"/*; do
      [[ -f "$path" ]] && basename "$path"
    done
    ;;
  "image alias rename")
    if [[ "${FAIL_PROMOTION:-}" == "1" && "$5" == "working" && "$4" == *-staged-* ]]; then exit 1; fi
    mv "$MOCK_DIR/$4" "$MOCK_DIR/$5"
    ;;
  "image alias delete")
    rm -f "$MOCK_DIR/$4"
    ;;
  *) echo "unexpected invocation: $*" >&2; exit 2 ;;
esac
MOCK
chmod +x "$tmp/bin/incus"
export PATH="$tmp/bin:$PATH" MOCK_DIR="$tmp"
printf 'original-image\n' >"$tmp/working"

if FAIL_IMPORT=1 bash "$root/scripts/update-image-alias.sh" working metadata rootfs 2>/dev/null; then
  echo "failed import unexpectedly succeeded" >&2; exit 1
fi
[[ "$(cat "$tmp/working")" == original-image ]] || { echo "failed import lost active alias" >&2; exit 1; }

if FAIL_PROMOTION=1 bash "$root/scripts/update-image-alias.sh" working metadata rootfs 2>/dev/null; then
  echo "failed promotion unexpectedly succeeded" >&2; exit 1
fi
[[ "$(cat "$tmp/working")" == original-image ]] || { echo "rollback lost original alias" >&2; exit 1; }

bash "$root/scripts/update-image-alias.sh" working metadata rootfs
[[ "$(cat "$tmp/working")" == new-image ]] || { echo "success did not replace alias" >&2; exit 1; }
echo "image alias failure/success scenarios passed"
