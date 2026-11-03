# Bigboard development

[README](../README.md) · [User reference](reference.md) · [Publisher setup](github-app.md)

## Local development

Use Xcode 26 or later with the macOS 26 SDK or newer and a usable Git installation.
Commands below run from the repository root. `DEVELOPER_DIR` defaults to
`/Applications/Xcode.app/Contents/Developer`; override it for another Xcode installation.

For GitHub sign-in, use the publisher configuration described in
[publisher setup](github-app.md#build-configuration). The GitHub App must
have both the production and development callback URLs registered.

```sh
make dev
```

This builds and opens `build/Bigboard Dev.app`. Sign in with your normal
GitHub account and select repositories in Settings. Quit Bigboard Dev before
rebuilding. Its sign-in, settings, and caches persist across builds.

To debug in Xcode, open `Bigboard.xcodeproj` and select the **Bigboard Dev**
scheme. The **Bigboard App** scheme uses the production identity. Both schemes
run the existing package tests with **Product → Test** (⌘U), without launching
the app or using your sign-in. `make test` runs the same tests from the terminal.

| Command | Result |
| --- | --- |
| `make dev` | Build and launch Bigboard Dev. |
| `make build` | Build `build/Bigboard.app` with the production identity. |
| `make test` | Run the Swift package tests serially. |
| `make check` | Run tests, build the optimized app, and verify arm64 and x86_64 support. |
| `make clean` | Remove package and app build output. |

### App identities

| Setting | Production | Development |
| --- | --- | --- |
| Bundle identifier | `com.richhaase.bigboard` | `com.richhaase.bigboard.dev` |
| Keychain service | `com.richhaase.bigboard.github` | `com.richhaase.bigboard.dev.github` |
| Support/cache directory | `com.richhaase.bigboard.native` | `com.richhaase.bigboard.dev.native` |

The bundle identity selects the OAuth callback, credential store, and data paths.
Development starts with an independent workspace; it does not copy production
credentials or data. The installed release can stay open while Bigboard Dev runs.
Both use the same GitHub App registration, so changes to installation access or
GitHub authorization affect both.

Debug and Release configurations use the production identity. Close the installed
release before launching a local production build: those copies share credentials
and data. Local builds are ad hoc signed, and macOS may request Keychain access
after a rebuild.

## Source organization

The Xcode app target links the local Swift package, which contains the application
implementation and tests. SwiftUI provides the interface; AppKit handles lifecycle
and desktop integration. Foundation provides HTTP and filesystem operations, and
Security provides Keychain access. There are no third-party Swift dependencies.

Paths in this table are relative to the repository root.

| Location | Responsibility |
| --- | --- |
| `Bigboard.xcodeproj`, `App/` | App target, schemes, and launcher. |
| `Config/` | Build settings, bundle metadata, and archive export options. |
| `Assets.xcassets/` | App icon asset catalog; source artwork is in `Assets/AppIcon.png`. |
| `Sources/BigboardApp/WorkspaceModel.swift` | Refresh orchestration, account state, settings, grants, bookmarks, and native actions. |
| `Sources/BigboardApp/BoardState.swift` | Shared viewing scope and independent board state. |
| `Sources/BigboardApp/Boards/` | Board views, charts, and commit lists. |
| `Sources/BigboardApp/Panels/`, `Sources/BigboardApp/Settings/` | Sheets, popovers, and settings editors. |
| `Sources/BigboardCore/Authentication.swift`, `Sources/BigboardCore/ApplicationIdentity.swift` | OAuth, token rotation, Keychain, and build identities. |
| `Sources/BigboardCore/GitHub.swift` | Repository discovery, PR evidence, pagination, and request budgets. |
| `Sources/BigboardCore/History/` | Git processes, owned caches, history parsing, and counting rules. |
| `Sources/BigboardCore/Analysis.swift`, `Sources/BigboardCore/Models.swift` | Data models, scoped totals, Heat, contributor analysis, and PR attention. |
| `Sources/BigboardCore/Storage.swift` | Account-scoped JSON, atomic writes, and conflict detection. |
| `Tests/App/`, `Tests/Core/`, `Tests/Workspace/`, `Tests/Build/` | UI state, core logic, workspace integration, and publisher configuration tests. |

Xcode assembles the bundle, processes its Info.plist and icon catalog, and runs
`scripts/configure-publisher.swift` before signing.
`scripts/build-macos.sh` invokes Xcode for local builds;
`scripts/release.sh` archives, exports, and publishes releases.

## Storage and access

Durable settings, access grants, and bookmarks use versioned JSON under
`~/Library/Application Support/<identity>/accounts/<account-id>/`.
Git mirrors and parsed scans live under
`~/Library/Caches/<identity>/accounts/<account-id>/repos/`.
The identity directory names are listed above. Credentials live only in Keychain.

`BIGBOARD_DATA_ROOT` and `BIGBOARD_CACHE_ROOT` override the support and cache
roots for isolated testing. They do not redirect Keychain. Use Bigboard Dev for
authenticated testing alongside the installed release.

Git runs as a cancellable child process against app-owned bare repositories.
The app discovers Git through PATH and standard Homebrew/system locations.
It isolates Git configuration and credentials from developer checkouts.
History is streamed; parsed JSON caches are optional and can be rebuilt.
Resource limits and interrupted collection retain honest stale or partial
coverage instead of silently truncating results.

Account-scoped grants control cached access. A pending check preserves the last
verified grant; confirmed denials remove the affected access. A failed save after
grant invalidation requires live verification again. Cancellation cannot restore
access after sign-out or an account change. See the
[user reference](reference.md#data-status-offline-work-and-storage) for
offline behavior, cache removal, and bookmark eligibility.

## Testing

Run `make check` before submitting code changes. Tests use temporary directories,
local Git fixture repositories, mock HTTP transports, and isolated credential
stores. They do not read real credentials or require a signed-in app.

Tests run serially so subprocess-heavy fixtures do not interfere with timeout
and cancellation checks. Individual tests still exercise concurrent operations.
Add focused unit or integration coverage for changed behavior.

For UI changes, verify the affected workflow in Bigboard Dev with real
repositories. Include saving/canceling edits, keyboard interaction, and
quit/reopen behavior when relevant. Authentication changes also need the real
browser callback and Keychain flow. Automated fixtures do not establish those
live behaviors or large-workspace performance.

## Releases

Publishing requires repository write access and the configured
[publisher inputs](github-app.md#release-credentials).

1. Use current main and confirm CI passed for the commit being released.
2. Choose an unused version, create an annotated `vX.Y.Z` tag on that commit,
   and push only that tag.
3. Watch the [Release workflow](../.github/workflows/release.yml). It runs tests,
   archives and exports a universal app, signs with Developer ID, notarizes,
   staples, and verifies Gatekeeper acceptance.
4. Verify the published ZIP and `checksums.txt`. Stable releases also update
   the [Homebrew cask](https://github.com/richhaase/homebrew-tap/blob/trunk/Casks/bigboard.rb);
   its version and checksum must match the release.

Release assets are named `Bigboard_<version>_darwin_universal.zip`.
Prerelease tags publish GitHub prereleases without updating Homebrew.
New tag pushes trigger publication; local builds do not publish.

If a release fails, inspect the failing command and fix its cause. Leave the
tag in place and use a new version for the corrected release. If GitHub cannot
assign a runner, rerun the CI job when capacity returns; a queue failure alone
does not require a code change.

### Installed-upgrade verification

For changes to packaging, signing, authentication, or storage:

1. Start with a signed-in installed release, then quit it.
2. Upgrade through Homebrew or replace it with the published app at the same location.
3. Launch and confirm the account, repository selections, and cached data persist.
4. Quit and reopen. With the login Keychain unlocked, neither launch should
   require renewed Keychain permission or another OAuth sign-in.

Keep the publisher Team ID, bundle identifier, and compatible signing requirements.
Do not delete or recreate credentials to make the check pass. Local ad hoc builds
do not establish Developer ID upgrade continuity. See
[Apple's code identity guidance](https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements).
