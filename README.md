# Bigboard

A native macOS app for understanding activity across your GitHub repositories:
where change happened, who contributed, and which pull requests need attention.
Inspired by Hiro Protagonist’s Big Board in Neal Stephenson’s *Snow Crash*.

This branch replaces the Go/Wails app with Swift, SwiftUI, AppKit, and Charts.
It requires **macOS 27**, Xcode 27, and Git 2.31 or newer. The app talks directly
to GitHub and maintains owned bare Git caches; developer checkouts are untouched.

## Build and run

```sh
make build
open native/build/Bigboard.app
```

The output is a locally signed development app. It is not installed or published.
`make test` runs the native test suite; `make check` also builds optimized code.
Open `native/Package.swift` in Xcode to develop or debug the app.

Live sign-in needs the publisher's existing `BIGBOARD_GITHUB_CLIENT_ID`,
`BIGBOARD_GITHUB_CLIENT_SECRET`, and `BIGBOARD_GITHUB_APP_SLUG` in the build
process environment. See [publisher setup](docs/github-app.md). These are build
settings, not fields end users fill in. Do not put them in source control or chat.
A build without them opens normally and explains why sign-in is unavailable.

An explicit UI preview is available for visual checks without GitHub or Keychain:

```sh
open -n native/build/Bigboard.app --args --ui-preview
```

Preview data is labeled and used only with that argument. It never substitutes
for missing live data.

## Workflows

| Board | Explore |
| --- | --- |
| Activity | Repository volume, Heat trends, directories and files, scoped commits, people, and related PRs. |
| Contributors | Repository distribution, timelines, activity calendars, and recent commits for each person. |
| Pull requests | Open PRs, review/check state, individual review requests, and work needing your attention. |

Date and repository viewing scope apply across boards. Each board retains its
own search, sort, and selection. Settings manages tracked repositories, named
groups, work areas, analysis rules, and offline mode. Repository controls manages
session tracking and eligible cache removal. Identities merges email aliases
without merging people merely because they share a display name.

The app uses native sidebar navigation, tables, inspectors, sheets, menus,
clipboard actions, and Save dialogs. Activity is evidence, not a productivity
score or a judgment that a PR is ready to merge.

## Fresh native workspace

There is **no legacy import**. Choose repositories again in the native app.

- Durable workspace settings, access grants, and bookmarks: `~/Library/Application Support/com.richhaase.bigboard.native/accounts/<account-id>/`.
- Rebuildable Git history and parsed scans: `~/Library/Caches/com.richhaase.bigboard.native/accounts/<account-id>/repos/`.
- Credentials: the existing non-synchronizing Bigboard GitHub Keychain item.

The replacement can reuse the existing sign-in when built with the same publisher
settings. Keep the old app closed while testing: both versions share rotating
credentials, and signing out removes the shared local credential. macOS may ask
you to permit Keychain access because the development app has a different signature.
Native settings and caches remain separate from the old app's files.

See the [native development and verification guide](native/README.md) for the
architecture, behavior contract, and live acceptance checks. The old Go and web
sources remain in this worktree as a reference; they are not linked into the
native app. Their tests can be run with `make legacy-test`.

## License

[MIT](LICENSE)
