#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${OUT_DIR:-"$ROOT_DIR/dist/cshared/android"}"
API_LEVEL="${ANDROID_API_LEVEL:-21}"
TAGS="${GO_TAGS:-foss,cmfa,with_low_memory,no_tailscale,no_fake_tcp}"
NDK_DIR="${ANDROID_NDK_HOME:-${NDK_HOME:-}}"
HOST_TAG="${ANDROID_NDK_HOST_TAG:-}"
if [[ -z "$HOST_TAG" ]]; then
  case "$(uname -s)" in
    Darwin) HOST_TAG=darwin-x86_64 ;;
    Linux) HOST_TAG=linux-x86_64 ;;
    *) echo "Set ANDROID_NDK_HOST_TAG for this host" >&2; exit 1 ;;
  esac
fi
STRIP_SO="${STRIP_SO:-1}"
LDFLAGS="${GO_LDFLAGS:--s -w -extldflags=-Wl,-z,max-page-size=16384}"
INSTALL_DIR="${INSTALL_DIR:-}"

if [[ -z "$NDK_DIR" ]]; then
  echo "ANDROID_NDK_HOME or NDK_HOME is required" >&2
  exit 1
fi

TOOLCHAIN="$NDK_DIR/toolchains/llvm/prebuilt/$HOST_TAG/bin"
STRIP_TOOL="${STRIP_TOOL:-"$TOOLCHAIN/llvm-strip"}"

if [[ ! -d "$TOOLCHAIN" ]]; then
  echo "NDK LLVM toolchain not found: $TOOLCHAIN" >&2
  exit 1
fi

if [[ "$STRIP_SO" != "0" && ! -x "$STRIP_TOOL" ]]; then
  echo "llvm-strip not found: $STRIP_TOOL" >&2
  exit 1
fi

build_target() {
  local abi="$1"
  local goarch="$2"
  local cc="$3"
  local goarm="${4:-}"
  local abi_dir="$OUT_DIR/$abi"
  local so_path="$abi_dir/libclash.so"

  mkdir -p "$abi_dir"

  echo "Building $abi"
  (
    cd "$ROOT_DIR"
    export CGO_ENABLED=1
    export GOOS=android
    export GOARCH="$goarch"
    export CC="$TOOLCHAIN/$cc"
    if [[ -n "$goarm" ]]; then
      export GOARM="$goarm"
    else
      unset GOARM || true
    fi

    go build \
      -buildmode=c-shared \
      -trimpath \
      -tags "$TAGS" \
      -ldflags "$LDFLAGS" \
      -o "$so_path" \
      ./cmd
  )

  if [[ "$STRIP_SO" != "0" ]]; then
    local before
    local after

    before="$(wc -c < "$so_path" | tr -d ' ')"
    "$STRIP_TOOL" --strip-unneeded "$so_path"
    after="$(wc -c < "$so_path" | tr -d ' ')"

    echo "Stripped $abi/libclash.so: $before -> $after bytes"
  fi

  if [[ -n "$INSTALL_DIR" ]]; then
    mkdir -p "$INSTALL_DIR/$abi"
    cp "$so_path" "$INSTALL_DIR/$abi/libclash.so"
  fi
}

mkdir -p "$OUT_DIR/include"

build_target "armeabi-v7a" "arm" "armv7a-linux-androideabi${API_LEVEL}-clang" "7"
build_target "arm64-v8a" "arm64" "aarch64-linux-android${API_LEVEL}-clang"
build_target "x86_64" "amd64" "x86_64-linux-android${API_LEVEL}-clang"

first_header="$OUT_DIR/armeabi-v7a/libclash.h"
if [[ -f "$first_header" ]]; then
  cp "$first_header" "$OUT_DIR/include/libclash.h"
fi

echo "Artifacts written to $OUT_DIR"
if [[ -n "$INSTALL_DIR" ]]; then
  echo "Installed shared libraries to $INSTALL_DIR"
fi
