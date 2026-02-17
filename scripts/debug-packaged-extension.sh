#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"

LOCAL_ONLY_FLAG=""
if [ "${1:-}" = "--local-only" ]; then
  LOCAL_ONLY_FLAG="--local-only"
fi

if [ -n "$LOCAL_ONLY_FLAG" ]; then
  "$SCRIPT_DIR/install-packages.sh" reinstall "$LOCAL_ONLY_FLAG"
else
  "$SCRIPT_DIR/install-packages.sh" reinstall
fi

"$SCRIPT_DIR/spawn-wayland-packaged.sh"
