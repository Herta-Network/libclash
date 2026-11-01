#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${OUT_DIR:-"$ROOT_DIR/dist/android"}"
API_LEVEL="${ANDROID_API_LEVEL:-21}"
TAGS="${GO_TAGS:-foss,cmfa,with_low_memory,no_tailscale,no_fake_tcp}"
PACKAGE="${GO_PACKAGE:-github.com/Ember-Moth/libclash}"
JAVA_PKG="${JAVA_PKG:-com.github.embermoth}"
AAR_NAME="${AAR_NAME:-libclash.aar}"
NDK_DIR="${ANDROID_NDK_HOME:-${NDK_HOME:-}}"
STRIP_SO="${STRIP_SO:-1}"
DEFAULT_TARGET="android/arm,android/arm64,android/amd64"
HOST_TAG="${ANDROID_NDK_HOST_TAG:-}"
if [[ -z "$HOST_TAG" ]]; then
  case "$(uname -s)" in
    Darwin) HOST_TAG=darwin-x86_64 ;;
    Linux) HOST_TAG=linux-x86_64 ;;
    *) echo "Set ANDROID_NDK_HOST_TAG for this host" >&2; exit 1 ;;
  esac
fi

if [[ -z "$NDK_DIR" ]]; then
  echo "ANDROID_NDK_HOME or NDK_HOME is required" >&2
  exit 1
fi

if ! command -v gomobile >/dev/null 2>&1; then
  echo "gomobile is required; install golang.org/x/mobile/cmd/gomobile" >&2
  exit 1
fi

if ! command -v zip >/dev/null 2>&1 || ! command -v unzip >/dev/null 2>&1; then
  echo "zip and unzip are required" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
AAR_PATH="$(cd "$OUT_DIR" && pwd)/$AAR_NAME"

strip_aar() {
  local aar="$1"
  local strip_tool="${STRIP_TOOL:-"$NDK_DIR/toolchains/llvm/prebuilt/$HOST_TAG/bin/llvm-strip"}"
  local work_dir

  if [[ ! -x "$strip_tool" ]]; then
    echo "llvm-strip not found: $strip_tool" >&2
    exit 1
  fi

  work_dir="$(mktemp -d)"
  (
    cd "$work_dir"
    unzip -q "$aar"

    while IFS= read -r -d '' so_file; do
      local before
      local after

      before="$(wc -c < "$so_file" | tr -d ' ')"
      "$strip_tool" --strip-unneeded "$so_file"
      after="$(wc -c < "$so_file" | tr -d ' ')"

      echo "Stripped $so_file: $before -> $after bytes"
    done < <(find jni -type f -name '*.so' -print0)

    rm -f "$aar"
    zip -qr "$aar" .
  )
  rm -rf "$work_dir"
}

(
  cd "$ROOT_DIR"
  export ANDROID_NDK_HOME="$NDK_DIR"

  gomobile bind \
    -target "${GOMOBILE_TARGET:-$DEFAULT_TARGET}" \
    -androidapi "$API_LEVEL" \
    -javapkg "$JAVA_PKG" \
    -trimpath \
    -tags "$TAGS" \
    -o "$AAR_PATH" \
    "$PACKAGE"
)

if [[ "$STRIP_SO" != "0" ]]; then
  strip_aar "$AAR_PATH"
fi

echo "Artifact written to $AAR_PATH"
