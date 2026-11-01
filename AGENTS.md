# Bigboard

Native Swift 6 app for macOS 26 Tahoe or later, using SwiftUI, AppKit, and Charts.
There is no Go/web runtime or legacy data import.

## Layout

- `native/Sources/BigboardApp`: interface, navigation, and workspace orchestration.
- `native/Sources/BigboardCore`: GitHub, authentication, Git history, analysis, and storage.
- `native/Tests`: native tests; `scripts`: app packaging and release checks.

## Working rules

- Follow native macOS conventions and preserve independent board state. Repository
  filters only affect viewing; Settings controls saved collection choices.
- Use existing atomic, account-scoped storage. Keep durable data in Application
  Support, rebuildable history in Caches, and credentials in Keychain. Operate only
  on app-owned Git caches, never developer checkouts.
- Preserve cancellation, access checks, identity rules, and honest stale/partial
  coverage. Update affected projections and tests when changing data models.
- Do not add source comments unless requested. Keep publisher settings and
  credentials out of source control and tool output.
- Verify UI changes in the actual running build before reporting them complete.
  Do not run old and new apps concurrently with their shared rotating credential.

## Checks

Requires Xcode 26 or later and Apple Command Line Tools for Git history.
Override `DEVELOPER_DIR` for Swift builds when needed.

```sh
make test
make check
python3 -B -m unittest discover -s scripts -p '*_test.py'
make build
```

The app is built at `native/build/Bigboard.app`; `--ui-preview` uses isolated
fixtures. See the [native guide](native/README.md) for behavior and live checks,
and [publisher setup](docs/github-app.md) for authentication and signing.
Publish only when requested, using the existing tag-triggered release workflow.
