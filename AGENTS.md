# Bigboard

Native Swift 6 app for macOS 26 Tahoe or later, using SwiftUI, AppKit, and Charts.

## Layout

- `Bigboard.xcodeproj`: app target and development/release schemes.
- `Package.swift`: application library and test targets.
- `Sources/BigboardApp`: interface, navigation, and workspace orchestration.
- `Sources/BigboardCore`: GitHub, authentication, Git history, analysis, and storage.
- `Tests`: native tests; `scripts`: app packaging and release checks.

## Working rules

- Follow native macOS conventions and preserve independent board state. Repository
  filters only affect viewing; Settings controls saved collection choices.
- Use atomic, account-scoped storage. Keep durable data in Application
  Support and rebuildable history in Caches. GitHub access comes only from the
  GitHub CLI (`gh auth token`); Bigboard stores no credentials. Operate only on
  app-owned Git caches, never developer checkouts.
- Preserve cancellation, access checks, identity rules, and honest stale/partial
  coverage. Update affected projections and tests when changing data models.
- Do not add source comments unless requested. Keep signing settings and
  tokens out of source control and tool output.
- Verify UI changes in the actual running build before reporting them complete.
  Use Bigboard Dev for local testing. Do not run multiple production-identity
  builds concurrently; they share data.

## Checks

Requires Xcode 26 or later, a usable Git installation, and a signed-in GitHub CLI.
Override `DEVELOPER_DIR` for Swift builds when needed.

```sh
make test
make check
make build
```

`make dev` builds and launches `build/Bigboard Dev.app` with separate data.
`make build` produces `build/Bigboard.app`. See the
[development guide](docs/development.md) for setup, verification, and release
credentials, and the [user reference](docs/reference.md) for behavior.
Publish only when requested, using the tag-triggered release workflow.
