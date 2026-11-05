# Bigboard

A native macOS app for understanding activity across your GitHub repositories:
where change happened, who contributed, and which pull requests need attention.
Inspired by Hiro Protagonist’s Big Board in Neal Stephenson’s *Snow Crash*.

## Install

Requires **macOS 26 Tahoe or later**, Git (Homebrew or Xcode Command Line
Tools), and the [GitHub CLI](https://cli.github.com) signed in with `gh auth login`.

```sh
brew install --cask richhaase/tap/bigboard
```

You can also download the universal macOS ZIP from
[Releases](https://github.com/richhaase/bigboard/releases/latest), extract it,
and move **Bigboard.app** to Applications.

To update a Homebrew installation, quit Bigboard and run:

```sh
brew update
brew upgrade --cask richhaase/tap/bigboard
```

## Get started

1. Run `gh auth login` in Terminal if you haven't already.
2. Open Bigboard. It uses the active GitHub CLI account.
3. Choose the folders that hold your Git clones. You can change them later in
   **Settings → Repositories**.
4. In **Settings → Repositories**, select the repositories to collect, and
   click **Apply**.

Bigboard reads Git history from your own clones in the source folders you
choose and loads pull request evidence from GitHub with the GitHub CLI token.
Refresh fetches only each clone's remote-tracking default branch, using your own
Git setup; it never changes your branches, index, or working files. Bigboard
stores no credentials of its own. Settings and parsed history
persist between launches and upgrades.

## Explore

| Board | Use it to |
| --- | --- |
| Heat Maps | See where and when activity occurred across the selected range. |
| Activity | Follow changes through repositories, directories, files, commits, people, and related PRs. |
| Contributors | Explore each person's repository distribution, timelines, calendar, and recent commits. |
| Pull requests | Find open PRs, review requests, failing checks, and work needing your attention. |

Each board keeps its own relevant controls and selection. The Repositories control
combines group selection and repository filtering. The historical boards share a date range, while
Pull requests shows current open work regardless of age. The toolbar's repository
filter changes what you see; Settings controls what the app collects.
Heat Maps shows activity across the selected range.
Activity, Contributors, and Pull requests provide the supporting detail.
Activity describes recorded changes, not productivity.

## Command line

The Homebrew cask also installs `bigboard`. Run it with no arguments to open the
app, or use a command:

```sh
bigboard status            # GitHub CLI account and collected repositories
bigboard activity          # recorded change in your clones' default branches
bigboard activity --days 30 --json
bigboard refresh           # fetch each clone's remote-tracking default branch
```

Commands use the same account, settings, and caches as the app. See the
[user reference](docs/reference.md#command-line) for options.

See the [user reference](docs/reference.md) for workflows, keyboard shortcuts,
data interpretation, and troubleshooting.

## Develop

Bigboard uses Swift 6, SwiftUI, AppKit, and Charts. Development requires Xcode 26
or later. Start with the [development guide](docs/development.md) and run
`make dev`. Bigboard Dev has separate data, so the installed release can remain
open.

## License

[MIT](LICENSE)
