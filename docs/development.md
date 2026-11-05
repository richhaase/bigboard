# Bigboard development

[README](../README.md) · [User reference](reference.md)

## Local development

Use Xcode 26 or later with the macOS 26 SDK or newer, a usable Git installation,
and the GitHub CLI signed in with `gh auth login`.
Commands below run from the repository root. `DEVELOPER_DIR` defaults to
`/Applications/Xcode.app/Contents/Developer`; override it for another Xcode installation.

Set up [development signing](#development-signing) once on each Mac, then run:

```sh
make dev
```

This builds and opens `build/Bigboard Dev.app`, which uses your active GitHub CLI
account. Choose the folders that hold your clones, then select repositories in
Settings. Quit Bigboard Dev before rebuilding. Its settings and caches persist
across builds.

The app executable is also the `bigboard` command. It runs as a command line
tool when invoked through a link whose name is not `Bigboard`, or when its first
argument is a command such as `status`, and uses the data of the app bundle it
belongs to. For Bigboard Dev:

```sh
ln -sf "$PWD/build/Bigboard Dev.app/Contents/MacOS/Bigboard" /opt/homebrew/bin/bigboard-dev
bigboard-dev status
```

To debug in Xcode, open `Bigboard.xcodeproj` and select the **Bigboard Dev**
scheme. The **Bigboard App** scheme uses the production identity. Both schemes
run the existing package tests with **Product → Test** (⌘U), without launching
the app or reading your GitHub CLI token. `make test` runs the same tests from the terminal.

| Command | Result |
| --- | --- |
| `make dev` | Build and launch Bigboard Dev. |
| `make build` | Build `build/Bigboard.app` with the production identity. |
| `make test` | Run the Swift package tests serially. |
| `make check` | Run tests, build the optimized app, and verify arm64 and x86_64 support. |
| `make clean` | Remove package and app build output. |

### Development signing

Bigboard Dev uses an Apple Development certificate so macOS recognizes it as the
same app across rebuilds. To create the certificate:

1. Open **Xcode → Settings → Apple Accounts** (called **Accounts** in some versions).
2. Add or select your Apple Account and select your developer team.
3. Open **Manage Certificates**, click **+**, and choose **Apple Development**.
4. Create the ignored `Config/Development.local.xcconfig` with your Team ID:

   ```xcconfig
   DEVELOPMENT_TEAM = YOUR_TEAM_ID
   ```

Your Team ID is listed in your Apple Developer account's membership details.
The certificate and its private key stay in your login Keychain. Both `make dev`
and the **Bigboard Dev** Xcode scheme use these signing settings. A missing
certificate or team must be configured before building; development builds do
not fall back to ad hoc signing.

On the first build, macOS may ask to let `codesign` use the certificate's private
key. Choose **Always Allow** to authorize signing subsequent builds.

### App identities

| Setting | Production | Development |
| --- | --- | --- |
| Bundle identifier | `com.richhaase.bigboard` | `com.richhaase.bigboard.dev` |
| Config and cache directory | `bigboard` | `bigboard-dev` |

The bundle identity selects the data paths. Development starts with an
independent workspace; it does not copy production data. The installed release
can stay open while Bigboard Dev runs. Both use the same GitHub CLI account.

Debug and Release configurations use the production identity. Close the installed
release before launching a local production build: those copies share data.
Local Debug and Release builds are ad hoc signed. Bigboard Dev uses the
development certificate described above; published releases use Developer ID
signing.

## Source organization

The Xcode app target links the local Swift package, which contains the application
implementation and tests. SwiftUI provides the interface; AppKit handles lifecycle
and desktop integration. Foundation provides HTTP and filesystem operations.
There are no third-party Swift dependencies. At runtime Bigboard depends on the
`git` and `gh` executables.

Paths in this table are relative to the repository root.

| Location | Responsibility |
| --- | --- |
| `Bigboard.xcodeproj`, `App/` | App target, schemes, and launcher. |
| `Config/` | Build settings, bundle metadata, and archive export options. |
| `Assets.xcassets/` | App icon asset catalog; source artwork is in `Assets/AppIcon.png`. |
| `Sources/BigboardCLI/` | The `bigboard` command: dispatch, options, and text and JSON reports. |
| `Sources/BigboardApp/WorkspaceModel.swift` | Refresh orchestration, account state, settings, clone discovery, and native actions. |
| `Sources/BigboardApp/BoardState.swift` | Shared viewing scope and independent board state. |
| `Sources/BigboardApp/Boards/` | Board views, charts, and commit lists. |
| `Sources/BigboardApp/Panels/`, `Sources/BigboardApp/Settings/` | Sheets, popovers, and settings editors. |
| `Sources/BigboardCore/GitHubCLI.swift`, `Sources/BigboardCore/ApplicationIdentity.swift` | GitHub CLI token and identity, and build identities. |
| `Sources/BigboardCore/GitHub.swift` | GitHub viewer check, PR evidence, pagination, and request budgets. |
| `Sources/BigboardCore/History/` | Git processes, clone discovery, fetch-only refresh, history parsing, and counting rules. |
| `Sources/BigboardCore/Analysis.swift`, `Sources/BigboardCore/Models.swift` | Data models, scoped totals, Heat, contributor analysis, and PR attention. |
| `Sources/BigboardCore/Storage.swift` | XDG config and cache paths, atomic writes, and conflict detection. |
| `Tests/App/`, `Tests/CLI/`, `Tests/Core/`, `Tests/Workspace/`, `Tests/Support/` | UI state, command line, core logic, workspace integration, and shared fixtures. |

Xcode assembles the bundle and processes its Info.plist and icon catalog.
`scripts/build-macos.sh` invokes Xcode for local builds;
`scripts/release.sh` archives, exports, and publishes releases.

## Storage and access

Bigboard follows the XDG base directories, like `gh` and Git. Settings are one
versioned JSON file, `$XDG_CONFIG_HOME/<name>/config.json` (default
`~/.config/<name>/config.json`), shared by every GitHub CLI account. Parsed
scans and repository locks live under `$XDG_CACHE_HOME/<name>/history/`
(default `~/.cache/<name>/history/`), next to `gh-identity.json`, the last
verified GitHub login and numeric ID that lets cached history open offline.
Relative XDG values are ignored. The directory names are listed above. Git
history stays in the user's clones, and clones are found again on every refresh
rather than saved. Bigboard stores no tokens; it runs `gh auth token` when it
needs one and keeps the result in memory for at most five minutes.

`BIGBOARD_CONFIG_ROOT` and `BIGBOARD_CACHE_ROOT` override the config and cache
roots for isolated testing. Use Bigboard Dev for
authenticated testing alongside the installed release.

Git runs as a cancellable child process against the user's clones. The app
discovers Git through PATH and standard Homebrew/system locations, ignores
global and system Git config, and pins hooks, fsmonitor, maintenance, and
protocol settings off. Refresh is a single `git fetch` of
`+refs/heads/<branch>:refs/remotes/<remote>/<branch>` from
`https://github.com/<owner>/<name>.git`, authenticated with an `extraHeader`
passed through the environment; it is skipped when repo-local config sets
`http.*`, `url.*`, `credential.*`, `include*`, or proxy keys. Scans read the
remote-tracking ref as a bare repository with the mailmap pinned to the scanned
revision, so working files never affect results. Partial clones are scanned
without `--numstat` and marked as unknown lines and paths. Tests must never fetch
from the network or touch real clones. History is streamed; parsed JSON caches
are optional and can be rebuilt.
Resource limits and interrupted collection retain honest stale or partial
coverage instead of silently truncating results.

Fetch denials mark a repository stale without hiding its local history; a
rejected GitHub CLI token disconnects the session. See the
[user reference](reference.md#data-status-offline-work-and-storage) for offline
behavior.

## Testing

Run `make check` before submitting code changes. Tests use temporary directories,
local Git fixture repositories, mock HTTP transports, and a stubbed GitHub CLI.
They do not run `gh` or read real tokens.

Tests run serially so subprocess-heavy fixtures do not interfere with timeout
and cancellation checks. Individual tests still exercise concurrent operations.
Add focused unit or integration coverage for changed behavior.

For UI changes, verify the affected workflow in Bigboard Dev with real
repositories. Include saving/canceling edits, keyboard interaction, and
quit/reopen behavior when relevant. GitHub CLI changes also need a check with the
real `gh` (signed in, signed out, and switched accounts). Automated fixtures do
not establish those live behaviors or large-workspace performance.

## Releases

Publishing requires repository write access and the
[release credentials](#release-credentials).

1. Use current main and confirm CI passed for the commit being released.
2. Choose an unused version, create an annotated `vX.Y.Z` tag on that commit,
   and push only that tag. The tag message is the release notes: write what
   changed for users and anything they must do after upgrading. GitHub's
   generated list of merged pull requests follows it. Lightweight tags and
   empty messages are rejected before the build starts.
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

### Release credentials

Configure these repository secrets for the
[Release workflow](../.github/workflows/release.yml):

| Secret | Purpose |
| --- | --- |
| `QUILL_SIGN_P12` | Base64-encoded Developer ID Application certificate and private key. |
| `QUILL_SIGN_PASSWORD` | Password for the certificate file. |
| `QUILL_NOTARY_KEY` | Apple notarization API key, as PEM text or base64. |
| `QUILL_NOTARY_KEY_ID` | Notarization API key ID. |
| `QUILL_NOTARY_ISSUER` | Notarization API issuer ID. |
| `HOMEBREW_TAP_GITHUB_TOKEN` | Token with write access to `richhaase/homebrew-tap`. |

GitHub supplies `GITHUB_TOKEN` for publishing the release in this repository.
The certificate action imports the signing identity into a temporary keychain.
The workflow requires one Developer ID Application identity and derives its
Team ID. It passes `SIGNING_IDENTITY`, `DEVELOPMENT_TEAM`, `NOTARY_KEY_PATH`,
`NOTARY_KEY_ID`, and `NOTARY_ISSUER` to the release script. The temporary
notarization key and signing keychain are cleaned up after the job.

### Installed-upgrade verification

For changes to packaging, signing, or storage:

1. Start with an installed release that has repositories selected, then quit it.
2. Upgrade through Homebrew or replace it with the published app at the same location.
3. Launch and confirm the account, repository selections, and cached data persist.
4. Quit and reopen, and confirm the workspace opens without reconnecting.

Keep the Team ID, bundle identifier, and compatible signing requirements. Local
ad hoc builds do not establish Developer ID upgrade continuity. See
[Apple's code identity guidance](https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements).
