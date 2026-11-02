import json
from pathlib import Path
import re
import subprocess
import sys


def render_notes(tag, previous_tag, commits):
    groups = [("Features", r"^feat"), ("Bug Fixes", r"^fix"), ("Security", r"^sec"),
              ("Breaking Changes", r"^.*!:"), ("Other Changes", r"")]
    entries = {title: [] for title, _ in groups}
    for commit in sorted(commits, key=lambda item: item["commit"]["message"].split("\n", 1)[0]):
        subject = commit["commit"]["message"].split("\n", 1)[0]
        if re.match(r"^(docs|test|chore|style|refactor|perf|ci):", subject):
            continue
        author = commit.get("author") or {}
        credit = f"@{author['login']}" if author.get("login") else f"{author.get('name') or ''} <{author.get('email') or ''}>"
        title = next(title for title, pattern in groups if re.match(pattern, subject))
        entries[title].append(f"* {commit['sha']}: {subject} ({credit})")
    changelog = "\n".join("### " + title + "\n" + "\n".join(lines) for title, lines in entries.items() if lines)
    return f"""## Big Board {tag}

Bigboard for macOS: explore work areas, contributors, and commit evidence across GitHub repositories.

### GitHub sign-in
Sign in with GitHub from Bigboard and select repository access in your browser.
GitHub CLI is no longer required. Organization access policies still apply.

## Changelog
{changelog}

---

**Full Changelog**: https://github.com/richhaase/bigboard/compare/{previous_tag}...{tag}

## Installation

### Homebrew
```bash
brew install --cask richhaase/tap/bigboard
```

### Direct Download
Download the universal macOS archive, extract it, and move Bigboard.app to Applications.
Git is required for repository history. Open Bigboard and choose Sign in with GitHub.

"""


if __name__ == "__main__":
    tag = sys.argv[1]
    repo_root = Path(__file__).resolve().parent.parent
    previous_tag = subprocess.check_output(
        ["git", "describe", "--tags", "--abbrev=0", f"{tag}^"], cwd=repo_root, text=True
    ).strip()
    pages = json.loads(subprocess.check_output(
        ["gh", "api", "--paginate", "--slurp", f"repos/richhaase/bigboard/compare/{previous_tag}...{tag}?per_page=100"], text=True
    ))
    sys.stdout.write(render_notes(tag, previous_tag, [commit for page in pages for commit in page["commits"]]))
