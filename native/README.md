# Native Bigboard

Bigboard is a Swift 6 package targeting macOS 26 Tahoe or later. It has no third-party
Swift packages or embedded browser runtime.
SwiftUI supplies the interface; AppKit supplies lifecycle, clipboard, browser,
and Save dialog integration; Security supplies Keychain; Foundation supplies
HTTP and filesystem operations. Git runs as a cancellable child process against
app-owned bare repositories. History collection uses the Apple-signed Git in
`/Library/Developer/CommandLineTools`, installed with `xcode-select --install`.

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
| `Assets/AppIcon.png` | Source app icon used by local and release packaging. |

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
- Only saved, selected repositories are collected. Exact exclusions, exclusion
  patterns, and named groups can further narrow that selection. Newly discovered
  repositories stay unchecked until explicitly saved. The shared Repository
  filter only narrows loaded data; there is no session-tracking layer.
- Heat uses its own 26-week context, with recent four-week and preceding
  twelve-week averages, independent of the shared date range.
- PR inventory, file and reviewer completeness remain independent. The request
  budget and pagination bounds qualify unfinished work. Attention uses available
  individual evidence and does not infer team membership.
- Account-scoped offline grants remain available while live access checks are
  pending, including when the app quits mid-refresh. Completed verification
  replaces the grant; confirmed denials remove affected access. A failed save
  after invalidation requires live verification again. Transient failure can
  retain previously authorized stale evidence. Unlisted cache removal requires a
  successful live refresh and verifies cache ownership; it is available in
  Workspace Settings → Storage.
- Session bookmark baselines remain fixed. Normal quit saves a live snapshot
  time only when every repository in the collected global/group scope has current
  Git history and complete, nonstale PR inventory, with some commit history loaded.
  Failed or incomplete collection preserves the last eligible time. Bounded PR
  file/reviewer details remain qualified without blocking the bookmark. Viewing
  filters, offline scans, and exports do not advance it.
- JSON export aggregates all loaded tracked history; Markdown copies current
  view context. Neither initiates network access.

The [user reference](../docs/reference.md) and [workflow guide](../docs/visual-guide.md)
describe the native interface. Time-range comparison, a new catch-up board,
persistent acknowledge/snooze/pin, and historical PR statistics remain separate
future features.

## Repository collection and viewing

Use **Workspace Settings → Repositories** to save what Bigboard collects. Named
groups intersect that saved selection. These choices are separate from
**Repository filter**, available as a persistent toolbar popover, in the Boards
menu, or through `i`. The popover stays open across selections. The sidebar contains
only Activity, Contributors, and Pull requests. Workspace Settings lives in the
app menu and Keyboard Guide under Help. Data Status opens from a single clickable
control containing status and timestamp at the bottom right, or from the Boards menu or `e`.
The footer control spins during refresh; the main screen's top progress display
remains visible. Date and repository filters and Refresh stay in the toolbar.

The filter is one checklist, initially showing every loaded repository checked.
It stays open while changes apply immediately across Activity, Contributors,
and Pull requests. Reopening it reflects the current selection. Search narrows
the displayed rows, and **Select all loaded** checks every loaded repository.
When only one repository remains checked, its checkbox is disabled until another
repository is checked.

Filtering preserves each board's other filters. It makes no refresh or network
request, writes no settings, and does not unload history. Closing the picker
keeps the selection already applied.

Settings reuses the current verified repository inventory, supports Select All,
shows last-push dates, and defaults to most recently pushed with alternate sorts.
**Apply** validates and saves the local draft, then closes Settings while refresh
continues in the main screen. A save failure keeps the draft open. **General**
contains common preferences; advanced collection/counting options are grouped
separately. There is no intermediate review screen.

**Workspace Settings → Storage** contains local cache removal. Removal remains
limited to app-owned caches whose repositories were unlisted after a successful
live GitHub refresh. Settings discovery alone or changing a viewing filter does not authorize
removal. Cached history mode cannot remove caches.
Removal requires its own confirmation and does not change the GitHub repository.

## Build and verification

Use Xcode 26 or later with the macOS 26 SDK or newer. The deployment target is
macOS 26; building with a newer SDK does not raise the runtime requirement.

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
scripts/build-swift.sh` builds both CPU architectures. The Release workflow
builds the tagged source's `native/build/Bigboard.app`, signs, notarizes, and staples
it, then creates `release-artifacts/Bigboard_<version>_darwin_universal.zip`. A Python publisher uses
GitHub CLI to upload the ZIP and `checksums.txt` to a draft, verify the uploaded
bytes, and publish. Stable releases update the app-only Homebrew cask directly;
the cask requires macOS Tahoe and explains the app's Apple Command Line Tools
requirement, with no Homebrew Git dependency. No publication is part of local
validation.

New version tags trigger that workflow. To resume an existing tag after fixing
release automation on `main`, dispatch the same workflow with its `tag` input:
`gh workflow run release.yml --repo richhaase/bigboard --ref main -f tag=vX.Y.Z`.
The automation checkout and tagged application source are separate, so recovery
uses the corrected workflow while preserving the original tag and source commit.
It reuses verified uploaded archives and refuses conflicting assets. A completed
release can finish a failed tap update without replacing the published app.

Automated fixtures do not establish live GitHub, real Keychain access, release
signing, notarization, or large-account performance. Before replacing an installed
app, test together with the old app closed:

1. Build with the existing publisher configuration and launch the native bundle.
2. Allow Keychain access if macOS asks; confirm the expected account is reused.
3. Select a small public/private set in Settings and Apply. Verify that Settings
   closes after saving while the main screen shows refresh progress.
4. Refresh; inspect Activity → Heat → directory/file → commits → people → PRs.
5. Exercise Contributor selection, PR filters, groups, identities and both exports.
   Change Repository filter and verify immediate results across boards. Leave
   one repository checked and verify that it cannot be unchecked until another
   is checked. Close and reopen the picker; verify the selection is retained,
   saved collection choices remain intact, and no refresh starts.
6. Quit and reopen; confirm native preferences/bookmarks and cached startup.
7. Test offline refresh, interrupted refresh, changed repository access, and
   verified cache removal in Workspace Settings → Storage.

Sign-out and account-switch tests change the shared credential, so schedule them
after the initial reuse test. The development app is locally signed; a signed
release still needs an end-to-end OAuth callback check and distribution checks.

### Installed-upgrade Keychain validation

Normal published upgrades must reuse the existing sign-in without another
Keychain permission prompt. Initial permission is acceptable. For the first
native release, Rich will validate by upgrading to the published version and
report any regression for a follow-up patch. This is not a pre-release gate:

1. Install the prior Developer ID-signed Bigboard release at its normal location.
   With the login Keychain unlocked, establish the existing credential and
   approve persistent access once if needed.
2. Quit it and replace it through the normal upgrade path with the signed
   candidate. Both apps must use the same publisher Team ID, bundle identifier,
   compatible designated requirement, and existing Keychain entry. Do not delete
   or recreate the entry to make the test pass.
3. Launch the candidate, verify the same signed-in account is reused, then quit
   and restart. Neither launch may request Keychain access again while the login
   Keychain remains unlocked.

Local builds use ad-hoc signing: their designated requirement is tied to the
binary's code hash and can change after a rebuild. Published Developer ID builds
need a stable code identity across versions. Ad-hoc rebuilds do not prove this
upgrade behavior. See [Apple's code identity guidance](https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements).

This validation is pending the first published native upgrade; the local test
environment has no publisher Developer ID signing identity available.

## Verification recorded for this branch

On 2026-10-31, the replacement was built locally with Xcode 27.2 for arm64 and
x86_64, and the bundle signature was verified. Activity/Heat, Contributors,
Pull requests, and Settings were inspected in the isolated preview. A configured
build reused the existing `richhaase` Keychain sign-in, discovered installation
repositories, and saved a fresh single-repository workspace for
`richhaase/bigboard`. The live refresh loaded 187 commits and reported complete
PR inventory, files, and reviewers, with no data errors.

PR collection makes per-repository inventory requests with explicit request
bounds and coverage qualifications. Large workspaces may exhaust the request
budget before every repository is complete. Remote Git branch probes use
authenticated `ls-remote`.

Real private-repository failures, large-account performance, release signing,
notarization, and a new browser OAuth exchange still require joint testing.

Git operations isolate global/system Git configuration and inherited `GIT_*`
settings. Repository mailmap and native identity rules apply. Each command has
a 120-second deadline and 256 MiB output bound. Repository `.gitattributes`
input is limited to 4 MiB and 20,000 generated/vendored rules, with a shared
matching-work budget for each history scan. Cancellation and timeouts interrupt
parsing as well as Git. Exceeding a limit reports a scan error and retains stale
evidence when available; rules are never silently truncated or skipped.
Each repository scan also permits at most 50,000 commits, 100,000 file changes,
and 16 MiB of retained metadata and path text. Exceeding these limits rejects the
scan rather than presenting truncated history as complete. Parsed JSON caches
are limited to 16 MiB on read; oversized older caches are ignored and history is
parsed again under the current limits. A conservative size estimate bounds JSON
encoding before it starts. When the estimate exceeds the cache limit, the complete
scan can still be displayed without replacing the optional parsed cache.
The app supports its own complete bare caches, not arbitrary shallow/partial
caches or developer checkouts. The initial port verification covered 73 native tests and 12 packaging tests;
subsequent walkthrough fixes add focused repository-filter and settings tests.

The first usability pass adds a persistent multi-select repository popover,
a board-only sidebar, toolbar scope controls, footer data status, recent-push repository
sorting, and direct Apply with background refresh. The combined native suite
passes 86 tests after these changes; the optimized universal bundle builds and
passes local signature verification. Contributor rhythm and calendar behavior
were intentionally retained for further evaluation.
