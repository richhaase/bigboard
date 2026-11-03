#!/usr/bin/env bash
set -euo pipefail

release_tag=${1:?Usage: release.sh vX.Y.Z}
if [[ ! "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  printf 'Expected a release tag such as v2.1.1\n' >&2
  exit 1
fi

: "${SIGNING_IDENTITY:?A Developer ID Application signing identity is required}"
: "${DEVELOPMENT_TEAM:?An Apple developer Team ID is required}"
: "${NOTARY_KEY_PATH:?A notarization API key file is required}"
: "${NOTARY_KEY_ID:?A notarization API key ID is required}"
: "${NOTARY_ISSUER:?A notarization API issuer is required}"

repo_root=$(cd "$(dirname "$0")/.." && pwd)
export DEVELOPER_DIR=${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}
release_version=${release_tag#v}
bundle_version=${release_version%%-*}
output_dir="$repo_root/native/build"
archive_path="$output_dir/Bigboard.xcarchive"
export_path="$output_dir/export"
zip_path="$output_dir/Bigboard_${release_version}_darwin_universal.zip"

swift test --package-path "$repo_root/native"

xcodebuild -quiet \
  -project "$repo_root/native/Bigboard.xcodeproj" \
  -scheme "Bigboard App" \
  -configuration Release \
  -destination 'generic/platform=macOS' \
  -derivedDataPath "$output_dir/DerivedData" \
  -archivePath "$archive_path" \
  MARKETING_VERSION="$bundle_version" \
  CURRENT_PROJECT_VERSION="$bundle_version" \
  CODE_SIGN_IDENTITY="$SIGNING_IDENTITY" \
  DEVELOPMENT_TEAM="$DEVELOPMENT_TEAM" \
  BIGBOARD_REQUIRE_AUTH=YES \
  archive

export_options="$output_dir/ExportOptions.plist"
cp "$repo_root/native/Config/ExportOptions.plist" "$export_options"
plutil -insert teamID -string "$DEVELOPMENT_TEAM" "$export_options"
plutil -insert signingCertificate -string "$SIGNING_IDENTITY" "$export_options"

xcodebuild -quiet -exportArchive \
  -archivePath "$archive_path" \
  -exportOptionsPlist "$export_options" \
  -exportPath "$export_path"

app_path="$export_path/Bigboard.app"
lipo "$app_path/Contents/MacOS/Bigboard" -verify_arch arm64
lipo "$app_path/Contents/MacOS/Bigboard" -verify_arch x86_64
codesign --verify --deep --strict "$app_path"

bash "$repo_root/scripts/notarize-macos.sh" "$app_path" "$zip_path"
(cd "$output_dir" && shasum -a 256 "$(basename "$zip_path")" > checksums.txt)

release_flags=(
  --verify-tag
  --generate-notes
  --notes-file "$repo_root/docs/release-notes.md"
  --title "Bigboard $release_tag"
)
if [[ "$release_tag" == *-* ]]; then
  release_flags+=(--prerelease)
fi
gh release create "$release_tag" "$zip_path" "$output_dir/checksums.txt" \
  --repo richhaase/bigboard "${release_flags[@]}"
