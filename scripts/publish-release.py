import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile


class ReleaseError(Exception):
    pass


class GitHub:
    def __init__(self, repository):
        self.repository = repository

    def run(self, arguments, payload=None, output=None, missing_ok=False):
        try:
            result = subprocess.run(
                ["gh", *arguments], input=payload, stdout=output or subprocess.PIPE,
                stderr=subprocess.PIPE, check=False,
            )
        except OSError as error:
            raise ReleaseError("GitHub CLI could not be started.") from error
        if result.returncode:
            match = re.search(rb"HTTP (\d{3})", result.stderr)
            status = match.group(1).decode() if match else None
            if missing_ok and status == "404":
                return None
            detail = f" (HTTP {status})" if status else ""
            raise ReleaseError(f"GitHub request failed{detail}; no existing assets were replaced.")
        return result.stdout

    def api(self, method, endpoint, data=None, missing_ok=False):
        arguments = ["api", "--method", method, endpoint]
        payload = None
        if data is not None:
            arguments += ["--input", "-"]
            payload = json.dumps(data).encode()
        result = self.run(arguments, payload=payload, missing_ok=missing_ok)
        if result is None:
            return None
        try:
            return json.loads(result)
        except (ValueError, TypeError) as error:
            raise ReleaseError("GitHub returned an invalid response.") from error

    def download(self, asset, destination):
        with destination.open("wb") as output:
            self.run(["api", f"repos/{self.repository}/releases/assets/{asset['id']}",
                      "--header", "Accept: application/octet-stream"], output=output)

    def upload(self, tag, path):
        self.run(["release", "upload", tag, str(path), "--repo", self.repository])


class Publisher:
    def __init__(self, repository, tag, commit, output_dir, github=None):
        if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
            raise ReleaseError("Repository must be owner/name.")
        if not re.fullmatch(r"v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?", tag):
            raise ReleaseError("Release tag must be vMAJOR.MINOR.PATCH with an optional prerelease suffix.")
        if not re.fullmatch(r"[0-9a-f]{40}", commit):
            raise ReleaseError("Release commit must be a full lowercase commit SHA.")
        self.repository = repository
        self.base = f"repos/{repository}"
        self.tag = tag
        self.commit = commit
        self.version = tag[1:]
        self.prerelease = "-" in tag
        self.output_dir = Path(output_dir)
        self.output_dir.mkdir(parents=True, exist_ok=True)
        self.archive_name = f"Bigboard_{self.version}_darwin_universal.zip"
        self.archive = self.output_dir / self.archive_name
        self.checksums = self.output_dir / "checksums.txt"
        self.github = github or GitHub(repository)

    def verify_tag(self):
        reference = self.github.api("GET", f"{self.base}/git/ref/tags/{self.tag}")
        item = reference.get("object", {})
        for _ in range(8):
            if item.get("type") == "commit":
                if item.get("sha") != self.commit:
                    raise ReleaseError("Remote release tag no longer points to the verified commit.")
                return
            if item.get("type") != "tag" or not re.fullmatch(r"[0-9a-f]{40}", item.get("sha", "")):
                break
            item = self.github.api("GET", f"{self.base}/git/tags/{item['sha']}").get("object", {})
        raise ReleaseError("Remote release tag does not resolve to a commit.")

    def release(self):
        release = self.github.api("GET", f"{self.base}/releases/tags/{self.tag}", missing_ok=True)
        if release is None:
            page = 1
            while True:
                releases = self.github.api("GET", f"{self.base}/releases?per_page=100&page={page}")
                if not isinstance(releases, list):
                    raise ReleaseError("GitHub returned an invalid release listing.")
                matches = [item for item in releases if item.get("tag_name") == self.tag]
                if len(matches) > 1:
                    raise ReleaseError("Multiple releases use the requested tag.")
                if matches:
                    release_id = matches[0].get("id")
                    if not isinstance(release_id, int):
                        raise ReleaseError("Existing release metadata is incomplete.")
                    release = self.github.api("GET", f"{self.base}/releases/{release_id}")
                    break
                if len(releases) < 100:
                    break
                page += 1
        if release is not None:
            if release.get("tag_name") != self.tag or release.get("prerelease") != self.prerelease:
                raise ReleaseError("Existing release tag or prerelease status conflicts with this release.")
            if not isinstance(release.get("draft"), bool) or not isinstance(release.get("id"), int):
                raise ReleaseError("Existing release metadata is incomplete.")
        return release

    def assets(self, release):
        result = {}
        for asset in release.get("assets", []):
            if asset.get("name") in (self.archive_name, "checksums.txt"):
                if asset["name"] in result or not isinstance(asset.get("id"), int):
                    raise ReleaseError("Release contains conflicting asset names.")
                if asset.get("state", "uploaded") != "uploaded" or asset.get("size") == 0:
                    raise ReleaseError(f"Release asset {asset['name']} has an incomplete upload; it must be inspected before retrying.")
                result[asset["name"]] = asset
        return result

    @staticmethod
    def digest(path):
        result = hashlib.sha256()
        with path.open("rb") as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                result.update(chunk)
        return result.hexdigest()

    def checksum_text(self):
        if not self.archive.is_file() or self.archive.stat().st_size == 0:
            raise ReleaseError("The application ZIP is missing or empty.")
        return f"{self.digest(self.archive)}  {self.archive_name}\n"

    def check_checksum(self):
        try:
            current = self.checksums.read_text(encoding="utf-8")
        except (OSError, UnicodeError) as error:
            raise ReleaseError("Release checksums could not be read.") from error
        if current != self.checksum_text():
            raise ReleaseError("Application ZIP does not match checksums.txt; no assets were replaced.")

    def download(self, asset, target):
        with tempfile.TemporaryDirectory(prefix="bigboard-asset-", dir=self.output_dir) as temporary:
            incoming = Path(temporary) / "asset"
            self.github.download(asset, incoming)
            if target.exists():
                if not target.is_file() or self.digest(target) != self.digest(incoming):
                    raise ReleaseError(f"Existing {target.name} differs from the release asset; refusing to overwrite it.")
            else:
                incoming.replace(target)

    def prepare(self):
        self.verify_tag()
        release = self.release()
        if release is None:
            return True
        assets = self.assets(release)
        if not release["draft"] and set(assets) != {self.archive_name, "checksums.txt"}:
            raise ReleaseError("Published release is missing required assets; it will not be modified.")
        for name in (self.archive_name, "checksums.txt"):
            if name in assets:
                self.download(assets[name], self.output_dir / name)
        if self.archive_name not in assets:
            return True
        if "checksums.txt" in assets:
            self.check_checksum()
        else:
            self.checksums.write_text(self.checksum_text(), encoding="utf-8")
        return False

    def notes(self):
        generated = self.github.api("POST", f"{self.base}/releases/generate-notes", {
            "tag_name": self.tag, "target_commitish": self.commit,
        })
        body = generated.get("body")
        if not isinstance(body, str):
            raise ReleaseError("GitHub release notes could not be generated.")
        migration = ""
        if self.version == "2.0.0":
            migration = "\nThis native Swift replacement starts a fresh workspace without importing legacy settings or caches. Existing GitHub sign-in can be reused.\n"
        return (
            f"## Bigboard {self.tag}\n\n"
            "Native Bigboard for macOS 26 Tahoe or later. Apple Command Line Tools are required for repository history; install them with `xcode-select --install` if needed.\n"
            + migration + "\n" + body + "\n\n## Installation\n\n"
            "```sh\nbrew install --cask richhaase/tap/bigboard\n```\n\n"
            "Or download the universal macOS ZIP, extract it, and move Bigboard.app to Applications. "
            "Open Bigboard and sign in with GitHub. GitHub CLI is not required to use the app.\n"
        )

    def make_latest(self):
        if self.prerelease:
            return False
        latest = self.github.api("GET", f"{self.base}/releases/latest", missing_ok=True)
        if latest is None or latest.get("tag_name") == self.tag:
            return True
        current = latest.get("tag_name", "")
        if not re.fullmatch(r"v\d+\.\d+\.\d+", current):
            return False
        return tuple(map(int, self.version.split("."))) > tuple(map(int, current[1:].split(".")))

    def publish(self):
        self.verify_tag()
        checksum = self.checksum_text()
        if self.checksums.exists():
            self.check_checksum()
        else:
            self.checksums.write_text(checksum, encoding="utf-8")
        release = self.release()
        if release is None:
            release = self.github.api("POST", f"{self.base}/releases", {
                "tag_name": self.tag, "target_commitish": self.commit,
                "name": f"Bigboard {self.tag}", "body": self.notes(),
                "draft": True, "prerelease": self.prerelease, "make_latest": "false",
            })
            release = self.release()
            if release is None:
                raise ReleaseError("Draft release was not returned after creation.")
        assets = self.assets(release)
        if not release["draft"] and set(assets) != {self.archive_name, "checksums.txt"}:
            raise ReleaseError("Published release is missing required assets; it will not be modified.")
        for name in (self.archive_name, "checksums.txt"):
            if name in assets:
                self.download(assets[name], self.output_dir / name)
        for name in (self.archive_name, "checksums.txt"):
            if name not in assets and release["draft"]:
                self.github.upload(self.tag, self.output_dir / name)
        verified = self.release()
        if verified is None:
            raise ReleaseError("Release disappeared during asset verification.")
        assets = self.assets(verified)
        if set(assets) != {self.archive_name, "checksums.txt"}:
            raise ReleaseError("Required release assets were not uploaded.")
        for name in (self.archive_name, "checksums.txt"):
            self.download(assets[name], self.output_dir / name)
        self.check_checksum()
        self.verify_tag()
        if verified["draft"]:
            latest = self.make_latest()
            self.github.api("PATCH", f"{self.base}/releases/{verified['id']}", {
                "draft": False, "make_latest": "true" if latest else "false",
            })
        published = self.release()
        if published is None or published["draft"]:
            raise ReleaseError("Release has not been published.")
        self.update_tap = self.make_latest()
        return f"https://github.com/{self.repository}/releases/tag/{self.tag}"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("prepare", "publish"))
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("--repository", default="richhaase/bigboard")
    parser.add_argument("--github-output")
    arguments = parser.parse_args()
    try:
        publisher = Publisher(arguments.repository, arguments.tag, arguments.commit, arguments.output_dir)
        if arguments.command == "prepare":
            needs_build = publisher.prepare()
            value = f"needs_build={'true' if needs_build else 'false'}\n"
            if arguments.github_output:
                with open(arguments.github_output, "a", encoding="utf-8") as output:
                    output.write(value)
            print(value, end="")
        else:
            print(publisher.publish())
            if arguments.github_output:
                with open(arguments.github_output, "a", encoding="utf-8") as output:
                    output.write(f"update_tap={'true' if publisher.update_tap else 'false'}\n")
    except (ReleaseError, OSError) as error:
        print(f"Release failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
