#!/usr/bin/env bash
set -euo pipefail

release_version=${1:?Usage: homebrew-cask.sh VERSION ZIP_PATH}
archive_path=${2:?Usage: homebrew-cask.sh VERSION ZIP_PATH}
if [[ ! "$release_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ || ! -f "$archive_path" ]]; then
  printf 'A stable release version and application archive are required\n' >&2
  exit 1
fi
expected_name="Bigboard_${release_version}_darwin_universal.zip"
if [[ "$(basename "$archive_path")" != "$expected_name" ]]; then
  printf 'Unexpected application archive name\n' >&2
  exit 1
fi
archive_sha=$(shasum -a 256 "$archive_path" | awk '{print $1}')
cat <<CASK
cask "bigboard" do
  version "$release_version"
  sha256 "$archive_sha"

  url "https://github.com/richhaase/bigboard/releases/download/v#{version}/Bigboard_#{version}_darwin_universal.zip"
  name "Bigboard"
  desc "Explore work areas, contributors, and commit evidence across GitHub repositories"
  homepage "https://github.com/richhaase/bigboard"

  depends_on formula: "git"
  depends_on macos: :ventura

  app "Bigboard.app"

  caveats <<~EOS
    Open Bigboard and choose Sign in with GitHub to connect your repositories.
  EOS
end
CASK
