#!/usr/bin/env bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/.." && pwd)
publisher_config=${BIGBOARD_PUBLISHER_CONFIG:-$repo_root/native/publisher.local.json}
if [[ -f "$publisher_config" && "${BIGBOARD_PUBLISHER_LOADED:-0}" != 1 ]]; then
  export BIGBOARD_PUBLISHER_CONFIG="$publisher_config"
  exec python3 - "$0" "$@" <<'CONFIG'
import json, os, sys
try:
 with open(os.environ["BIGBOARD_PUBLISHER_CONFIG"]) as f: values=json.load(f)
 if not isinstance(values,dict): raise ValueError()
 environment=os.environ.copy()
 for key in ("BIGBOARD_GITHUB_CLIENT_ID","BIGBOARD_GITHUB_CLIENT_SECRET","BIGBOARD_GITHUB_APP_SLUG"):
  value=values.get(key,"")
  if not isinstance(value,str): raise ValueError()
  if key not in environment and value: environment[key]=value
 environment["BIGBOARD_PUBLISHER_LOADED"]="1"
except (OSError,ValueError):
 print("Cannot read publisher settings JSON. Check its path and string fields.",file=sys.stderr)
 sys.exit(1)
os.execve("/bin/bash",["bash",sys.argv[1],*sys.argv[2:]],environment)
CONFIG
fi
export DEVELOPER_DIR=${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}
version=${1:-0.0.0-dev}
configuration=${CONFIGURATION:-debug}
if [[ "${REQUIRE_AUTH:-0}" == 1 ]]; then
  for name in BIGBOARD_GITHUB_CLIENT_ID BIGBOARD_GITHUB_CLIENT_SECRET BIGBOARD_GITHUB_APP_SLUG; do
    if [[ -z "${!name:-}" ]]; then printf 'Missing publisher setting: %s\n' "$name" >&2; exit 1; fi
  done
fi
python3 - <<'CHECK'
import os, re, sys
keys=("BIGBOARD_GITHUB_CLIENT_ID","BIGBOARD_GITHUB_CLIENT_SECRET","BIGBOARD_GITHUB_APP_SLUG")
values=[os.environ.get(k, "") for k in keys]
if any(values):
 for key,value in zip(keys,values):
  valid=bool(re.fullmatch(r"[a-z0-9-]{1,100}" if key.endswith("APP_SLUG") else r"[!-~]{1,4096}",value))
  if not valid:
   print("Missing or malformed publisher setting: " + key, file=sys.stderr)
   sys.exit(1)
CHECK
arch_flags=()
if [[ "${UNIVERSAL:-0}" == 1 ]]; then arch_flags=(--arch arm64 --arch x86_64); fi
swift build --package-path "$repo_root/native" -c "$configuration" "${arch_flags[@]}"
bin_dir=$(swift build --package-path "$repo_root/native" -c "$configuration" "${arch_flags[@]}" --show-bin-path)
app_path="$repo_root/native/build/Bigboard.app"
mkdir -p "$app_path/Contents/MacOS" "$app_path/Contents/Resources"
cp "$bin_dir/Bigboard" "$app_path/Contents/MacOS/Bigboard"
python3 - "$app_path/Contents/Info.plist" "$version" <<'PY'
import os, plistlib, sys
version=sys.argv[2]
info={"CFBundleName":"Bigboard","CFBundleDisplayName":"Bigboard Native","CFBundleIdentifier":"com.richhaase.bigboard","CFBundleExecutable":"Bigboard","CFBundlePackageType":"APPL","CFBundleShortVersionString":version.split('-')[0],"CFBundleVersion":version.split('-')[0],"LSMinimumSystemVersion":"26.0","NSHighResolutionCapable":True,"NSPrincipalClass":"NSApplication","CFBundleIconFile":"AppIcon","CFBundleURLTypes":[{"CFBundleURLName":"com.richhaase.bigboard.oauth","CFBundleURLSchemes":["com.richhaase.bigboard"],"CFBundleTypeRole":"Viewer"}]}
for key in ("BIGBOARD_GITHUB_CLIENT_ID","BIGBOARD_GITHUB_CLIENT_SECRET","BIGBOARD_GITHUB_APP_SLUG"):
 if os.environ.get(key): info[key]=os.environ[key]
with open(sys.argv[1],'wb') as f: plistlib.dump(info,f)
PY
iconset="$repo_root/native/build/AppIcon.iconset"
mkdir -p "$iconset"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$repo_root/native/Assets/AppIcon.png" --out "$iconset/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z "$double" "$double" "$repo_root/native/Assets/AppIcon.png" --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$app_path/Contents/Resources/AppIcon.icns"
codesign --force --sign - "$app_path"
printf 'Built %s\n' "$app_path"
