#!/usr/bin/env bash
# Checks scan-history.sh on a temporary repo: clean -> exit 0, planted fake key and
# .env/key.properties on another branch (then deleted) -> exit 1, redacted report.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

repo="$tmp/repo"
git init -q "$repo"
git -C "$repo" config user.email t@example.org
git -C "$repo" config user.name t
echo hello > "$repo/a.txt"
echo 'API_KEY=changeme' > "$repo/.env.example"
git -C "$repo" add .
git -C "$repo" commit -qm clean

"$here/scan-history.sh" --repo "$repo" --report "$tmp/clean.md" >/dev/null || fail "clean repo must exit 0"
grep -q "No hits" "$tmp/clean.md" || fail "clean report must say no hits"
grep -Eq '^- Date: [0-9]{4}-[0-9]{2}-[0-9]{2}' "$tmp/clean.md" || fail "report has no date"
grep -q "1 commits" "$tmp/clean.md" || fail "report has no commit count"

# Fake key assembled at run time so this file itself holds no key-like string.
secret="sk-ant-$(printf 'abcdef0123456789%.0s' 1 2)"
git -C "$repo" checkout -qb side
printf 'key = "%s"\n' "$secret" > "$repo/config.txt"
echo 'ACCESS_TOKEN=realvalue1234567890' > "$repo/.env"
echo 'storePassword=x' > "$repo/key.properties"
git -C "$repo" add .
git -C "$repo" commit -qm planted
git -C "$repo" rm -q config.txt .env key.properties
git -C "$repo" commit -qm removed
git -C "$repo" checkout -q master 2>/dev/null || git -C "$repo" checkout -q main

rc=0
"$here/scan-history.sh" --repo "$repo" --report "$tmp/hit.md" >/dev/null || rc=$?
[ "$rc" = 1 ] || fail "planted key must exit 1, got $rc"
planted="$(git -C "$repo" rev-parse --short=10 side~1)"
grep -q "$planted" "$tmp/hit.md" || fail "commit missing in report"
grep -q 'config.txt' "$tmp/hit.md" || fail "path missing in report"
grep -q 'Anthropic API key' "$tmp/hit.md" || fail "pattern missing in report"
grep -q 'file: .env' "$tmp/hit.md" || fail ".env not reported"
grep -q 'key.properties' "$tmp/hit.md" || fail "key.properties not reported"
grep -q 'sk-a' "$tmp/hit.md" || fail "redacted prefix missing"
if grep -q "$secret" "$tmp/hit.md" || grep -q 'realvalue1234567890' "$tmp/hit.md"; then fail "secret leaked into report"; fi
if grep -q 'env.example' "$tmp/hit.md"; then fail ".env.example must not be reported"; fi
echo "scan-history tests passed"
