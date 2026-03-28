#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="$ROOT_DIR/build"
GO_CACHE_DIR="${GOCACHE:-/tmp/go-build-cache}"

mkdir -p "$BUILD_DIR/linux" "$BUILD_DIR/windows" "$BUILD_DIR/mobile" "$BUILD_DIR/scripts"

echo "Building Linux binary..."
GOCACHE="$GO_CACHE_DIR" GOOS=linux GOARCH=amd64 \
  go build -o "$BUILD_DIR/linux/gopher-ai" "$ROOT_DIR/cmd/gopher-ai"

echo "Building Windows binary..."
GOCACHE="$GO_CACHE_DIR" GOOS=windows GOARCH=amd64 \
  go build -o "$BUILD_DIR/windows/gopher-ai.exe" "$ROOT_DIR/cmd/gopher-ai"

cp "$ROOT_DIR/ai.ico" "$BUILD_DIR/windows/ai.ico"
cp "$ROOT_DIR/ai.ico" "$BUILD_DIR/linux/ai.ico"
cp "$ROOT_DIR/ai.ico" "$BUILD_DIR/mobile/ai.ico"
cp "$ROOT_DIR/scripts/installer_banner.png" "$BUILD_DIR/linux/installer_banner.png"
cp "$ROOT_DIR/scripts/installer_banner.png" "$BUILD_DIR/windows/installer_banner.png"
cp "$ROOT_DIR/scripts/installer_gui.py" "$BUILD_DIR/linux/installer_gui.py"
cp "$ROOT_DIR/scripts/installer_gui.py" "$BUILD_DIR/windows/installer_gui.py"
cp "$ROOT_DIR/scripts/train_lora.py" "$BUILD_DIR/linux/train_lora.py"
cp "$ROOT_DIR/scripts/train_lora.py" "$BUILD_DIR/windows/train_lora.py"
cp "$ROOT_DIR/scripts/installer_linux.sh" "$BUILD_DIR/linux/Gopher-AI-Installer.sh"
cp "$ROOT_DIR/scripts/installer_windows.ps1" "$BUILD_DIR/windows/Gopher-AI-Installer.ps1"
cp "$ROOT_DIR/scripts/installer_windows.bat" "$BUILD_DIR/windows/Gopher-AI-Installer.bat"
cp "$ROOT_DIR/scripts/build_android.sh" "$BUILD_DIR/mobile/build-android.sh"
cp "$ROOT_DIR/scripts/train_lora.py" "$BUILD_DIR/scripts/train_lora.py"
chmod +x "$BUILD_DIR/mobile/build-android.sh"
chmod +x "$BUILD_DIR/scripts/train_lora.py"
chmod +x "$BUILD_DIR/linux/Gopher-AI-Installer.sh" "$BUILD_DIR/linux/train_lora.py" "$BUILD_DIR/linux/installer_gui.py" "$BUILD_DIR/windows/train_lora.py" "$BUILD_DIR/windows/installer_gui.py"

if command -v 7z >/dev/null 2>&1; then
  rm -f "$BUILD_DIR/linux/Gopher-AI-Linux-Package.7z" "$BUILD_DIR/windows/Gopher-AI-Windows-Package.7z"
  7z a -t7z "$BUILD_DIR/linux/Gopher-AI-Linux-Package.7z" \
    "$BUILD_DIR/linux/gopher-ai" \
    "$BUILD_DIR/linux/ai.ico" \
    "$BUILD_DIR/linux/installer_banner.png" \
    "$BUILD_DIR/linux/installer_gui.py" \
    "$BUILD_DIR/linux/train_lora.py" \
    "$BUILD_DIR/linux/Gopher-AI-Installer.sh" >/dev/null
  7z a -t7z "$BUILD_DIR/windows/Gopher-AI-Windows-Package.7z" \
    "$BUILD_DIR/windows/gopher-ai.exe" \
    "$BUILD_DIR/windows/ai.ico" \
    "$BUILD_DIR/windows/installer_banner.png" \
    "$BUILD_DIR/windows/installer_gui.py" \
    "$BUILD_DIR/windows/train_lora.py" \
    "$BUILD_DIR/windows/Gopher-AI-Installer.ps1" \
    "$BUILD_DIR/windows/Gopher-AI-Installer.bat" >/dev/null
fi

if "$ROOT_DIR/scripts/build_android.sh" >"$BUILD_DIR/mobile/build.log" 2>&1; then
  echo "Android build finished successfully."
else
  printf 'Android build not completed.\nCheck build/mobile/build.log for details.\n' > "$BUILD_DIR/mobile/BUILD_STATUS.txt"
fi

echo "Release artifacts available under $BUILD_DIR"
