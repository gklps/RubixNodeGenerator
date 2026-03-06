#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────
#  build.sh — Build rubix-setup for the current platform
#
#  Usage:
#    ./build.sh              # builds ./rubix-setup
#    ./build.sh test/        # builds into test/rubix-setup
# ─────────────────────────────────────────────────────────────

set -e

BINARY_NAME="rubix-setup"
OUTPUT_DIR="${1:-.}"          # first arg = output dir, default = current dir

# Windows binary needs .exe
if [[ "$OSTYPE" == "msys"* || "$OSTYPE" == "cygwin"* || "$OSTYPE" == "win"* ]]; then
  BINARY_NAME="rubix-setup.exe"
fi

OUTPUT_PATH="${OUTPUT_DIR%/}/${BINARY_NAME}"

echo "======================================"
echo "  Building rubix-setup"
echo "======================================"
echo "  Output : ${OUTPUT_PATH}"
echo "  OS     : $(uname -s)/$(uname -m)"
echo "======================================"

# rubix-setup itself is pure Go — no CGO needed.
# (CGO is only required when rubix-setup later builds rubixgoplatform.)
CGO_ENABLED=0 go build -o "${OUTPUT_PATH}" .

echo ""
echo "[OK] Build complete: ${OUTPUT_PATH}"
echo ""
echo "Run with:"
echo "  ./${OUTPUT_PATH%/*}/rubix-setup          # interactive"
echo "  ./${OUTPUT_PATH%/*}/rubix-setup --auto   # non-interactive (all defaults)"
echo "  ./${OUTPUT_PATH%/*}/rubix-setup --help   # show all flags"
echo "  ./${OUTPUT_PATH%/*}/rubix-setup status   # show running nodes"
