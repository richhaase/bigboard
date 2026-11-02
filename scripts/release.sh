#!/usr/bin/env bash
set -euo pipefail

release_tag=${1:?Usage: release.sh vX.Y.Z}
if [[ ! "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  printf 'Expected a release tag such as v2.0.1\n' >&2
  exit 1
fi
release_version=${release_tag#v}
repo_root=$(cd "$(dirname "$0")/.." && pwd)
swift test --package-path "$repo_root/native"
bash "$repo_root/scripts/build-macos.sh" "$release_version"
archive_name="Bigboard_${release_version}_darwin_universal.zip"
output_dir="$repo_root/native/build"
mkdir -p "$output_dir"
bash "$repo_root/scripts/sign-macos.sh" "$repo_root/native/build/Bigboard.app" "$output_dir/$archive_name"
(cd "$output_dir" && shasum -a 256 "$archive_name" > checksums.txt)

release_flags=(--verify-tag --generate-notes --notes-file "$repo_root/docs/release-notes.md" --title "Bigboard $release_tag")
if [[ "$release_tag" == *-* ]]; then release_flags+=(--prerelease); fi
gh release create "$release_tag" "$output_dir/$archive_name" "$output_dir/checksums.txt" \
  --repo richhaase/bigboard "${release_flags[@]}"
