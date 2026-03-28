#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="$ROOT_DIR/build/mobile"
ANDROID_DIR="$ROOT_DIR/mobile/android"
SDK_DIR="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-}}"
GRADLE_CMD="${GRADLE_BIN:-}"
SIGNING_DIR="$ROOT_DIR/.signing"
SIGNING_PROPS="$SIGNING_DIR/android-release.properties"
KEYSTORE_PATH="$SIGNING_DIR/gopher-ai-release.jks"
KEY_ALIAS="gopher-ai-release"

random_secret() {
  python3 - <<'PY'
import secrets
print(secrets.token_urlsafe(24))
PY
}

mkdir -p "$BUILD_DIR"

if [ -z "$SDK_DIR" ] && [ -d "$ROOT_DIR/.android-sdk" ]; then
  SDK_DIR="$ROOT_DIR/.android-sdk"
fi

if [ -z "$SDK_DIR" ] && [ -d /usr/lib/android-sdk ]; then
  SDK_DIR="/usr/lib/android-sdk"
fi

if [ -z "$SDK_DIR" ]; then
  echo "Android SDK not found. Set ANDROID_SDK_ROOT or ANDROID_HOME."
  exit 1
fi

export ANDROID_SDK_ROOT="$SDK_DIR"
export ANDROID_HOME="$SDK_DIR"
export GRADLE_USER_HOME="${GRADLE_USER_HOME:-$ROOT_DIR/.gradle-home}"
printf 'sdk.dir=%s\n' "$SDK_DIR" > "$ANDROID_DIR/local.properties"

mkdir -p "$SIGNING_DIR"

if [ ! -f "$SIGNING_PROPS" ]; then
  STORE_PASSWORD="$(random_secret)"
  KEY_PASSWORD="$STORE_PASSWORD"
  cat > "$SIGNING_PROPS" <<EOF
storeFile=$KEYSTORE_PATH
storePassword=$STORE_PASSWORD
keyAlias=$KEY_ALIAS
keyPassword=$KEY_PASSWORD
EOF
  chmod 600 "$SIGNING_PROPS"
fi

STORE_PASSWORD="$(sed -n 's/^storePassword=//p' "$SIGNING_PROPS" | head -n 1)"
KEY_PASSWORD="$STORE_PASSWORD"
python3 - "$SIGNING_PROPS" "$KEYSTORE_PATH" "$STORE_PASSWORD" "$KEY_ALIAS" <<'PY'
from pathlib import Path
import sys

props_path = Path(sys.argv[1])
keystore_path = sys.argv[2]
store_password = sys.argv[3]
key_alias = sys.argv[4]
lines = [
    f"storeFile={keystore_path}",
    f"storePassword={store_password}",
    f"keyAlias={key_alias}",
    f"keyPassword={store_password}",
]
props_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
PY

if [ ! -f "$KEYSTORE_PATH" ]; then
  keytool -genkeypair \
    -alias "$KEY_ALIAS" \
    -keyalg RSA \
    -keysize 4096 \
    -validity 3650 \
    -storetype PKCS12 \
    -keystore "$KEYSTORE_PATH" \
    -storepass "$STORE_PASSWORD" \
    -keypass "$STORE_PASSWORD" \
    -dname "CN=Gopher AI, OU=Gopher AI, O=Gopher AI, L=Lisbon, ST=Lisbon, C=PT"
  chmod 600 "$KEYSTORE_PATH"
fi

if [ -z "$GRADLE_CMD" ] && [ -x "$ROOT_DIR/.gradle-dist/gradle-8.9/bin/gradle" ]; then
  GRADLE_CMD="$ROOT_DIR/.gradle-dist/gradle-8.9/bin/gradle"
fi

if [ -z "$GRADLE_CMD" ] && [ ! -x "$ANDROID_DIR/gradlew" ]; then
  if ! command -v gradle >/dev/null 2>&1; then
    echo "Gradle not found and no Gradle wrapper present."
    exit 1
  fi
  cd "$ANDROID_DIR"
  gradle wrapper --gradle-version 8.9
fi

cd "$ANDROID_DIR"
if [ -n "$GRADLE_CMD" ]; then
  "$GRADLE_CMD" -p "$ANDROID_DIR" --no-daemon assembleDebug assembleRelease
else
  ./gradlew --no-daemon assembleDebug assembleRelease
fi

APK_PATH="$(find "$ANDROID_DIR/app/build/outputs/apk/debug" -name '*.apk' | sort | head -n 1)"
RELEASE_APK_PATH="$(find "$ANDROID_DIR/app/build/outputs/apk/release" -name '*.apk' | sort | head -n 1)"
if [ -z "$APK_PATH" ] || [ -z "$RELEASE_APK_PATH" ]; then
  echo "assembleDebug completed but no APK was produced."
  exit 1
fi

cp "$APK_PATH" "$BUILD_DIR/Gopher-AI-debug.apk"
cp "$RELEASE_APK_PATH" "$BUILD_DIR/Gopher-AI-release.apk"
cp "$ROOT_DIR/ai.ico" "$BUILD_DIR/ai.ico"
printf 'Android build succeeded.\nDebug APK: %s\nRelease APK: %s\nSDK: %s\nSigning: %s\n' \
  "$BUILD_DIR/Gopher-AI-debug.apk" \
  "$BUILD_DIR/Gopher-AI-release.apk" \
  "$SDK_DIR" \
  "$KEYSTORE_PATH" > "$BUILD_DIR/BUILD_STATUS.txt"
