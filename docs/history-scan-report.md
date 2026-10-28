# Git history scan report

- Date: 2026-10-28
- Scanned: every commit reachable from all refs (`git log --all`): 44 commits, 11 refs
- Refs: main team/T-0029 team/T-0031 team/T-0033 team/T-0034 origin origin/android-release-key origin/import-overwrite-balances origin/main origin/stage1-install-and-update origin/web-pages 
- Examined: file names (.env, key.properties, keystores, private keys) and added lines (API key and token patterns)
- Hits: 3
- Secrets are redacted: only the first 4 characters and the length are shown.
- Nothing in the history was changed. The product owner decides what to do about any hit.
- Reproduce: `scripts/scan-history.sh` (exit code 1 when there is a hit).

| Commit | Path | Pattern | Sample |
|---|---|---|---|
| `3350a8eb61` | `server/internal/providers/providers_test.go` | sk- style API key | `sk-t...[redacted 34 chars]` |
| `3350a8eb61` | `server/internal/store/provider_settings.go` | secret-like assignment | `excl...[redacted 20 chars]` |
| `7d9236763b` | `server/internal/api/web.html` | secret-like assignment | `sess...[redacted 22 chars]` |
