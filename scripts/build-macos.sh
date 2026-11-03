#!/usr/bin/env bash
set -euo pipefail

configuration=${1:-Debug}
case "$configuration" in
  Debug|Development|Release) ;;
  *)
    printf 'Usage: build-macos.sh [Debug|Development|Release]\n' >&2
    exit 1
    ;;
esac

repo_root=$(cd "$(dirname "$0")/.." && pwd)
export DEVELOPER_DIR=${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}

xcodebuild -quiet \
  -project "$repo_root/Bigboard.xcodeproj" \
  -scheme "Bigboard App" \
  -configuration "$configuration" \
  -derivedDataPath "$repo_root/build/DerivedData" \
  -destination 'generic/platform=macOS' \
  CONFIGURATION_BUILD_DIR="$repo_root/build" \
  build
