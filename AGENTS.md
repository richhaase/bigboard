# Bigboard

Native Swift 6 app for macOS 26 Tahoe or later, using SwiftUI, AppKit, and Charts.

## Layout

- `Bigboard.xcodeproj`: app target and development/release schemes.
- `Package.swift`: application library and test targets.
- `Sources/BigboardApp`: interface, navigation, and workspace orchestration.
- `Sources/BigboardCLI`: the `bigboard` command, dispatched from the app executable.
- `Sources/BigboardCore`: GitHub, authentication, Git history, analysis, and storage.
- `Tests`: native tests; `scripts`: app packaging and release checks.

## MCP app plugin

The MCP server, web app, and `bigboard:explore` and `bigboard:configure` skills
have moved to [toolshed](https://github.com/richhaase/toolshed/tree/main/plugins/bigboard).
Make plugin changes in `plugins/bigboard/` in that repository, following its
instructions. The marketplace identity is `bigboard@toolshed`; the existing
`bigboard-mcp-app` state directory remains unchanged to preserve saved sources
and boards. This repository owns the native macOS app.

`codex/bigboard-mcp-app` retains the rebuild's development history. Keep `main`
as the native app baseline until Rich requests a merge.

## Working rules

- Follow native macOS conventions and preserve independent board state. Repository
  filters only affect viewing; Settings controls saved collection choices.
- Use atomic storage under the XDG base directories: one settings file in
  `$XDG_CONFIG_HOME/bigboard` and rebuildable history in `$XDG_CACHE_HOME/bigboard`
  (`bigboard-dev` for Bigboard Dev). GitHub API access comes only from the
  GitHub CLI (`gh auth token`); Bigboard stores no credentials. History comes from
  the user's own clones, fetched with the user's own Git setup: only fetch into
  `refs/remotes/<remote>/<default branch>`, never prompt, and never write
  branches, the index, the working tree, or config, or run gc or other
  maintenance in them.
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
