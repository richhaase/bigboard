# Bigboard

Native Swift 6 app for macOS 26 Tahoe or later, using SwiftUI, AppKit, and Charts.

## Layout

- `Bigboard.xcodeproj`: app target and development/release schemes.
- `Package.swift`: application library and test targets.
- `Sources/BigboardApp`: interface, navigation, and workspace orchestration.
- `Sources/BigboardCLI`: the `bigboard` command, dispatched from the app executable.
- `Sources/BigboardCore`: GitHub, authentication, Git history, analysis, and storage.
- `Tests`: native tests; `scripts`: app packaging and release checks.
- `plugin/`: Git/GitHub MCP server, web app, packaged skill, and committed
  installable bundles. See [the plugin guide](plugin/README.md).

## MCP app integration branch

`codex/bigboard-mcp-app` is the integration baseline for the MCP rebuild, with
the managed worktree named `bigboard-mcp-app`. Base follow-up MCP work on this
branch and preserve its history with follow-up commits. Keep `main` as the
native app baseline until Rich requests a merge.

The `bigboard-mcp` repo marketplace tracks this branch when installed with the
guide's commands. For each installable change, bump `plugin/plugin.json` and
`plugin/package.json` together, refresh the lockfile and bundles, and run the
plugin checks before pushing. Run Node commands from `plugin/` with `fnm exec`
to use its `.node-version`. Bundles are committed so installing the plugin
does not require a compiler, dependency download, or install script.

The plugin uses real local Git history and GitHub PR data. Source settings,
collected evidence, and boards live separately from native data. Importing the
native repository selection is read-only. Collection never fetches or modifies
clones. Activating the Pull requests view refreshes sources automatically;
other views use manual refresh. Viewing filters and polling read completed
snapshots. Preserve per-repository coverage, account access invalidation, and
snapshot-bound evidence paging. Verify protocol and rendered UI with plugin
tests; record desktop-host interaction separately with the guide's checklist.

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
