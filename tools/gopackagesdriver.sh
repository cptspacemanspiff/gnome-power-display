#!/bin/bash
set -euo pipefail

# pick any dedicated VS Code output base
OUTPUT_BASE="${OUTPUT_BASE:-$PWD/.bazel_out_vscode}"

LOGFILE="${GOPACKAGESDRIVER_LOG:-/tmp/gopackagesdriver.log}"
echo "[$(date '+%T')] gopackagesdriver invoked: $*" >> "$LOGFILE"

exec bazel --output_base="$OUTPUT_BASE" run -- \
  @rules_go//go/tools/gopackagesdriver "$@" \
  2>> "$LOGFILE"
