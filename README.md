# Bigboard

A native macOS app for understanding activity across your GitHub repositories:
where change happened, who contributed, and which pull requests need attention.
Inspired by Hiro Protagonist’s Big Board in Neal Stephenson’s *Snow Crash*.

## Install

Requires **macOS 26 Tahoe or later** and Git, available through Homebrew or
Xcode Command Line Tools.

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

1. Open Bigboard and choose **Sign in with GitHub**.
2. Open **Settings → Repositories**. Use **Manage access…** to choose the
   repositories the GitHub App may read, then return and refresh the list.
3. Select the repositories to collect and click **Apply**.

Bigboard loads Git history and pull request evidence directly from GitHub.
Organization access policies still apply. Your sign-in is stored in Keychain,
and settings and cached history persist between launches and upgrades.
Git history uses app-owned caches; developer checkouts are untouched.

## Explore

| Board | Use it to |
| --- | --- |
| Activity | Follow changes through repositories, directories, files, commits, people, and related PRs. |
| Contributors | Explore each person's repository distribution, timelines, calendar, and recent commits. |
| Pull requests | Find open PRs, review requests, failing checks, and work needing your attention. |

Each board keeps its own search, sorting, and selection. The toolbar's repository
filter changes what you see; Settings controls what the app collects.
Heat uses a fixed historical window; other views follow the selected date range.
Activity describes recorded changes, not productivity.

See the [user reference](docs/reference.md) for workflows, keyboard shortcuts,
data interpretation, and troubleshooting.

## Develop

Bigboard uses Swift 6, SwiftUI, AppKit, and Charts. Development requires Xcode 26
or later. Start with the [development guide](native/README.md) to configure
GitHub sign-in and run `make dev`. Bigboard Dev has separate credentials and
data, so the installed release can remain open.

[Publisher setup](docs/github-app.md) covers GitHub App registration and release
credentials.

## License

[MIT](LICENSE)
