---
name: bigboard-release
description: Publish a Bigboard version or resume a failed release through richhaase/bigboard's GitHub Actions Release workflow.
---

# Bigboard release

Use the supplied checkout, or locate it with `git rev-parse --show-toplevel`. Verify that origin identifies `richhaase/bigboard`; ask for the checkout only if it is unrelated. Read applicable repository instructions, `.github/workflows/release.yml`, `scripts/publish-release.py`, and the build/sign/cask scripts before acting. They define the current process.

Publishing requires a user request to release or resume a release. That request authorizes the normal tag push or recovery dispatch and automated tap update; do not add another confirmation without a material ambiguity or permission requirement. Inspecting or editing this skill does not authorize publishing. Keep credentials out of output.

## New release

1. Release clean, updated `main`. Preserve unrelated work; use `git pull --ff-only origin main` and `git fetch origin --tags` without overwriting it.
2. Query GitHub releases and remote tags. Compare the latest stable release with updated `main`; if no new commits exist, report that there is nothing to release. Honor a requested version. Otherwise choose a patch for fixes, a minor for compatible features, or a major for breaking compatibility changes, using the current published version rather than stale local tags. Announce the version and scope.
3. Verify required CI succeeded for the **exact source commit**, using each run's `headSha` and conclusion. Existing successful CI can supply native and packaging test validation. Changes to publishing also require its regression tests: `python3 -B -m unittest discover -s scripts -p '*_test.py'`.
4. Confirm the chosen tag is absent locally and remotely. Create an annotated tag on that verified commit and push only the tag:

   ```sh
   git tag -a <vX.Y.Z> <verified-commit> -m 'Release <vX.Y.Z>'
   git push origin refs/tags/<vX.Y.Z>
   ```

There is no version-file bump. The Release workflow builds a universal `Bigboard.app`, signs with Developer ID, notarizes, staples, and archives it. Its Python publisher uses GitHub CLI to upload `Bigboard_<version>_darwin_universal.zip` and `checksums.txt` to a draft, verify their bytes, and publish. The stable release then directly updates `richhaase/homebrew-tap/Casks/bigboard.rb`; prereleases skip that tap. The cask installs only `Bigboard.app`, requires macOS Tahoe, and explains the Apple Command Line Tools requirement. It does not install Homebrew Git or a Bigboard command.

## Resume a failed release

Inspect the failed run, existing release/assets, remote tag commit, and tap before retrying. Do not move or delete the tag, invent another version, change secrets, overwrite conflicting assets, or publish locally through a second path.

If a workflow correction is needed, merge and verify it on `main`. Use the same Release workflow's `workflow_dispatch` input to resume the existing tag:

```sh
gh workflow run release.yml --repo richhaase/bigboard --ref main -f tag=<existing-tag>
```

The workflow keeps its automation checkout separate from the exact tagged application source. On a recovery dispatch, the workflow's `headSha` can therefore differ from the released source SHA; verify the resolved source SHA in the run as well. Rerunning an old failed job does not pick up a workflow correction on `main`.

The publisher reuses a verified uploaded archive rather than replacing it with a newly signed ZIP. Conflicting existing bytes stop recovery. If the release already published correctly, the same workflow can finish the tap update without republishing the app. Keep release concurrency serialized to prevent competing tap updates.

## Verify completion

Monitor the Release run to completion with spaced checks and brief updates. Verify the published version/prerelease status, expected ZIP and checksum file, ZIP SHA256, and successful universal build, Developer ID signing, notarization, and stapling (or verified reuse of the previously signed artifact). For a stable release, verify the remote cask's version, URL, SHA256, app artifact, and minimum macOS requirement match the release. A pushed tag or draft with uploaded assets is not a completed release.

Report the version, release link, main change, and workflow/tap result. For the first native release, the owner validates signed-upgrade Keychain continuity after publishing and reports regressions for a patch, as documented in `native/README.md`. Ad-hoc local builds do not establish that behavior; do not add it as a pre-release gate.
