# Native Bigboard

The replacement is a Swift 6 package targeting macOS 27. It has no third-party
Swift packages, JavaScript runtime, embedded browser, Go bridge, or HTTP server.
SwiftUI supplies the interface; AppKit supplies lifecycle, clipboard, browser,
and Save dialog integration; Security supplies Keychain; Foundation supplies
HTTP and filesystem operations. Git runs as a cancellable child process against
app-owned bare repositories.

## Source layout

| Location | Responsibility |
| --- | --- |
| `Sources/BigboardApp/WorkspaceModel.swift` | Account-scoped snapshots, refresh orchestration, settings, grants, bookmarks, export and native actions. |
| `Sources/BigboardApp/BoardState.swift` | Shared view scope and independent board state. |
| `Sources/BigboardApp/*View.swift`, `Dialogs.swift`, `Components.swift` | Native boards, inspectors and sheets. |
| `Sources/BigboardCore/Authentication.swift` | PKCE, callback validation, serialized rotation, opaque Keychain persistence. |
| `Sources/BigboardCore/GitHub.swift` | Paginated installation discovery, bounded PR evidence, request budgets and cooldowns. |
| `Sources/BigboardCore/GitHistory.swift` | Owned caches, default-branch probes/fetches, NUL-safe commit parsing, file rules, identities and parsed scan cache. |
| `Sources/BigboardCore/Analysis.swift` | Canonical deduplication, scoped totals, Heat, people, calendars and PR attention. |
| `Sources/BigboardCore/Storage.swift` | Versioned JSON, account isolation, atomic writes, conflicts, access and bookmarks. |
| `Tests/BigboardCoreTests/` | Mock HTTP/auth, real temporary Git repositories, projections, UI state and storage tests. |

## Storage choice

Small durable state uses versioned JSON in Application Support. This is easy to
inspect, supports atomic replacement and external-edit detection, and does not
need a database migration layer for the current workspace shape. Git objects
and parsed scans live in Caches because they can be rebuilt. No credentials are
stored in either location. A future historical PR store may justify SQLite;
that feature and its schema are outside this port.

`BIGBOARD_DATA_ROOT` and `BIGBOARD_CACHE_ROOT` override native paths for isolated
testing. They do not redirect Keychain; use `--ui-preview` to exercise the UI
without touching credentials or network. The preview uses an isolated credential
store and temporary workspace and labels its fixture data.

## Preserved behavior

- Cache-first history, explicit refresh, and cached-only mode. Stale and partial
  evidence is qualified. Missing inventory is never a complete zero.
- Git author dates and PR update dates; exclusive lower bounds; snapshot-relative
  ranges; local calendar weeks/days; future-dated evidence retained where applicable.
- Canonical shared-commit selection, repository associations, mailmap followed
  by saved identities, excluded generated/vendored/bulk volume and unknown lines.
- Collection settings, session tracking, named groups and view filters remain
  separate. Exact repository exclusions win over includes and patterns.
- Heat uses its own 26-week context, with recent four-week and preceding
  twelve-week averages, independent of the shared date range.
- PR inventory, file and reviewer completeness remain independent. The request
  budget and pagination bounds qualify unfinished work. Attention uses available
  individual evidence and does not infer team membership.
- Account-scoped offline grants are invalidated before checking live access.
  Confirmed denials remove visible evidence; transient failure can retain a
  previously authorized stale snapshot. Unlisted cache removal requires a
  successful live refresh and verifies cache ownership.
- Session bookmark baselines remain fixed. Normal quit saves eligible live
  snapshot times per global/group scope; offline scans and exports do not advance them.
- JSON export aggregates all loaded tracked history; Markdown copies current
  view context. Neither initiates network access.

Native control placement intentionally differs from the old web layout. Existing
web documentation and screenshots describe the reference app. Time-range
comparison, a new catch-up board, persistent acknowledge/snooze/pin, and historical
PR statistics remain separate future features.

## Build and verification

```sh
make build
make test
make check
python3 -B -m unittest discover -s scripts -p '*_test.py'
open -n native/build/Bigboard.app --args --ui-preview
```

Publisher settings may be placed in the ignored `native/publisher.local.json`
(string fields named `BIGBOARD_GITHUB_CLIENT_ID`, `BIGBOARD_GITHUB_CLIENT_SECRET`,
and `BIGBOARD_GITHUB_APP_SLUG`). Restrict it to your account with `chmod 600`.
`BIGBOARD_PUBLISHER_CONFIG` selects another JSON path; explicit environment
values override the file. The build validates values without printing them.

`DEVELOPER_DIR` defaults to `/Applications/Xcode.app/Contents/Developer`; override
it for another Xcode installation. `UNIVERSAL=1 CONFIGURATION=release bash
scripts/build-swift.sh` builds both CPU architectures. The existing tag workflow
packages the native executable, signs/notarizes it, and publishes the same app
artifact/cask name. No publication is part of local validation.

Automated fixtures do not establish live GitHub, real Keychain access, release
signing, notarization, or large-account performance. Before replacing an installed
app, test together with the old app closed:

1. Build with the existing publisher configuration and launch the native bundle.
2. Allow Keychain access if macOS asks; confirm the expected account is reused.
3. Discover repositories, select a small public/private set, review and save.
4. Refresh; inspect Activity → Heat → directory/file → commits → people → PRs.
5. Exercise Contributor selection, PR filters, groups, identities and both exports.
6. Quit and reopen; confirm native preferences/bookmarks and cached startup.
7. Test offline refresh, interrupted refresh, changed repository access, and cache controls.

Sign-out and account-switch tests change the shared credential, so schedule them
after the initial reuse test. The development app is locally signed; a signed
release still needs an end-to-end OAuth callback check and distribution checks.

## Verification recorded for this branch

On 2026-10-31, the replacement was built locally with Xcode 27.2 for arm64 and
x86_64, and the bundle signature was verified. Activity/Heat, Contributors,
Pull requests, and Settings were inspected in the isolated preview. A configured
build reused the existing `richhaase` Keychain sign-in, discovered installation
repositories, and saved a fresh single-repository workspace for
`richhaase/bigboard`. The live refresh loaded 187 commits and reported complete
PR inventory, files, and reviewers, with no data errors.

PR collection currently makes per-repository inventory requests rather than
the reference implementation's batched/lean GraphQL queries. The same bounds
and coverage qualifications apply, but large workspaces may exhaust the request
budget sooner. Remote Git branch probes use authenticated `ls-remote`.

Real private-repository failures, large-account performance, release signing,
notarization, and a new browser OAuth exchange still require joint testing.

Git operations isolate global/system Git configuration and inherited `GIT_*`
settings. Repository mailmap and native identity rules apply. Each command has
a 120-second deadline and 256 MiB output bound; exceptionally large history can
therefore produce an explicitly incomplete/stale result rather than finish.
The app supports its own complete bare caches, not arbitrary shallow/partial
caches or developer checkouts. The final automated suite covers 73 native tests
and 12 packaging tests.
