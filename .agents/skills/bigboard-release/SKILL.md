---
name: bigboard-release
description: Release richhaase/bigboard through its existing tag-triggered GitHub Actions workflow. Use when asked to publish a new Bigboard version or resume a failed Bigboard release.
---

# Bigboard release

Use the user-provided checkout, or locate the repository root from the current working directory with `git rev-parse --show-toplevel`. Verify that its origin identifies `richhaase/bigboard` (accept SSH or HTTPS remote URLs). If the current repository is unrelated and no checkout was provided, ask for the Bigboard checkout location; do not assume a home directory, username, or filesystem layout. Run repository commands from that verified root. Read applicable repository instructions, `.github/workflows/release.yml`, and `.goreleaser.yaml` before acting; these are authoritative if the process changes.

1. Check branch and working-tree state. Release clean `main`; preserve unrelated work and stop if updating would overwrite it. Run `git pull --ff-only origin main` and `git fetch origin --tags`. Do not infer the latest release from stale local tags.
2. Query GitHub releases and remote tags with `gh`/`git`. Compare the latest stable release commit with updated `main`. If there are no new commits, report that there is nothing new to release. Inspect changes since the latest stable release. Honor a requested version; otherwise choose a patch for fixes or a minor for new features under the current 0.x convention. Announce the version and included changes.
3. Verify CI and Security succeeded for the **exact commit** being released, using workflow run `headSha` and conclusion. Wait for pending runs; resolve or report failures before publishing. Run `goreleaser check` when available. Existing successful CI can supply tests/lint/security validation; follow additional local checks required by repository instructions.
4. Publishing requires a user request to release, not merely inspect the process or create this skill. A release request authorizes the normal tag push and automated tap update; do not add another confirmation unless permissions or a material ambiguity require it. Verify the chosen tag is absent locally and remotely, then create an annotated tag on the verified commit and push only that tag:

   ```sh
   git tag -a <vX.Y.Z> <verified-commit> -m 'Release <vX.Y.Z>'
   git push origin refs/tags/<vX.Y.Z>
   ```

   No version-file bump or manual GitHub release is needed. Tag push triggers GoReleaser to test, build Linux/macOS amd64 and arm64 archives, sign/notarize macOS binaries, publish checksums/assets, and directly update `richhaase/homebrew-tap/Casks/bigboard.rb`. Prereleases skip the stable tap. Do not introduce a tap PR or run a second publishing path.
5. Find the Release workflow run for this tag and commit and monitor through completion, with brief progress updates and spaced checks. On failure, inspect logs and report the concrete failure; do not delete/move a published tag, invent another version, change secrets, or rerun publishing blindly. For a requested retry, first inspect existing assets and tap state to determine what already published.
6. Verify the GitHub release is published with the expected prerelease status, four archives and checksums. For stable releases, verify the remote cask version and archive checksums match the release. Report the release link, version, main change, and workflow/tap outcome. Do not call a pushed tag a completed release.

Use normal permission escalation for network or `.git` writes when required; never bypass a rejected action. Do not print credential values. This skill's creation or validation must not publish a release.
