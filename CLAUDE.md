# Bigboard development context

Bigboard is a native GUI for GitHub repository activity, contributors, and PR
triage. The application entry point is `desktop/`; there is no separate Bigboard
command-line product. See [README](README.md) for local startup and
[docs/reference.md](docs/reference.md) for the user-facing contract.

## Architecture

| Location | Responsibility |
| --- | --- |
| `git/` | Owned bare Git caches, authenticated clone/fetch, commit collection, file filtering, identities, disposable parsed scans, retained-revision queries |
| `github/` | Repository discovery, branch probes, bounded PR inventories/files/reviewers, request budgets and cooldowns through `gh` |
| `stats/` | Canonical commit selection, contributor aggregation, scoped directory counts, calendars, related work areas |
| `gui/` | Application service, snapshots, cache-first refresh, settings/identities, repository controls, groups, bookmarks, JSON export |
| `desktop/main.go` | Native Wails lifecycle, embedded assets, development-only loopback server entry |
| `desktop/app.go` | Native bridge, clipboard/browser integration, export Save dialog |
| `desktop/frontend/app.js` | Boards, dialogs, keyboard interaction, and rendering |
| `desktop/frontend/model.js` | Pure frontend projections; tested independently of the browser |
| `desktop/frontend/styles.css` | Responsive GUI styling |
| `desktop/server.go` | Development HTTP bridge; the native app does not use it |

The root Go module uses the standard library. `desktop` is a nested module with
Wails dependencies and a local replacement for the root module. The frontend is
plain ES modules; assets are embedded without a bundler. Runtime dependencies
are Git and authenticated GitHub CLI, not Go, Node, or a hosted service.

## Data and state contracts

- Startup reads `config.json` for analysis rules and initial sources, then
  `desktop.json` for the saved workspace. Do not overwrite unrelated config
  fields. Identity changes update only `identities`; reject conflicting external
  file edits and preserve the user’s draft.
- Live startup publishes cached history before the network pass completes.
  Frontend snapshot polling runs only while an operation is pending. Later
  refreshes are explicit. Never fill runtime gaps with fabricated records.
- Keep collection settings, session tracking, named groups, and viewing filters
  distinct. Saved exclusions allow newly discovered repositories on later
  refreshes. Settings must discover current sources before review/save.
- Preserve usable snapshots on failure and qualify stale/partial evidence.
  PR inventory coverage is independent of file and reviewer coverage. Unknown
  inventory with no rows is not a complete zero. PR loading is also distinct
  from an empty completed inventory.
- Use author dates for Git and update times for PRs. Compare against retained
  snapshot times with an exclusive lower bound. Calendar buckets use local time;
  preserve future-dated selected evidence. Default to 14 days unless configured.
- Deduplicate shared object IDs with `stats`’ canonical selection rules while
  preserving repository associations. Complete line counts beat unknown counts;
  remaining copy choice is deterministic, not the largest change volume.
  Names do not merge people. Apply mailmap then saved email mappings.
- Directory volume includes only scoped files. Unknown line counts contribute
  commits but not measured lines. Bars always represent additions/removals,
  regardless of sorting. Activity and PR evidence are not productivity or
  merge-readiness judgments.
- All-time JSON export uses the loaded tracked snapshot and Go aggregation,
  ignoring viewing filters and making no network requests. Native export uses a
  Save dialog; the browser development harness uses a download.
- Cache removal is ownership-checked and limited to unlisted repositories after
  successful discovery. GitHub path links verify retained revisions locally.
  Never scan or mutate developer checkouts.
- Bookmarks share the existing timestamp file. Their baseline stays fixed during
  a session; normal native exit saves eligible fetched snapshots by global/group
  scope. Offline scans, identity rescans, and export do not advance them.

## Implementation discipline

Keep the contributor detail layout and independent board state intact. Render
remote content with escaping or `textContent`; validate outbound GitHub URLs.
Avoid unbounded commit-list DOM and argument spreads over large histories.
Changes to DTOs must update the native bridge, development server, and frontend
consistently. No source comments should be added on Rich’s behalf.

## Build and verify

```sh
make build
make dev
make test
make fmt-check lint vet staticcheck
```

`make build` outputs `desktop/build/bin/Bigboard.app`; `make dev` runs native
development. `make preview` is a loopback development server, not deployment.
The `gui`, `gui-build`, and `gui-preview` aliases remain available.

`make test` covers both Go modules and frontend models; root `go test ./...`
alone misses the nested desktop module. Use the `-core` and `-desktop` variants
of test/lint/vet/staticcheck for focused checks. Use fnm with `.node-version`
for Node commands. The browser harness is described in
[desktop/README.md](desktop/README.md) and uses temporary storage and test-only
API interception. Native launch, GitHub refresh, and release checks are separate
from isolated model tests.

## Release boundary

The tag workflow in [.github/workflows/release.yml](.github/workflows/release.yml)
and the packaging scripts define release behavior. The GUI package is a signed,
notarized universal macOS app with an app-only Homebrew cask and Git/gh
dependencies. Local builds and PR checks do not publish, install, or release it.
Do not run a tag, release, or installation without the user’s authorization.
