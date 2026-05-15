#!/usr/bin/env bash
# Automates Android SDK/NDK, OpenJDK 17, and gomobile setup for WSL/Ubuntu agent APK builds.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

ANDROID_HOME="${ANDROID_HOME:-$HOME/Android/Sdk}"
ANDROID_SDK_ROOT="${ANDROID_SDK_ROOT:-$ANDROID_HOME}"
CMDLINE_TOOLS_ZIP_URL="${CMDLINE_TOOLS_ZIP_URL:-https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip}"
CMDLINE_TOOLS_DIR="$ANDROID_HOME/cmdline-tools/latest"
SDKMANAGER="$CMDLINE_TOOLS_DIR/bin/sdkmanager"
NDK_VERSION="${NDK_VERSION:-26.1.10909125}"
PLATFORM_VERSION="${PLATFORM_VERSION:-android-34}"
BUILD_TOOLS_VERSION="${BUILD_TOOLS_VERSION:-34.0.0}"
JAVA_PKG="${JAVA_PKG:-openjdk-17-jdk}"

log() { printf '[setup_wsl_android] %s\n' "$*"; }

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "This script targets WSL/Ubuntu (Linux). Current OS: $(uname -s)" >&2
  exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
  SUDO="sudo"
else
  SUDO=""
fi

log "Installing base packages (curl, unzip, zip, git, build-essential)..."
$SUDO apt-get update -y
$SUDO apt-get install -y curl unzip zip git build-essential ca-certificates

if ! java -version 2>&1 | grep -q 'version "17'; then
  log "Installing $JAVA_PKG..."
  $SUDO apt-get install -y "$JAVA_PKG"
else
  log "OpenJDK 17 already present."
fi

JAVA_HOME_PATH="$(dirname "$(dirname "$(readlink -f "$(command -v java)")")")"
export JAVA_HOME="$JAVA_HOME_PATH"

mkdir -p "$ANDROID_HOME/cmdline-tools"
if [[ ! -x "$SDKMANAGER" ]]; then
  log "Downloading Android command-line tools..."
  tmpzip="$(mktemp /tmp/cmdline-tools.XXXXXX.zip)"
  curl -fsSL "$CMDLINE_TOOLS_ZIP_URL" -o "$tmpzip"
  tmpdir="$(mktemp -d /tmp/cmdline-tools.XXXXXX)"
  unzip -q "$tmpzip" -d "$tmpdir"
  rm -f "$tmpzip"
  rm -rf "$ANDROID_HOME/cmdline-tools/latest"
  mkdir -p "$ANDROID_HOME/cmdline-tools"
  mv "$tmpdir/cmdline-tools" "$ANDROID_HOME/cmdline-tools/latest"
  rm -rf "$tmpdir"
else
  log "Android command-line tools already installed."
fi

export ANDROID_HOME ANDROID_SDK_ROOT PATH="$CMDLINE_TOOLS_DIR/bin:$ANDROID_HOME/platform-tools:$PATH"

log "Accepting SDK licenses..."
yes | "$SDKMANAGER" --sdk_root="$ANDROID_HOME" --licenses >/dev/null

log "Installing platform, build-tools, and NDK..."
"$SDKMANAGER" --sdk_root="$ANDROID_HOME" \
  "platforms;$PLATFORM_VERSION" \
  "build-tools;$BUILD_TOOLS_VERSION" \
  "ndk;$NDK_VERSION" \
  "platform-tools"

if ! command -v go >/dev/null 2>&1; then
  log "Go not found; install Go 1.22+ and re-run this script for gomobile."
else
  export PATH="$(go env GOPATH)/bin:$PATH"
  if ! command -v gomobile >/dev/null 2>&1; then
    log "Installing gomobile..."
    go install golang.org/x/mobile/cmd/gomobile@latest
  fi
  log "Running gomobile init..."
  gomobile init
fi

if ! command -v gradle >/dev/null 2>&1; then
  log "Installing Gradle (for assembleRelease when wrapper is absent)..."
  $SUDO apt-get install -y gradle || true
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ANDROID_PROJECT="$ROOT/mobile/android"
if [[ -d "$ANDROID_PROJECT" ]] && [[ ! -x "$ANDROID_PROJECT/gradlew" ]]; then
  if command -v gradle >/dev/null 2>&1; then
    log "Generating Gradle wrapper in mobile/android..."
    (cd "$ANDROID_PROJECT" && gradle wrapper --gradle-version 8.7)
  fi
fi

log "Setup complete."
cat <<EOF

Append these lines to your ~/.bashrc (or ~/.profile):

export JAVA_HOME="$JAVA_HOME"
export ANDROID_HOME="$ANDROID_HOME"
export ANDROID_SDK_ROOT="$ANDROID_SDK_ROOT"
export PATH="\$JAVA_HOME/bin:\$ANDROID_HOME/cmdline-tools/latest/bin:\$ANDROID_HOME/platform-tools:\$ANDROID_HOME/build-tools/$BUILD_TOOLS_VERSION:\$(go env GOPATH 2>/dev/null)/bin:\$PATH"

Then run: source ~/.bashrc
Build APK from repo root: make build-agent-android

EOF
