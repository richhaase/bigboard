# Bigboard desktop prototype

A native Wails desktop application using Bigboard's existing Go Git cache,
GitHub provider, and contributor identity rules. The web interface is bundled
inside the application; no hosted service or frontend build is required.
The terminal application remains available through its existing commands.

## Run

From the repository root:

```sh
make gui
```

Go and your platform's Wails prerequisites are required: Xcode command line
tools on macOS, WebView2 on Windows, or GTK3 and WebKitGTK development packages
on Linux. See [Wails installation](https://v2.wails.io/docs/gettingstarted/installation/).
Linux systems with WebKitGTK 4.1 need the `webkit2_41` build tag.
Git and a signed-in `gh` CLI are needed for live data. On macOS, the app also
searches the standard Homebrew locations when launched from Finder.

To build a native application bundle:

```sh
make gui-build
```

On macOS the output is `desktop/build/bin/Bigboard.app`. This development bundle
is locally signed; release signing, notarization, installers, and automatic
updates are outside this prototype.

For an optional local web preview, run `make gui-preview` and open
`http://127.0.0.1:34115`. It binds only to loopback. The native app uses Wails
bindings rather than an HTTP listener.

## Data sources

The application loads your existing Bigboard configuration on launch. When an
account or organizations are configured, it starts in **Live GitHub** mode,
shows available cached history first, and refreshes default-branch history and
open PRs using your existing signed-in `gh` CLI. Your configured repository
exclusions and contributor identity rules apply automatically.

**Settings** offers two real data sources:

- **Cached history** reads Bigboard's owned repository cache without network
  requests. PR context is unavailable in this mode.
- **Live GitHub** discovers repositories from your personal account and
  organizations, refreshes the owned cache, and loads bounded PR metadata.

When no account is configured, Bigboard loads any existing cached repositories
and offers setup guidance. It never substitutes sample records for missing or
unavailable data. Settings from the earlier sample-data prototype migrate to
your existing account/organization configuration on launch.

Settings are stored separately in `$XDG_CONFIG_HOME/bigboard/desktop.json`
(default `~/.config/bigboard/desktop.json`). Existing terminal configuration
provides initial account/organization sources and analysis rules, including
identity aliases, generated-file exclusions, bot/AI identities, and PR request
budget. The GUI does not overwrite `config.json`. Conflicting external edits
cause a settings save to fail rather than overwrite them.

Refresh is manual after startup. Failures keep prior evidence where possible,
mark it stale, and surface diagnostics in **Data status**. Dates filter Git
history by author date and PRs by update time. Change volume describes activity,
not productivity. PR check/review badges are evidence, not merge authorization.

## Explore

- **Activity:** compare repositories, open directories and files, inspect the
  commits and contributors behind the selected scope.
- **Contributors:** inspect a person's repository distribution, daily history,
  activity calendar, and commits.
- **Pull requests:** search and filter by author, draft state, reviews, checks,
  and work authored by or requesting review from the signed-in account. Open
  details and follow real PRs to GitHub.
- The date and repository scope applies across boards; board-specific searches
  and filters are retained when switching. Settings controls data collection;
  repository scope only controls what is visible.

This prototype covers those workflows. It does not yet port the terminal's
identity merge editor, named-group launcher, last-look bookmarks, related-area
overlap lenses, or owned-cache deletion controls. Identity aliases already
configured in the terminal are honored.

## Verify

```sh
make gui-test
go test ./...
cd desktop && go test -race ./...
```

The repository `.node-version` is used by `fnm` for frontend tests. The frontend
uses native ES modules and has no runtime npm dependencies. Backend tests use
fake providers and temporary storage; they do not contact GitHub or alter your
normal configuration.

To run the isolated browser workflow checks:

```sh
cd desktop
fnm exec --using $(cat ../.node-version) npm ci
fnm exec --using $(cat ../.node-version) npx playwright install chromium
fnm exec --using $(cat ../.node-version) npm run test:e2e
```

These checks build a local preview with temporary configuration/cache directories,
exercise the boards and dialogs, and write screenshots to
`desktop/build/screenshots`. They use test-only API fixtures and never use your normal GitHub configuration.
