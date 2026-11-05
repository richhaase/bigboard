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
3. Open **Settings → Repositories**, select the repositories to collect, and
   click **Apply**.

Bigboard loads Git history and pull request evidence directly from GitHub using
the GitHub CLI token. Organization access policies, including SSO authorization
for the GitHub CLI, still apply. Bigboard stores no credentials of its own.
Settings and cached history persist between launches and upgrades. Git history
uses app-owned caches; developer checkouts are untouched.

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
bigboard activity          # recorded change from cached history
bigboard activity --days 30 --json
bigboard refresh           # fetch collected repositories
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
