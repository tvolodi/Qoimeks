#!/usr/bin/env bash
# Publishes the download site: template files, then every APK with its checksum, and
# latest.json LAST, so the manifest never points at a missing file. Nothing is ever
# deleted on the target (older APKs stay for rollback).
#
#   scripts/publish-site.sh [--dry-run] [--out DIR] <target>
#
# <target> is an scp destination directory, e.g. user@host:/srv/ai-dala/qoimeks/
# DIR is the release output folder (default: ../release-out, as in release-android.sh, or $RELEASE_OUT).
# $PUBLISH_CP overrides the copy command (default: scp).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="${RELEASE_OUT:-$root/../release-out}"
cp_cmd="${PUBLISH_CP:-scp}"
dry=0
target=""

while [ $# -gt 0 ]; do
    case "$1" in
        --dry-run) dry=1 ;;
        --out) shift; out="${1:?--out needs a folder}" ;;
        -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
        -*) echo "unknown option: $1" >&2; exit 2 ;;
        *) [ -z "$target" ] || { echo "only one target allowed" >&2; exit 2; }; target="$1" ;;
    esac
    shift
done
[ -n "$target" ] || { echo "usage: $0 [--dry-run] [--out DIR] <user@host:/path/>" >&2; exit 2; }
[ -d "$out" ] || { echo "release output folder not found: $out" >&2; exit 1; }
[ -f "$out/latest.json" ] || { echo "no latest.json in $out; run the release script first" >&2; exit 1; }

shopt -s nullglob
apks=("$out"/qoimeks-*.apk)
[ ${#apks[@]} -gt 0 ] || { echo "no qoimeks-*.apk in $out" >&2; exit 1; }
target="${target%/}/"

run() {
    if [ "$dry" = 1 ]; then
        printf '%s' "$cp_cmd"; printf ' %q' "$@"; printf '\n'
    else
        "$cp_cmd" "$@"
    fi
}

for f in index.html site.css site.js; do
    run "$root/deploy/qoimeks-site/$f" "$target"
done
for apk in "${apks[@]}"; do
    run "$apk" "$target"
    for sum in "$apk.sha256" "${apk%.apk}.sha256"; do
        if [ -f "$sum" ]; then run "$sum" "$target"; fi
    done
done
run "$out/latest.json" "$target"
