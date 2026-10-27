#!/usr/bin/env bash
# Builds the web app (the installable web app for iPhone, docs/ios-study.md option A).
# All arguments go to `flutter build web`, e.g. scripts/build-web.sh --base-href /web/
# --no-web-resources-cdn serves CanvasKit from the app itself, not from gstatic.com.
# Afterwards main.dart.js gets a ?v=<build id> in flutter_bootstrap.js (tool/cache_bust.dart),
# because Cloudflare lets browsers reuse .js files for hours.
set -euo pipefail

# Git Bash on Windows would rewrite an argument like /web/ into a Windows path.
export MSYS_NO_PATHCONV=1

root="$(cd "$(dirname "$0")/.." && pwd)"
mobile="${MOBILE_DIR:-$root/mobile}"

cd "$mobile"
flutter build web --release --no-web-resources-cdn "$@"
id="$(dart run tool/cache_bust.dart build/web | tail -n 1)"
echo "Build id: $id"
echo "Web build: $mobile/build/web"
