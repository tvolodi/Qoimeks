#!/usr/bin/env bash
# Updates the checkout to origin/main, rebuilds and restarts the production stack.
# The previous image is kept as qoimeks-prod:rollback-<date>.
set -euo pipefail
cd "$(dirname "$0")"

git fetch --prune origin
git checkout -q main
git pull --ff-only origin main

if docker image inspect qoimeks-prod:latest >/dev/null 2>&1; then
    docker tag qoimeks-prod:latest "qoimeks-prod:rollback-$(date +%Y%m%d)"
fi

docker compose -f docker-compose.prod.yml build
docker compose -f docker-compose.prod.yml up -d --force-recreate app

port=$(grep -E '^HOST_PORT=' .env | cut -d= -f2 || true)
for _ in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:${port:-3113}/healthz" >/dev/null 2>&1; then
        echo "qoimeks-prod is healthy at $(git rev-parse --short HEAD)"
        exit 0
    fi
    sleep 2
done
echo "qoimeks-prod did not become healthy; see: docker logs qoimeks-prod" >&2
exit 1
