#!/usr/bin/env bash
# Convenience wrapper around docker compose for the mneme stack.
# Usage: ./scripts/run-docker.sh [up -d|down|logs -f|build|ps|...]
set -euo pipefail
cd "$(dirname "$0")/.."
exec docker compose "$@"
