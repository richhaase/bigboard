#!/usr/bin/env bash
set -euo pipefail

app_path=${1:?Usage: notarize-macos.sh APP_PATH ZIP_PATH}
archive_path=${2:?Usage: notarize-macos.sh APP_PATH ZIP_PATH}
: "${NOTARY_KEY_PATH:?A notarization API key file is required}"
: "${NOTARY_KEY_ID:?A notarization API key ID is required}"
: "${NOTARY_ISSUER:?A notarization API issuer is required}"

temporary_dir=$(mktemp -d "${TMPDIR:-/tmp}/bigboard-notarize.XXXXXX")
trap 'rm -rf "$temporary_dir"' EXIT

ditto -c -k --keepParent "$app_path" "$temporary_dir/submission.zip"
xcrun notarytool submit "$temporary_dir/submission.zip" \
  --key "$NOTARY_KEY_PATH" \
  --key-id "$NOTARY_KEY_ID" \
  --issuer "$NOTARY_ISSUER" \
  --wait --timeout 20m \
  --output-format plist > "$temporary_dir/result.plist"

status=$(plutil -extract status raw "$temporary_dir/result.plist")
if [[ "$status" != Accepted ]]; then
  printf 'Apple notarization status: %s\n' "$status" >&2
  exit 1
fi

xcrun stapler staple "$app_path"
xcrun stapler validate "$app_path"
spctl --assess --type execute --verbose=2 "$app_path"

mkdir -p "$(dirname "$archive_path")"
rm -f "$archive_path"
ditto -c -k --keepParent "$app_path" "$archive_path"
