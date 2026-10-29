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
updates are outside this prototype. The Go service and frontend assets are
embedded in the executable inside that bundle. Users do not need Node, Go,
or a separate web server. Finder presents the `.app` as one application.

Wails uses the platform webview instead of shipping a browser engine. This
fits Bigboard's existing Go backend and produces one GUI application, but
the current application still invokes installed Git and the authenticated
`gh` CLI. A distribution with no separately installed tools would need those
dependencies bundled or replaced; switching to Electron would not remove them.
See [Wails packaging](https://v2.wails.io/docs/introduction/).

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
budget. Workspace preferences do not overwrite `config.json`. Contributor
identity edits update only its `identities` field so the terminal and desktop
use the same mapping. Conflicting external edits cause a settings save to fail
rather than overwrite them. Saving repository choices stores exclusions, so
newly discovered repositories remain eligible on later refreshes.

Refresh is manual after startup. Failures keep prior evidence where possible,
mark it stale, and surface diagnostics in **Data status**. Dates filter Git
history by author date and PRs by update time. Change volume describes activity,
not productivity. PR check/review badges are evidence, not merge authorization.

## Explore

- **Activity:** compare repositories, open directories and files, inspect the
  commits and contributors behind the selected scope.
- **Contributors:** inspect a person's repository distribution, history,
  activity calendar, and commits. Export all-time contributor statistics as JSON
  using the same schema and aggregation as the terminal.
- **Pull requests:** search and filter by author, draft state, reviews, checks,
  and work authored by or requesting review from the signed-in account. Open
  details and follow real PRs to GitHub.
- The date and repository scope applies across boards; board-specific searches
  and filters are retained when switching. Settings controls data collection;
  repository scope only controls what is visible.

The initial range is **14 days**, or the existing configuration's `since` value.
The toolbar includes 1, 7, 14, 30, 90, and 365 days plus All time. Custom days
(1–3650) and Since last look are available in the date dialog. Shared scope and
board filters can be staged together, applied, or canceled.

Activity includes Commits, People, and Related lenses, contributor overlap,
named work areas, verified links at the retained scan revision, and temporary
repository PR views. Contributor timelines switch between daily, weekly, and
monthly buckets; identity rename and merge use a review step before saving.
PR triage includes Needs my attention, Authored by me, Review requested from me,
and Updated since my review, with full sort/reverse and coverage details.

Repository controls change tracking for the current session and show cache
usage. Only caches no longer listed by a successful discovery can be removed,
after explicit confirmation. Configured named groups are available in the
scope toolbar. Last-look bookmarks share the terminal's state file, remain
fixed during the session, and advance on normal native-app exit only after a
usable fetched snapshot. Offline scans and identity rescans do not advance them.

See the [parity checklist](parity.md) for the comparison and verification scope.

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
