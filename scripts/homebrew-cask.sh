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

  depends_on macos: :tahoe
  depends_on formula: "gh"

  app "Bigboard.app"
  binary "#{appdir}/Bigboard.app/Contents/MacOS/Bigboard", target: "bigboard"

  caveats <<~EOS
    Bigboard requires Git for repository history (Homebrew or Xcode Git).
    Install Git if it is missing before opening Bigboard.
    Run `gh auth login` before opening Bigboard; it uses the active GitHub CLI account.
    Run `bigboard` to open the app, or `bigboard help` for terminal commands.
  EOS
end
CASK
