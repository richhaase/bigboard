# Bigboard development context

Bigboard is a native GUI for GitHub repository activity, contributors, and PR
triage. The application entry point is `desktop/`; there is no separate Bigboard
command-line product. See [README](README.md) for local startup and
[docs/reference.md](docs/reference.md) for the user-facing contract.

## Architecture

| Location | Responsibility |
| --- | --- |
| `git/` | Owned bare Git caches, authenticated clone/fetch, commit collection, file filtering, identities, disposable parsed scans, retained-revision queries |
| `auth/` | Native OAuth code/PKCE/state, backend token rotation, and opaque Keychain store interface |
| `github/` | Repository discovery, branch probes, bounded PR inventories/files/reviewers, request budgets and cooldowns through direct HTTPS APIs |
| `stats/` | Canonical commit selection, contributor aggregation, scoped directory counts, calendars, related work areas |
| `gui/` | Application service, snapshots, cache-first refresh, settings/identities, repository controls, groups, bookmarks, JSON export |
| `desktop/main.go` | Native Wails lifecycle, embedded assets, development-only loopback server entry |
| `desktop/app.go` | Native bridge, clipboard/browser integration, export Save dialog |
| `desktop/frontend/app.js` | Boards, dialogs, keyboard interaction, and rendering |
| `desktop/frontend/model.js` | Pure frontend projections; tested independently of the browser |
| `desktop/frontend/styles.css` | Responsive GUI styling |
| `desktop/server.go` | Development HTTP bridge; the native app does not use it |
| `desktop/frontend/model.test.js`, `desktop/smoke.mjs` | Projection tests and isolated browser workflow regressions |
| `scripts/*.sh`, `scripts/release_test.py` | Native build, signing, cask generation, and packaging regressions |
| `.goreleaser.yaml`, `.github/workflows/release.yml` | Release artifacts, notes, and tag-driven publication |
| `desktop/wails.json`, `desktop/build/darwin/Info.plist` | Desktop build configuration and macOS bundle metadata |

The root Go module uses the standard library. `desktop` is a nested module with
Wails dependencies and a local replacement for the root module. The frontend is
plain ES modules; assets are embedded without a bundler. Runtime dependencies
are Git and native GitHub sign-in, not Go, Node, GitHub CLI, or a hosted service.

## Data and state contracts

- Startup, Settings, and Identities share `config.json` under the XDG config
  directory. Settings updates workspace fields while preserving analysis rules;
  identity changes update only `identities`. Use the same atomic writer for both,
  reject conflicting external edits, and preserve the user’s draft.
- Live startup publishes cached history before the network pass completes.
  Frontend polling reads the lightweight snapshot status only while an
  operation is pending and fetches the full snapshot when it changes. Later
  refreshes are explicit. Never fill runtime gaps with fabricated records.
- Keep collection settings, session tracking, named groups, and viewing filters
  distinct. Exact exclusions win over explicit inclusions, then config exclusion
  patterns apply. Newly discovered repositories follow those same rules. Settings must discover current sources before review/save.
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
- Generated paths, `.gitattributes` generated/vendored paths, and bulk data
  changes are excluded from line totals unless `all_files` is set; report the
  excluded volume by reason rather than dropping it silently.
- Directory volume includes only scoped files. Unknown line counts contribute
  commits but not measured lines. Bars always represent additions/removals,
  regardless of sorting. Activity and PR evidence are not productivity or
  merge-readiness judgments.
- All-time JSON export uses the loaded tracked snapshot and Go aggregation,
  ignoring viewing filters and making no network requests. Native export uses a
  Save dialog; the browser development harness uses a download.
- Cache removal is ownership-checked and limited to unlisted repositories after
  a successful live GitHub refresh; Settings discovery and Cached history alone
  do not authorize removal. GitHub path links verify retained revisions locally.
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
make check
```

`make build` outputs `desktop/build/bin/Bigboard.app`; `make dev` runs native
development. `make preview` is a loopback development server, not deployment.
The `gui`, `gui-build`, and `gui-preview` aliases remain available.

`make test` covers both Go modules and frontend models; root `go test ./...`
alone misses the nested desktop module. `make test-frontend` runs projection
tests; `make check` combines formatting, lint, vet, staticcheck, and all tests.
Use the `-core` and `-desktop` variants
of test/lint/vet/staticcheck for focused checks. Use fnm with `.node-version`
for Node commands. The browser harness is described in
[desktop/README.md](desktop/README.md) and uses temporary storage and test-only
API interception. Native launch, GitHub refresh, and release checks are separate
from isolated model tests.

## Release boundary

The tag workflow in [.github/workflows/release.yml](.github/workflows/release.yml)
and the packaging scripts define release behavior. The GUI package is a signed,
notarized universal macOS app with an app-only Homebrew cask and a Git
dependency. Publisher-owned GitHub App build settings are required for release
sign-in; see [publisher setup](docs/github-app.md). Local builds and PR checks do not publish, install, or release it.
Do not run a tag, release, or installation without the user’s authorization.
