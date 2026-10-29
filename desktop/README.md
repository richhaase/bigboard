# Bigboard desktop development

Bigboard is a native Wails app with a Go backend and embedded HTML, CSS, and
JavaScript. The system webview renders the interface; the installed app does not
run a web server or require Node or Go. Git and an authenticated GitHub CLI remain
runtime dependencies for live data.

## Build and run on macOS

Use the Go version in the module files and install the Xcode command line tools.
For live data, install Git 2.31 or newer and GitHub CLI, then run `gh auth login`.
When launched from Finder, Bigboard also searches standard Homebrew locations
for these tools.

From the repository root:

```sh
make build
open desktop/build/bin/Bigboard.app
```

`make build` produces the local app bundle at `desktop/build/bin/Bigboard.app`.
This local build is separate from the signed and notarized release workflow.
`make dev` (also `make gui`) runs native development mode; `make gui-build` is an
alias for the bundle build.

For browser development, run `make preview` (also `make gui-preview`) and open
`http://127.0.0.1:34115`. This development server binds only to loopback. Native
windows call Wails bindings directly. The frontend uses ES modules with no
bundler or runtime npm dependencies.

## Application behavior

Startup reads the existing configuration and workspace settings. Live GitHub
mode publishes available cached history while the remote refresh is pending;
Cached history reads owned repositories without network requests and has no PR
context. Later refreshes are manual. Missing sources open setup guidance.

**Settings** saves sources and repository exclusions in `desktop.json`. Discovery
must succeed for the current sources before the user can review and save at
least one checked repository. New repositories remain eligible unless excluded.
**Repository controls** changes tracking for the session; the shared repository
picker only narrows the view. Existing `config.json`, cache, and last-look state
remain compatible. Identity edits update only `config.json`’s `identities` field.

The [user reference](../docs/reference.md) describes the three boards, filters,
export, coverage, bookmarks, and configuration. The [workflow coverage](parity.md)
document summarizes the tested behavior.

## Checks

```sh
make test
make fmt-check lint vet staticcheck
```

`make test` runs the core Go module, desktop Go module, and frontend model tests.
The `test-core`, `test-desktop`, `lint-core`, `lint-desktop`, `vet-core`,
`vet-desktop`, `staticcheck-core`, and `staticcheck-desktop` targets allow focused
checks. Desktop tests run from the nested `desktop` module; root `go test ./...`
does not include them.

For isolated browser checks, use the repository-pinned Node version through fnm:

```sh
cd desktop
fnm exec --using $(cat ../.node-version) npm ci
fnm exec --using $(cat ../.node-version) npx playwright install chromium
fnm exec --using $(cat ../.node-version) npm run test:e2e
```

The browser harness builds the development server with temporary configuration
and cache directories, intercepts test-only API fixtures, and writes screenshots
to `desktop/build/screenshots`. It does not access the user’s normal GitHub
configuration. Backend tests use fake providers and temporary storage.

## Release packaging

The tag workflow in [release.yml](../.github/workflows/release.yml) is the
release authority. It builds a universal macOS app, signs with Developer ID,
notarizes and staples it, then publishes
`Bigboard_<version>_darwin_universal.zip` and `checksums.txt`. The
`richhaase/tap/bigboard` cask installs the app and declares Git and GitHub CLI as
dependencies. Users launch Bigboard from Finder, Spotlight, or the Dock.

This branch prepares that distribution; it does not claim a GUI release has
already been published. No Windows or Linux application package is distributed
by this workflow. Signing credentials belong in the release environment, never
in source or local documentation.
