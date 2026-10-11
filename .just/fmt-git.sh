#!/usr/bin/env bash
set -euo pipefail

scope="${1:?scope argument required (empty for all workloads)}"
flag="${2:-}"

case "$flag" in
    "") exit 0 ;;
    --commit|--push) ;;
    *)
        echo "Unknown fmt option: $flag (expected --commit or --push)" >&2
        exit 2
        ;;
esac

case "$scope" in
    "") message="just fmt" ;;
    go|nix) message="just fmt $scope" ;;
    *)
        echo "Unknown fmt workload: $scope" >&2
        exit 2
        ;;
esac

git add .
git commit -m "$message"

if [[ "$flag" == "--push" ]]; then
    git push
fi
