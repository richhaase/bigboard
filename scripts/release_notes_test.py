import unittest

from release_notes import render_notes


def commit(subject, sha="a" * 40, author=None):
    return {"sha": sha, "commit": {"message": subject}, "author": author or {"login": "fixture"}}


class ReleaseNotesTests(unittest.TestCase):
    def test_original_groups_sorting_filters_and_installation_notes(self):
        commits = [
            commit("fix: Zebra\n\nCommit body"),
            commit("change!: incompatible"),
            commit("sec: protect credentials"),
            commit("feat!: feature"),
            commit("docs(scope): retained"),
            commit("fix: Apple", "b" * 40, {"name": "Contributor", "email": "person@example.com"}),
            *[commit(prefix + ": excluded") for prefix in ("docs", "test", "chore", "style", "refactor", "perf", "ci")],
        ]
        self.assertEqual(render_notes("v2.0.2", "v2.0.1", commits), """## Big Board v2.0.2

Bigboard for macOS: explore work areas, contributors, and commit evidence across GitHub repositories.

### GitHub sign-in
Sign in with GitHub from Bigboard and select repository access in your browser.
GitHub CLI is no longer required. Organization access policies still apply.

## Changelog
### Features
* aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: feat!: feature (@fixture)
### Bug Fixes
* bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb: fix: Apple (Contributor <person@example.com>)
* aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: fix: Zebra (@fixture)
### Security
* aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: sec: protect credentials (@fixture)
### Breaking Changes
* aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: change!: incompatible (@fixture)
### Other Changes
* aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: docs(scope): retained (@fixture)

---

**Full Changelog**: https://github.com/richhaase/bigboard/compare/v2.0.1...v2.0.2

## Installation

### Homebrew
```bash
brew install --cask richhaase/tap/bigboard
```

### Direct Download
Download the universal macOS archive, extract it, and move Bigboard.app to Applications.
Git is required for repository history. Open Bigboard and choose Sign in with GitHub.

""")

    def test_empty_groups_are_omitted(self):
        notes = render_notes("v2.0.2", "v2.0.1", [commit("fix: bug"), commit("docs: excluded")])
        self.assertIn("### Bug Fixes\n", notes)
        for title in ("Features", "Security", "Breaking Changes", "Other Changes"):
            self.assertNotIn("### " + title, notes)
