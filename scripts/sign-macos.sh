#!/usr/bin/env bash
set -euo pipefail
umask 077

app_path=${1:?Usage: sign-macos.sh APP_PATH ZIP_PATH}
archive_path=${2:?Usage: sign-macos.sh APP_PATH ZIP_PATH}
if [[ ! -d "$app_path" || "$(uname -s)" != Darwin ]]; then
  printf 'A macOS application bundle is required\n' >&2
  exit 1
fi
for name in QUILL_SIGN_P12 QUILL_SIGN_PASSWORD QUILL_NOTARY_KEY QUILL_NOTARY_KEY_ID QUILL_NOTARY_ISSUER; do
  if [[ -z "${!name:-}" ]]; then
    printf 'Missing release secret: %s\n' "$name" >&2
    exit 1
  fi
done

signing_dir=$(mktemp -d "${TMPDIR:-/tmp}/bigboard-sign.XXXXXX")
keychain_path="$signing_dir/signing.keychain-db"
cleanup() {
  security delete-keychain "$keychain_path" >/dev/null 2>&1 || true
  rm -rf "$signing_dir"
}
trap cleanup EXIT
python3 - "$signing_dir" <<'PY'
import base64
import os
from pathlib import Path
import sys

root = Path(sys.argv[1])
for variable, filename in [("QUILL_SIGN_P12", "certificate.p12"), ("QUILL_NOTARY_KEY", "AuthKey.p8")]:
    value = os.environ[variable]
    try:
        is_file = len(value) < 4096 and "\n" not in value and Path(value).is_file()
    except OSError:
        is_file = False
    if is_file:
        data = Path(value).read_bytes()
    elif value.startswith("-----BEGIN "):
        data = value.encode()
    else:
        data = base64.b64decode("".join(value.split()), validate=True)
    (root / filename).write_bytes(data)
PY
keychain_password=$(openssl rand -hex 32)
security create-keychain -p "$keychain_password" "$keychain_path"
security set-keychain-settings -lut 21600 "$keychain_path"
security unlock-keychain -p "$keychain_password" "$keychain_path"
security import "$signing_dir/certificate.p12" -k "$keychain_path" -P "$QUILL_SIGN_PASSWORD" -T /usr/bin/codesign >/dev/null
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$keychain_password" "$keychain_path" >/dev/null
identity=$(security find-identity -v -p codesigning "$keychain_path" | awk '/"Developer ID Application:/ { print $2 }')
if [[ ! "$identity" =~ ^[[:xdigit:]]{40}$ ]]; then
  printf 'Expected exactly one valid Developer ID Application identity\n' >&2
  exit 1
fi
codesign --force --options runtime --timestamp --keychain "$keychain_path" --sign "$identity" "$app_path"
codesign --verify --deep --strict --verbose=2 "$app_path"
ditto -c -k --keepParent "$app_path" "$signing_dir/notarize.zip"
xcrun notarytool submit "$signing_dir/notarize.zip" --key "$signing_dir/AuthKey.p8" --key-id "$QUILL_NOTARY_KEY_ID" --issuer "$QUILL_NOTARY_ISSUER" --wait --timeout 20m --output-format json > "$signing_dir/notarization.json"
python3 - "$signing_dir/notarization.json" <<'PY'
import json
from pathlib import Path
import sys

result = json.loads(Path(sys.argv[1]).read_text())
if result.get("status") != "Accepted":
    raise SystemExit("Apple did not accept the notarization submission")
PY
xcrun stapler staple "$app_path"
xcrun stapler validate "$app_path"
spctl --assess --type execute --verbose=2 "$app_path"
mkdir -p "$(dirname "$archive_path")"
rm -f "$archive_path"
ditto -c -k --keepParent "$app_path" "$archive_path"
