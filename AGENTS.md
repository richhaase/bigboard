# Bigboard

Native Swift 6 app for macOS 26 Tahoe or later, using SwiftUI, AppKit, and Charts.

## Layout

- `native/Bigboard.xcodeproj`: app target and development/release schemes.
- `native/Package.swift`: application library and test targets.
- `native/Sources/BigboardApp`: interface, navigation, and workspace orchestration.
- `native/Sources/BigboardCore`: GitHub, authentication, Git history, analysis, and storage.
- `native/Tests`: native tests; `scripts`: app packaging and release checks.

## Working rules

- Follow native macOS conventions and preserve independent board state. Repository
  filters only affect viewing; Settings controls saved collection choices.
- Use atomic, account-scoped storage. Keep durable data in Application
  Support, rebuildable history in Caches, and credentials in Keychain. Operate only
  on app-owned Git caches, never developer checkouts.
- Preserve cancellation, access checks, identity rules, and honest stale/partial
  coverage. Update affected projections and tests when changing data models.
- Do not add source comments unless requested. Keep publisher settings and
  credentials out of source control and tool output.
- Verify UI changes in the actual running build before reporting them complete.
  Use Bigboard Dev for local testing. Do not run multiple production-identity
  builds concurrently; they share rotating credentials and data.

## Checks

Requires Xcode 26 or later and a usable Git installation for Git history.
Override `DEVELOPER_DIR` for Swift builds when needed.

```sh
make test
make check
make build
```

`make dev` builds and launches `native/build/Bigboard Dev.app` with separate
credentials and data. `make build` produces `native/build/Bigboard.app`.
See the [development guide](native/README.md) for setup and verification, the
[user reference](docs/reference.md) for behavior, and
[publisher setup](docs/github-app.md) for authentication and signing inputs.
Publish only when requested, using the tag-triggered release workflow.
