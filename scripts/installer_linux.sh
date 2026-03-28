#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALLER_SCRIPT="$SCRIPT_DIR/installer_gui.py"

if [ ! -f "$INSTALLER_SCRIPT" ]; then
  printf 'Missing installer payload: %s\n' "$INSTALLER_SCRIPT"
  exit 1
fi

if command -v python3 >/dev/null 2>&1; then
  PYTHON_BIN="python3"
elif command -v python >/dev/null 2>&1; then
  PYTHON_BIN="python"
else
  printf 'Python 3 is required to run the Gopher AI installer.\n'
  exit 1
fi

exec "$PYTHON_BIN" "$INSTALLER_SCRIPT"
