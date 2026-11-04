#!/usr/bin/env bash
# Flutter web helpers.
#   scripts/web-build.sh dev            hot-reload in Chrome on localhost (seconds per change)
#   scripts/web-build.sh bundle [url]   release bundle for the iPhone behind nginx (1-2 min)
# Use `dev` while changing UI and `bundle` only when you want to check on the phone.
set -euo pipefail
cd "$(dirname "$0")/../app"
mode="${1:-bundle}"
case "$mode" in
dev)
  # Talks to the local API directly; press r in the terminal to hot reload.
  exec flutter run -d chrome --dart-define=SEHAT_API=http://localhost:8080 --dart-define=SEHAT_TEST_LOGIN=true
  ;;
bundle)
  API="${2:-https://192.168.1.9:8443}"
  time flutter build web --release --no-wasm-dry-run \
    --dart-define=SEHAT_API="$API" --dart-define=SEHAT_TEST_LOGIN=true
  ;;
*)
  echo "usage: $0 dev | bundle [api-url]" >&2
  exit 2
  ;;
esac
