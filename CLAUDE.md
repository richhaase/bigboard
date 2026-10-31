# Bigboard development context

Bigboard is a native Swift app. The product entry point is
`native/Sources/BigboardApp/BigboardApp.swift`; see [README](README.md) and
[native guide](native/README.md). Target macOS 26 Tahoe or later and Swift 6. Use native macOS
conventions while preserving the existing workflows. No legacy import.

`native/Sources/BigboardCore` contains authentication, GitHub collection, owned
Git caches, analysis, versioned JSON storage and data models. SwiftUI/AppKit
views and application orchestration are in `native/Sources/BigboardApp`.
The source tree and CI cover the native app; previous implementations are
available in Git history.

Durable JSON belongs in Application Support; rebuildable history belongs in
Caches; credentials reuse the existing Keychain service/account. Do not run the
old and new apps concurrently with shared rotating credentials. Do not import
old configuration, history, grants, bookmarks, or browser preferences.

## Data and state contracts

- Startup, Settings, and Identities share account-scoped `workspace.json` in
  Application Support. Settings updates workspace fields while preserving analysis rules;
  identity changes update only `identities`. Use the same atomic writer for both,
  reject conflicting external edits, and preserve the user’s draft.
- Live startup publishes cached history before the network pass completes.
  Swift Observation publishes snapshots to the native interface. Later
  refreshes are explicit. Never fill runtime gaps with fabricated records.
- Data Status opens from one clickable control containing status and timestamp
  at the bottom right. It shows a spinner during refresh; the main screen's top
  progress display remains. Date/repository filters and Refresh stay in the
  toolbar. The Boards menu and `e` shortcut also open Data Status.
- Settings saves repository collection choices; named groups retain their existing
  collection scope. Repository filter is shared viewing state in BoardState and
  only narrows already loaded data. It never refreshes, saves collection settings,
  or adds session tracking. All repositories uses an empty filter set; Selected
  repositories requires at least one loaded repository. Exact exclusions win over
  explicit inclusions, then config exclusion patterns apply. Newly discovered
  repositories follow those same rules. Settings reuses a current verified inventory or verifies access when needed before
  applying repository changes. Apply reports local save success separately from
  background refresh, so the sheet closes without waiting for network collection.
- Preserve usable snapshots on failure and qualify stale/partial evidence.
  PR inventory coverage is independent of file and reviewer coverage. Unknown
  inventory with no rows is not a complete zero. PR loading is also distinct
  from an empty completed inventory.
- Use author dates for Git and update times for PRs. Compare against retained
  snapshot times with an exclusive lower bound. Calendar buckets use local time;
  preserve future-dated selected evidence. Default to 14 days unless configured.
- Deduplicate shared object IDs with `Analysis`'s canonical selection rules while
  preserving repository associations. Complete line counts beat unknown counts;
  remaining copy choice is deterministic, not the largest change volume.
  Names do not merge people. Apply mailmap then saved email mappings.
- Generated paths, `.gitattributes` generated/vendored paths, and bulk data
  changes are excluded from line totals unless `allFiles` is set; report the
  excluded volume by reason rather than dropping it silently.
- Directory volume includes only scoped files. Unknown line counts contribute
  commits but not measured lines. Bars always represent additions/removals,
  regardless of sorting. Activity and PR evidence are not productivity or
  merge-readiness judgments.
- All-time JSON export uses the loaded tracked snapshot and Swift aggregation,
  ignoring viewing filters and making no network requests. Native export uses a Save dialog.
- Cache removal lives in Workspace Settings > Storage. It is ownership-checked
  and limited to unlisted repositories after a successful live GitHub refresh; Settings discovery and Cached history alone
  do not authorize removal. GitHub path links verify retained revisions locally.
  Never scan or mutate developer checkouts.
- Bookmarks use a fresh native account-scoped timestamp file. Their baseline stays fixed during
  a session; normal native exit saves eligible fetched snapshots by global/group
  scope. Offline scans, identity rescans, and export do not advance them.

## Implementation discipline

Keep the contributor detail layout and independent board state intact. Use SwiftUI Text for remote content; validate outbound GitHub URLs.
Bound large commit lists and preserve cancellation across async work.
Changes to models must update projections, views and tests consistently. No source comments should be added on Rich’s behalf.

## Build and verify

```sh
make build
make dev
make test
make check
```

`make build` outputs `native/build/Bigboard.app`. `make dev` builds and opens it.
`make test` runs Swift tests; `make check` also checks optimized compilation.
Use explicit `--ui-preview` for isolated visual fixtures. Real GitHub/Keychain
and signed distribution checks are separate from mocked tests. No source
comments should be added on Rich's behalf.

## Release boundary

The tag workflow in [.github/workflows/release.yml](.github/workflows/release.yml)
and the packaging scripts define release behavior. The GUI package is a signed,
notarized universal macOS app with an app-only Homebrew cask and a Git
dependency. Publisher-owned GitHub App build settings are required for release
sign-in; see [publisher setup](docs/github-app.md). Local builds and PR checks do not publish, install, or release it.
Do not run a tag, release, or installation without the user’s authorization.
