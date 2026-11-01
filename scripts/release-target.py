import argparse
import re
import subprocess
from pathlib import Path


def resolve(tag):
    if not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?", tag):
        raise ValueError("Expected an existing vX.Y.Z release tag, optionally with a prerelease suffix.")
    commit = subprocess.check_output(["git", "rev-parse", "--verify", f"refs/tags/{tag}^{{commit}}"], text=True).strip()
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("Release tag did not resolve to a commit.")
    subprocess.run(["git", "merge-base", "--is-ancestor", commit, "refs/remotes/origin/main"], check=True)
    return commit


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("tag")
    parser.add_argument("--github-output", type=Path, required=True)
    args = parser.parse_args()
    try:
        commit = resolve(args.tag)
    except (ValueError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"Cannot release tag: {error}") from error
    with args.github_output.open("a") as output:
        output.write(f"commit={commit}\n")
    print(f"Release {args.tag} builds exact source commit {commit}.")


if __name__ == "__main__":
    main()
