# Bigboard

A native macOS app for understanding activity across your GitHub repositories:
where change happened, who contributed, and which pull requests need attention.
Inspired by Hiro Protagonist’s Big Board in Neal Stephenson’s *Snow Crash*.

Bigboard is built with Swift, SwiftUI, AppKit, and Charts.
It requires **macOS 26 Tahoe or later** and a usable Git installation (Homebrew or Xcode Git)
for Git history. The app talks directly
to GitHub and maintains owned bare Git caches; developer checkouts are untouched.

## Build and run

Building requires Xcode 26 or later with the macOS 26 SDK or newer.

```sh
make dev
```

This builds and opens `native/build/Bigboard Dev.app`, a locally signed app with
its own sign-in, settings, and caches. Quit Bigboard Dev before rebuilding and
running `make dev` again; its state persists across rebuilds. The installed
release can remain open because it uses separate credentials and local data.
The command does not install or publish the app.
`make test` runs the native test suite; `make check` also builds optimized code.
Open `native/Package.swift` in Xcode to develop or debug the app.

For live sign-in, add `com.richhaase.bigboard.dev://oauth/github` as a second
callback URL on the existing publisher GitHub App, retaining the release callback
and disabling wildcard matching. Reuse its publisher settings through the ignored
`native/publisher.local.json` or build environment, then sign in once in Bigboard
Dev and select your normal repositories. See the [local development setup](native/README.md#local-development).
Publisher settings are build inputs, not fields end users fill in; keep them out
of source control and chat. A build without them opens with sign-in unavailable.

`make build` still packages `native/build/Bigboard.app` with the production
identity. Keep the installed release closed when running that production-identity
local bundle because they share credentials and data.

## Workflows

| Board | Explore |
| --- | --- |
| Activity | Repository volume, Heat trends, directories and files, scoped commits, people, and related PRs. |
| Contributors | Repository distribution, timelines, activity calendars, and recent commits for each person. |
| Pull requests | Open PRs, review/check state, individual review requests, and work needing your attention. |

Date and repository viewing scope apply across boards. Each board retains its
own search, sort, and selection. **Repository filter** shows all loaded repositories
or a selected subset across all three boards. It changes only what you see,
without refreshing data or saving collection choices.

**Settings** manages saved repository collection choices, named groups, work
areas, startup defaults, advanced counting rules, and offline mode. Eligible local caches can be removed
under **Settings → Storage**. Identities merges email aliases without merging
people merely because they share a display name.

The sidebar contains Activity, Contributors, and Pull requests. Date and repository
filters sit in the toolbar; the repository picker stays open for multiple selections.
Workspace Settings is in the app menu, Keyboard Guide is under Help, and Data
Status opens by clicking the status and timestamp together at the bottom right.
That footer control shows a spinner during refresh; progress also remains
visible at the top of the main screen.

The app uses native tables, inspectors, sheets, menus,
clipboard actions, and Save dialogs. Activity is evidence, not a productivity
score or a judgment that a PR is ready to merge.

## Fresh native workspace

There is **no legacy import**. Choose repositories again in the native app.

- Durable workspace settings, access grants, and bookmarks: `~/Library/Application Support/com.richhaase.bigboard.native/accounts/<account-id>/`.
- Rebuildable Git history and parsed scans: `~/Library/Caches/com.richhaase.bigboard.native/accounts/<account-id>/repos/`.
- Credentials: the existing non-synchronizing Bigboard GitHub Keychain item.

These are the production locations. Bigboard Dev uses
`com.richhaase.bigboard.dev.native` instead of `com.richhaase.bigboard.native`
for both data directories and its own GitHub Keychain service. It starts with a
fresh sign-in and workspace; production tokens and state are never copied.
macOS may ask for Keychain permission after a locally signed rebuild.

See the [workflow guide](docs/visual-guide.md) for a first session, the
[user reference](docs/reference.md) for controls and data semantics, and the
[native development and verification guide](native/README.md) for the
architecture, behavior contract, and live acceptance checks. Previous
implementations remain available in Git history.

## License

[MIT](LICENSE)
