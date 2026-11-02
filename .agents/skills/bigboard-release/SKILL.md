---
name: bigboard-release
description: Publish a Bigboard version through richhaase/bigboard's GitHub Actions Release workflow.
---

# Bigboard release

Verify the checkout's origin is `richhaase/bigboard`. Read `AGENTS.md`,
`.github/workflows/release.yml`, and `scripts/release.sh`.

1. Inspect current main, releases, and remote tags. Preserve unrelated local work.
2. Confirm CI passed for the commit to release. Use the user's requested version;
   otherwise choose the smallest appropriate version bump and announce it.
3. When the user requests a release, create an annotated tag on the verified main
   commit and push only that tag. Do not move existing tags or change credentials.
4. Watch the Release workflow. It tests and builds the tagged Swift source, uses
   the existing signing/notarization script, and publishes the ZIP and checksum
   with `gh release create`. Stable releases update the Homebrew cask.
5. Verify the published release, ZIP checksum, successful signing/notarization,
   and matching Homebrew version/checksum. Report the release link.

The workflow runs on new tag pushes only. On failure, report the actual failing
command, make the smallest fix, and use a new version for the next release.
Do not build recovery infrastructure for a one-off release failure.

No Go toolchain is needed. Publisher settings and signing credentials stay in
GitHub Actions secrets/variables. The owner validates signed-upgrade Keychain
continuity after the first native release, as described in `native/README.md`.
