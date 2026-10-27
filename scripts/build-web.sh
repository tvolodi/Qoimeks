#!/usr/bin/env bash
# Builds the web app (the installable web app for iPhone, docs/ios-study.md option A).
# --no-web-resources-cdn serves CanvasKit from the app itself, not from gstatic.com.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
mobile="${MOBILE_DIR:-$root/mobile}"

cd "$mobile"
flutter build web --release --no-web-resources-cdn
echo "Web build: $mobile/build/web"
