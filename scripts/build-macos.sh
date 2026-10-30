#!/usr/bin/env bash
set -euo pipefail

release_version=${1:?Usage: build-macos.sh VERSION}
if [[ ! "$release_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  printf 'Invalid release version\n' >&2
  exit 1
fi
if [[ "$(uname -s)" != Darwin ]]; then
  printf 'The macOS application must be built on macOS\n' >&2
  exit 1
fi

repo_root=$(cd "$(dirname "$0")/.." && pwd)
auth_flags=$(bash "$repo_root/scripts/auth-ldflags.sh" required)
cd "$repo_root/desktop"
GOTOOLCHAIN="go$(awk '$1 == "go" { print $2; exit }' go.mod)" go run github.com/wailsapp/wails/v2/cmd/wails@v2.17.0 build -v 0 -platform darwin/universal -skipbindings -s -m -ldflags "-X main.version=$release_version $auth_flags"
app_path="$repo_root/desktop/build/bin/Bigboard.app"
bundle_version=${release_version%%-*}
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $bundle_version" "$app_path/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $bundle_version" "$app_path/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleGetInfoString Bigboard $release_version" "$app_path/Contents/Info.plist"
codesign --force --sign - "$app_path"
lipo "$app_path/Contents/MacOS/Bigboard" -verify_arch arm64
lipo "$app_path/Contents/MacOS/Bigboard" -verify_arch x86_64
