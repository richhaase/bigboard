#!/usr/bin/env bash
set -euo pipefail
release_tag=${1:?Usage: verify-release.sh TAG ARCHIVE PACKAGE}
archive_path=${2:?Usage: verify-release.sh TAG ARCHIVE PACKAGE}
package_path=${3:?Usage: verify-release.sh TAG ARCHIVE PACKAGE}
verification_dir=$(mktemp -d "${TMPDIR:-/tmp}/bigboard-verify.XXXXXX")
trap 'rm -rf "$verification_dir"' EXIT
ditto -x -k "$archive_path" "$verification_dir"
app_path="$verification_dir/Bigboard.app"
python3 - "$app_path/Contents/Info.plist" "$release_tag" "$package_path" <<'PY'
import plistlib
import re
import sys
from pathlib import Path

with Path(sys.argv[1]).open("rb") as source:
    info = plistlib.load(source)
version = sys.argv[2].removeprefix("v").split("-")[0]
minimum = re.search(r'\.macOS\("([0-9.]+)"\)', Path(sys.argv[3]).read_text())
if minimum is None:
    raise SystemExit("Release source does not declare a macOS deployment target.")
expected = {"CFBundleIdentifier": "com.richhaase.bigboard", "CFBundleShortVersionString": version, "CFBundleVersion": version, "LSMinimumSystemVersion": minimum.group(1)}
if any(info.get(key) != value for key, value in expected.items()):
    raise SystemExit("Release bundle identity, version, or minimum macOS version does not match.")
PY
architectures=$(lipo -archs "$app_path/Contents/MacOS/Bigboard")
if [[ " $architectures " != *" arm64 "* || " $architectures " != *" x86_64 "* ]]; then
  printf 'Release archive must contain both arm64 and x86_64\n' >&2
  exit 1
fi
codesign --verify --deep --strict "$app_path"
codesign --verify -R='anchor apple generic and certificate leaf[field.1.2.840.113635.100.6.1.13] exists' "$app_path"
xcrun stapler validate "$app_path"
spctl --assess --type execute --verbose=2 "$app_path"
printf 'Verified universal signed and notarized release archive for %s\n' "$release_tag"
