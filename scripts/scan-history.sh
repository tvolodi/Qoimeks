#!/usr/bin/env bash
# Scans every commit of all branches (and tags) for files that must not be committed
# (.env, key.properties, keystores, private keys) and for strings that look like API
# keys or tokens. Writes a Markdown report; secrets are redacted (first 4 characters only).
# Changes nothing in the repository. Exit code: 1 when there is a hit, 0 when clean.
#
#   scripts/scan-history.sh [--repo DIR] [--report FILE]
#
# DIR is the repository to scan (default: this one); FILE the report
# (default: docs/history-scan-report.md in that repository).
# Only added lines are examined; merge commits add nothing new. Template files
# (.env.example, .env.sample, .env.template) are not reported as .env files.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
report=""

while [ $# -gt 0 ]; do
    case "$1" in
        --repo) shift; repo="$(cd "${1:?--repo needs a folder}" && pwd)" ;;
        --report) shift; report="${1:?--report needs a file}" ;;
        -h|--help) sed -n '2,11p' "$0"; exit 0 ;;
        *) echo "unknown option: $1" >&2; exit 2 ;;
    esac
    shift
done
[ -n "$report" ] || report="$repo/docs/history-scan-report.md"
git -C "$repo" rev-parse --git-dir >/dev/null 2>&1 || { echo "not a git repository: $repo" >&2; exit 1; }

refs="$(git -C "$repo" for-each-ref --format='%(refname:short)' refs/heads refs/remotes refs/tags | grep -v '/HEAD$' | tr '\n' ' ')"
commits="$(git -C "$repo" rev-list --all | wc -l | tr -d ' ')"
nrefs="$(echo "$refs" | wc -w | tr -d ' ')"

hits="$(mktemp)"
trap 'rm -f "$hits"' EXIT

# Output per hit: <commit>\t<path>\t<pattern>\t<redacted sample>
git -C "$repo" log --all -p -U0 --no-color --no-ext-diff --format='@@C %H' | awk '
function redact(s) { return substr(s, 1, 4) "...[redacted " length(s) " chars]" }
function hit(pat, sample) { printf "%s\t%s\t%s\t%s\n", commit, path, pat, sample }
function check(name, s, re, label) {
    if (match(s, re)) hit(label, redact(substr(s, RSTART, RLENGTH)))
}
/^@@C / { commit = $2; next }
/^diff --git / {
    path = $0; sub(/^diff --git a\/.* b\//, "", path)
    base = path; sub(/.*\//, "", base)
    if (base ~ /^\.env(\..*)?$/ && base !~ /\.(example|sample|template|dist)$/) hit("file: .env", "-")
    if (base == "key.properties") hit("file: key.properties", "-")
    if (base ~ /\.(jks|keystore|p12|pfx|pem|p8)$/) hit("file: keystore/key", "-")
    if (base ~ /^id_(rsa|dsa|ecdsa|ed25519)$/) hit("file: ssh private key", "-")
    next
}
/^\+\+\+ / { next }
/^\+/ {
    line = substr($0, 2)
    if (line ~ /-----BEGIN [A-Z ]*PRIVATE KEY-----/) hit("private key block", "-----BEGIN")
    check("aws", line, "AKIA[0-9A-Z]{16}", "AWS access key id")
    check("anthropic", line, "sk-ant-[A-Za-z0-9_-]{20,}", "Anthropic API key")
    check("openai", line, "sk-[A-Za-z0-9_-]{20,}", "sk- style API key")
    check("github", line, "gh[pousr]_[A-Za-z0-9]{30,}", "GitHub token")
    check("slack", line, "xox[baprs]-[A-Za-z0-9-]{10,}", "Slack token")
    check("google", line, "AIza[0-9A-Za-z_-]{35}", "Google API key")
    check("jwt", line, "eyJ[A-Za-z0-9_-]{10,}\\.eyJ[A-Za-z0-9_-]{10,}\\.[A-Za-z0-9_-]{10,}", "JWT")
    if (match(line, /(api[_-]?key|secret|token|passw(or)?d|access[_-]?code)[A-Za-z_]*["'\'']?[ \t]*[=:][ \t]*["'\'']?[A-Za-z0-9_+\/=.-]{16,}/)) {
        m = substr(line, RSTART, RLENGTH)
        v = m; sub(/^[^=:]*[=:][ \t]*["'\'']?/, "", v)
        if (tolower(v) !~ /example|changeme|change-me|your|xxx|placeholder|dummy|sample|\$\{|\$\(|test/)
            hit("secret-like assignment", redact(v))
    }
}
' | sort -u > "$hits"

total="$(wc -l < "$hits" | tr -d ' ')"
mkdir -p "$(dirname "$report")"
{
    echo "# Git history scan report"
    echo
    echo "- Date: $(date -u +%Y-%m-%d)"
    echo "- Scanned: every commit reachable from all refs (\`git log --all\`): $commits commits, $nrefs refs"
    echo "- Refs: $refs"
    echo "- Examined: file names (.env, key.properties, keystores, private keys) and added lines (API key and token patterns)"
    echo "- Hits: $total"
    echo "- Secrets are redacted: only the first 4 characters and the length are shown."
    echo "- Nothing in the history was changed. The product owner decides what to do about any hit."
    echo "- Reproduce: \`scripts/scan-history.sh\` (exit code 1 when there is a hit)."
    echo
    if [ "$total" = 0 ]; then
        echo "No hits."
    else
        echo "| Commit | Path | Pattern | Sample |"
        echo "|---|---|---|---|"
        while IFS=$'\t' read -r c p pat s; do
            echo "| \`${c:0:10}\` | \`$p\` | $pat | \`$s\` |"
        done < "$hits"
    fi
} > "$report"

echo "scanned $commits commits, $total hit(s); report: $report"
[ "$total" = 0 ]
