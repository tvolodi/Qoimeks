#!/usr/bin/env bash
# Checks publish-site.sh with --dry-run: APKs before latest.json, latest.json last,
# no delete command, older APKs included, the target is a parameter.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

for v in 0.1.1 0.1.2; do
    echo apk > "$tmp/qoimeks-$v.apk"
    echo sum > "$tmp/qoimeks-$v.apk.sha256"
done
echo '{}' > "$tmp/latest.json"

fail() { echo "FAIL: $*" >&2; exit 1; }

list="$("$here/publish-site.sh" --dry-run --out "$tmp" user@example.org:/srv/qoimeks/)"
echo "$list"

last="$(echo "$list" | tail -n 1)"
case "$last" in *latest.json*"user@example.org:/srv/qoimeks/") ;; *) fail "latest.json is not the last upload: $last" ;; esac
[ "$(echo "$list" | grep -c latest.json)" = 1 ] || fail "latest.json must appear exactly once"

first_json="$(echo "$list" | grep -n latest.json | cut -d: -f1)"
for v in 0.1.1 0.1.2; do
    n="$(echo "$list" | grep -n "qoimeks-$v.apk " | head -n 1 | cut -d: -f1)"
    [ -n "$n" ] || fail "qoimeks-$v.apk not uploaded"
    [ "$n" -lt "$first_json" ] || fail "qoimeks-$v.apk comes after latest.json"
    echo "$list" | grep -q "qoimeks-$v.apk.sha256" || fail "checksum of $v not uploaded"
done

if echo "$list" | grep -Eiq -- '--delete|(^| )(rm|delete)( |$)'; then fail "delete found"; fi
if grep -Eq -- '--delete|rsync' "$here/publish-site.sh"; then fail "script must not use --delete/rsync"; fi

"$here/publish-site.sh" --dry-run --out "$tmp" >/dev/null 2>&1 && fail "target is required" || true
# Both scripts must default to the same release folder (outside the repo).
rel="$(grep -m1 '^out=' "$here/release-android.sh")"
pub="$(grep -m1 '^out=' "$here/publish-site.sh")"
case "$rel" in *'$root/../release-out'*) ;; *) fail "release-android.sh default folder changed: $rel" ;; esac
case "$pub" in *'$root/../release-out'*) ;; *) fail "publish-site.sh default folder differs: $pub" ;; esac
echo "ok"
