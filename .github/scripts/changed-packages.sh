#!/usr/bin/env bash
# Print the packages CI should test, one per line.
#
#   a package tag  refs/tags/<pkg>-vX.Y.Z   -> that package
#   a pull request                          -> packages touched vs the base branch
#   a push to master                        -> packages touched since the previous push
#
# A package is a top-level directory holding an lgx.edn. Every package sits on
# sql/, so a change there - or to the CI files themselves - tests all of them.
#
# Inputs (GitHub Actions' env, or set by hand): GITHUB_REF, GITHUB_EVENT_NAME,
# GITHUB_BASE_REF, BEFORE_SHA. A git diff that fails exits non-zero: an empty
# list from a broken diff would pass CI while testing nothing.
set -euo pipefail

all_packages() {
    for f in */lgx.edn; do
        echo "${f%/lgx.edn}"
    done | sort
}

ref="${GITHUB_REF:-}"
if [[ "$ref" == refs/tags/*-v* ]]; then
    tag="${ref#refs/tags/}"
    pkg="${tag%-v*}"
    if [[ ! -f "$pkg/lgx.edn" ]]; then
        echo "tag $tag names no package (no $pkg/lgx.edn)" >&2
        exit 1
    fi
    echo "tag $tag -> $pkg" >&2
    echo "$pkg"
    exit 0
fi

case "${GITHUB_EVENT_NAME:-}" in
    pull_request)
        base="origin/${GITHUB_BASE_REF:?GITHUB_BASE_REF is unset}"
        echo "pull request -> diff against $base" >&2
        changed="$(git diff --name-only "$base...HEAD")"
        ;;
    *)
        before="${BEFORE_SHA:-}"
        if [[ -z "$before" || "$before" =~ ^0+$ ]] || ! git cat-file -e "$before^{commit}" 2>/dev/null; then
            before="HEAD~1"
        fi
        echo "push -> diff $before..HEAD" >&2
        changed="$(git diff --name-only "$before" HEAD)"
        ;;
esac

selected=()
run_all=""
while IFS= read -r path; do
    [[ -n "$path" ]] || continue
    top="${path%%/*}"
    case "$path" in
        .github/*|.mise.toml) run_all="ci files changed" ;;
    esac
    if [[ "$top" == "sql" ]]; then
        run_all="sql changed"
    elif [[ -f "$top/lgx.edn" ]]; then
        selected+=("$top")
    fi
done <<< "$changed"

if [[ -n "$run_all" ]]; then
    echo "$run_all -> all packages" >&2
    all_packages
elif [[ ${#selected[@]} -gt 0 ]]; then
    printf '%s\n' "${selected[@]}" | sort -u
else
    echo "no package changed" >&2
fi
