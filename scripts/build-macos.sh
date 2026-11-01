#!/usr/bin/env bash
set -euo pipefail
release_version=${1:?Usage: build-macos.sh VERSION}
if [[ ! "$release_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then printf 'Invalid release version\n' >&2; exit 1; fi
repo_root=$(cd "$(dirname "$0")/.." && pwd)
CONFIGURATION=release UNIVERSAL=1 REQUIRE_AUTH=1 bash "$repo_root/scripts/build-swift.sh" "$release_version"
architectures=$(lipo -archs "$repo_root/native/build/Bigboard.app/Contents/MacOS/Bigboard")
[[ " $architectures " == *" arm64 "* && " $architectures " == *" x86_64 "* ]]
