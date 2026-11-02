import base64
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("publish-release.py")
SPEC = importlib.util.spec_from_file_location("bigboard_publisher", SCRIPT)
PUBLISHER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PUBLISHER)
COMMIT = "a" * 40
TAG_OBJECT = "b" * 40


class FakeGitHub:
    def __init__(self):
        self.reference = {"object": {"type": "tag", "sha": TAG_OBJECT}}
        self.tag = {"object": {"type": "commit", "sha": COMMIT}}
        self.current = None
        self.latest = None
        self.contents = {}
        self.events = []
        self.next_asset = 1
        self.fail_upload = None
        self.fail_lookup = False

    def api(self, method, endpoint, data=None, missing_ok=False):
        self.events.append((method, endpoint, copy.deepcopy(data)))
        if endpoint.endswith("/git/ref/tags/v2.0.0") or "/git/ref/tags/" in endpoint:
            return copy.deepcopy(self.reference)
        if endpoint.endswith(f"/git/tags/{TAG_OBJECT}"):
            return copy.deepcopy(self.tag)
        if "/releases/tags/" in endpoint:
            if self.fail_lookup:
                raise PUBLISHER.ReleaseError("GitHub request failed (HTTP 503).")
            return copy.deepcopy(self.current) if self.current and not self.current["draft"] else None
        if "/releases?per_page=100&page=" in endpoint:
            return [copy.deepcopy(self.current)] if self.current and endpoint.endswith("page=1") else []
        if endpoint.endswith("/releases/17") and method == "GET":
            return copy.deepcopy(self.current)
        if endpoint.endswith("/releases/latest"):
            return copy.deepcopy(self.latest)
        if endpoint.endswith("/releases/generate-notes"):
            return {"body": "Changes from the native app."}
        if endpoint.endswith("/releases") and method == "POST":
            self.current = {"id": 17, "tag_name": data["tag_name"], "draft": data["draft"],
                            "prerelease": data["prerelease"], "assets": []}
            return copy.deepcopy(self.current)
        if endpoint.endswith("/releases/17") and method == "PATCH":
            self.current["draft"] = data["draft"]
            if data.get("make_latest") == "true":
                self.latest = copy.deepcopy(self.current)
            return copy.deepcopy(self.current)
        raise AssertionError((method, endpoint, data))

    def add_asset(self, name, content):
        item = {"id": self.next_asset, "name": name, "size": len(content)}
        self.next_asset += 1
        self.current["assets"].append(item)
        self.contents[item["id"]] = content

    def download(self, asset, destination):
        self.events.append(("download", asset["name"], None))
        destination.write_bytes(self.contents[asset["id"]])

    def upload(self, tag, path):
        self.events.append(("upload", path.name, None))
        if self.fail_upload == path.name:
            raise PUBLISHER.ReleaseError("Simulated upload interruption.")
        self.add_asset(path.name, path.read_bytes())


class PublisherTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="bigboard-publisher-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.github = FakeGitHub()
        self.publisher = PUBLISHER.Publisher("richhaase/bigboard", "v2.0.0", COMMIT, self.root, self.github)
        self.archive = b"signed archive fixture"

    def seed_release(self, draft=True, archive=True, checksums=True, tag="v2.0.0"):
        self.github.current = {"id": 17, "tag_name": tag, "draft": draft,
                               "prerelease": "-" in tag, "assets": []}
        if archive:
            self.github.add_asset(self.publisher.archive_name, self.archive)
        if checksums:
            digest = hashlib.sha256(self.archive).hexdigest()
            self.github.add_asset("checksums.txt", f"{digest}  {self.publisher.archive_name}\n".encode())

    def mutations(self):
        return [item for item in self.github.events if item[0] in ("POST", "PATCH", "upload")]

    def test_first_publish_verifies_assets_before_releasing_draft(self):
        self.assertTrue(self.publisher.prepare())
        self.assertEqual(self.mutations(), [])
        self.publisher.archive.write_bytes(self.archive)
        self.assertEqual(self.publisher.publish(), "https://github.com/richhaase/bigboard/releases/tag/v2.0.0")
        self.assertFalse(self.github.current["draft"])
        self.assertTrue(self.publisher.update_tap)
        uploads = [event[1] for event in self.github.events if event[0] == "upload"]
        self.assertEqual(uploads, [self.publisher.archive_name, "checksums.txt"])
        publish = next(index for index, event in enumerate(self.github.events) if event[0] == "PATCH")
        for name in uploads:
            self.assertTrue(any(event[:2] == ("download", name) for event in self.github.events[:publish]))
        create = next(event[2] for event in self.github.events if event[:2] == ("POST", "repos/richhaase/bigboard/releases"))
        self.assertTrue(create["draft"])
        self.assertEqual(create["target_commitish"], COMMIT)
        self.assertIn("macOS 26", create["body"])
        self.assertIn("without importing legacy", create["body"])
        self.assertIn("Homebrew or Xcode Command Line Tools", create["body"])

    def test_published_retry_downloads_verified_assets_without_mutation(self):
        self.seed_release(draft=False)
        self.assertFalse(self.publisher.prepare())
        self.assertEqual(self.publisher.archive.read_bytes(), self.archive)
        self.publisher.publish()
        self.assertEqual(self.mutations(), [])
        self.assertTrue(self.publisher.update_tap)

    def test_upload_interruption_resumes_missing_checksum_only(self):
        self.publisher.archive.write_bytes(self.archive)
        self.github.fail_upload = "checksums.txt"
        with self.assertRaisesRegex(PUBLISHER.ReleaseError, "interruption"):
            self.publisher.publish()
        self.assertTrue(self.github.current["draft"])
        self.assertEqual([asset["name"] for asset in self.github.current["assets"]], [self.publisher.archive_name])
        self.publisher.archive.unlink()
        self.publisher.checksums.unlink()
        self.github.fail_upload = None
        self.github.events = []
        self.assertFalse(self.publisher.prepare())
        self.publisher.publish()
        self.assertEqual([event[1] for event in self.github.events if event[0] == "upload"], ["checksums.txt"])
        self.assertFalse(self.github.current["draft"])

    def test_complete_draft_is_published_without_reupload(self):
        self.seed_release()
        self.assertFalse(self.publisher.prepare())
        self.publisher.publish()
        self.assertEqual([event[0] for event in self.mutations()], ["PATCH"])

    def test_checksum_only_draft_requires_matching_build(self):
        self.seed_release(archive=False)
        self.assertTrue(self.publisher.prepare())
        self.publisher.archive.write_bytes(b"different signed build")
        with self.assertRaisesRegex(PUBLISHER.ReleaseError, "does not match"):
            self.publisher.publish()
        self.assertEqual(self.mutations(), [])
        self.publisher.archive.write_bytes(self.archive)
        self.publisher.publish()
        self.assertEqual([event[1] for event in self.github.events if event[0] == "upload"], [self.publisher.archive_name])

    def test_conflicting_existing_archive_is_never_overwritten(self):
        self.seed_release()
        self.publisher.archive.write_bytes(b"a different local build")
        with self.assertRaisesRegex(PUBLISHER.ReleaseError, "refusing to overwrite"):
            self.publisher.prepare()
        self.assertEqual(self.publisher.archive.read_bytes(), b"a different local build")
        self.assertEqual(self.mutations(), [])

    def test_publish_checks_existing_checksum_before_uploading_missing_archive(self):
        self.seed_release(archive=False)
        self.publisher.archive.write_bytes(b"a different local build")
        with self.assertRaisesRegex(PUBLISHER.ReleaseError, "refusing to overwrite"):
            self.publisher.publish()
        self.assertEqual(self.mutations(), [])

    def test_bad_remote_checksum_does_not_publish(self):
        self.seed_release()
        checksum = next(asset for asset in self.github.current["assets"] if asset["name"] == "checksums.txt")
        self.github.contents[checksum["id"]] = b"invalid\n"
        with self.assertRaisesRegex(PUBLISHER.ReleaseError, "does not match"):
            self.publisher.prepare()
        self.assertEqual(self.mutations(), [])

    def test_published_release_missing_asset_is_not_repaired(self):
        for archive in (False, True):
            with self.subTest(archive=archive):
                self.seed_release(draft=False, archive=archive, checksums=not archive)
                with self.assertRaisesRegex(PUBLISHER.ReleaseError, "missing required assets"):
                    self.publisher.prepare()
                self.assertEqual(self.mutations(), [])

    def test_moved_remote_tag_fails_before_release_access(self):
        self.github.tag["object"]["sha"] = "c" * 40
        with self.assertRaisesRegex(PUBLISHER.ReleaseError, "no longer points"):
            self.publisher.prepare()
        self.assertFalse(any("/releases" in item[1] for item in self.github.events))

    def test_lightweight_tag_resolves_verified_commit(self):
        self.github.reference = {"object": {"type": "commit", "sha": COMMIT}}
        self.assertTrue(self.publisher.prepare())

    def test_tag_movement_before_publish_keeps_draft(self):
        self.seed_release()
        self.assertFalse(self.publisher.prepare())
        original = self.publisher.verify_tag
        calls = 0
        def move_after_first_check():
            nonlocal calls
            calls += 1
            if calls > 1:
                self.github.tag["object"]["sha"] = "c" * 40
            original()
        self.publisher.verify_tag = move_after_first_check
        with self.assertRaisesRegex(PUBLISHER.ReleaseError, "no longer points"):
            self.publisher.publish()
        self.assertTrue(self.github.current["draft"])
        self.assertEqual(self.mutations(), [])

    def test_api_failure_is_not_treated_as_no_release(self):
        self.github.fail_lookup = True
        with self.assertRaisesRegex(PUBLISHER.ReleaseError, "503"):
            self.publisher.prepare()
        self.assertEqual(self.mutations(), [])

    def test_draft_lookup_paginates_and_fetches_release_by_id(self):
        self.seed_release()
        original = self.github.api
        def paginated(method, endpoint, data=None, missing_ok=False):
            if endpoint.endswith("/releases?per_page=100&page=1"):
                return [{"id": number, "tag_name": f"v1.0.{number}"} for number in range(100)]
            if endpoint.endswith("/releases?per_page=100&page=2"):
                return [{"id": 17, "tag_name": "v2.0.0"}]
            return original(method, endpoint, data, missing_ok)
        self.github.api = paginated
        self.assertFalse(self.publisher.prepare())
        self.assertTrue(any(event[:2] == ("GET", "repos/richhaase/bigboard/releases/17") for event in self.github.events))
        self.assertEqual(self.mutations(), [])

    def test_draft_listing_failure_does_not_create_another_release(self):
        original = self.github.api
        def failed_listing(method, endpoint, data=None, missing_ok=False):
            if "/releases?" in endpoint:
                raise PUBLISHER.ReleaseError("GitHub request failed (HTTP 503).")
            return original(method, endpoint, data, missing_ok)
        self.github.api = failed_listing
        self.publisher.archive.write_bytes(self.archive)
        with self.assertRaisesRegex(PUBLISHER.ReleaseError, "503"):
            self.publisher.publish()
        self.assertEqual(self.mutations(), [])

    def test_prerelease_is_neither_latest_nor_tap_update(self):
        self.publisher = PUBLISHER.Publisher("richhaase/bigboard", "v2.1.0-beta.1", COMMIT, self.root, self.github)
        self.publisher.archive.write_bytes(self.archive)
        self.publisher.publish()
        self.assertTrue(self.github.current["prerelease"])
        self.assertFalse(self.publisher.update_tap)
        self.assertEqual(next(event[2] for event in self.github.events if event[0] == "PATCH")["make_latest"], "false")

    def test_older_stable_retry_does_not_downgrade_latest_or_tap(self):
        self.seed_release()
        self.github.latest = {"tag_name": "v2.1.0"}
        self.publisher.prepare()
        self.publisher.publish()
        self.assertFalse(self.publisher.update_tap)
        self.assertEqual(self.github.latest["tag_name"], "v2.1.0")
        self.assertEqual(next(event[2] for event in self.github.events if event[0] == "PATCH")["make_latest"], "false")

    def test_existing_prerelease_status_conflict_fails(self):
        self.seed_release()
        self.github.current["prerelease"] = True
        with self.assertRaisesRegex(PUBLISHER.ReleaseError, "prerelease status conflicts"):
            self.publisher.prepare()
        self.assertEqual(self.mutations(), [])

    def test_duplicate_expected_asset_names_fail(self):
        self.seed_release()
        self.github.add_asset(self.publisher.archive_name, self.archive)
        with self.assertRaisesRegex(PUBLISHER.ReleaseError, "conflicting asset names"):
            self.publisher.prepare()

    def test_incomplete_upload_fails_without_deleting_or_replacing_assets(self):
        for metadata in ({"state": "starter"}, {"size": 0}):
            with self.subTest(metadata=metadata):
                self.seed_release(checksums=False)
                self.github.current["assets"][0].update(metadata)
                with self.assertRaisesRegex(PUBLISHER.ReleaseError, "incomplete upload"):
                    self.publisher.prepare()
                self.assertEqual(self.mutations(), [])

    def test_invalid_identifiers_fail_before_network(self):
        for tag, commit in (("main", COMMIT), ("v2.0.0;echo bad", COMMIT), ("v02.0.0", COMMIT), ("v2.0.0", "short")):
            with self.subTest(tag=tag, commit=commit):
                with self.assertRaises(PUBLISHER.ReleaseError):
                    PUBLISHER.Publisher("richhaase/bigboard", tag, commit, self.root, self.github)
        self.assertEqual(self.github.events, [])

    def test_github_only_accepts_explicit_404_as_missing_and_redacts_errors(self):
        client = PUBLISHER.GitHub("richhaase/bigboard")
        with patch.object(PUBLISHER.subprocess, "run", return_value=subprocess.CompletedProcess([], 1, b"", b"gh: Not Found (HTTP 404)")):
            self.assertIsNone(client.api("GET", "fixture", missing_ok=True))
        for error in (b"HTTP 403 secret-token", b"network error secret-token", b"HTTP 503 secret-token"):
            with self.subTest(error=error):
                with patch.object(PUBLISHER.subprocess, "run", return_value=subprocess.CompletedProcess([], 1, b"", error)):
                    with self.assertRaises(PUBLISHER.ReleaseError) as raised:
                        client.api("GET", "fixture", missing_ok=True)
                    self.assertNotIn("secret-token", str(raised.exception))

    def test_prepare_cli_works_with_no_go_in_path(self):
        binaries = self.root / "bin"
        binaries.mkdir()
        gh = binaries / "gh"
        gh.write_text(
            f"#!{sys.executable}\n"
            "import json, sys\n"
            f"commit = {COMMIT!r}\n"
            "if '/git/ref/tags/' in sys.argv[4]:\n"
            " print(json.dumps({'object': {'type': 'commit', 'sha': commit}}))\n"
            "elif '/releases?' in sys.argv[4]:\n"
            " print('[]')\n"
            "else:\n"
            " print('gh: Not Found (HTTP 404)', file=sys.stderr)\n"
            " sys.exit(1)\n"
        )
        gh.chmod(0o755)
        env = {**os.environ, "PATH": str(binaries)}
        output = self.root / "outputs.txt"
        result = subprocess.run([sys.executable, str(SCRIPT), "prepare", "--tag", "v2.0.0", "--commit", COMMIT,
                                 "--output-dir", str(self.root / "artifacts"), "--github-output", str(output)],
                                env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output.read_text(), "needs_build=true\n")
        self.assertEqual(result.stdout, "needs_build=true\n")

    def test_publish_cli_uploads_and_verifies_with_no_go_in_path(self):
        binaries = self.root / "bin"
        binaries.mkdir()
        self.publisher.archive.write_bytes(self.archive)
        checksum = self.publisher.checksum_text().encode()
        base = "repos/richhaase/bigboard"
        reference = {"object": {"type": "commit", "sha": COMMIT}}
        draft = {"id": 17, "tag_name": "v2.0.0", "draft": True, "prerelease": False, "assets": []}
        complete = {**draft, "assets": [{"id": 1, "name": self.publisher.archive_name}, {"id": 2, "name": "checksums.txt"}]}
        published = {**complete, "draft": False}
        responses = []
        def api(method, endpoint, response, status=0, payload=None):
            command = ["api", "--method", method, endpoint]
            if method != "GET":
                command += ["--input", "-"]
            responses.append({"command": command, "stdout": json.dumps(response), "status": status, "payload": payload})
        api("GET", f"{base}/git/ref/tags/v2.0.0", reference)
        api("GET", f"{base}/releases/tags/v2.0.0", None, 1)
        api("GET", f"{base}/releases?per_page=100&page=1", [])
        api("POST", f"{base}/releases/generate-notes", {"body": "Native replacement"}, payload={"tag_name": "v2.0.0", "target_commitish": COMMIT})
        api("POST", f"{base}/releases", draft)
        api("GET", f"{base}/releases/tags/v2.0.0", None, 1)
        api("GET", f"{base}/releases?per_page=100&page=1", [draft])
        api("GET", f"{base}/releases/17", draft)
        for path in (self.publisher.archive, self.publisher.checksums):
            responses.append({"command": ["release", "upload", "v2.0.0", str(path), "--repo", "richhaase/bigboard"], "stdout": "", "status": 0})
        api("GET", f"{base}/releases/tags/v2.0.0", None, 1)
        api("GET", f"{base}/releases?per_page=100&page=1", [complete])
        api("GET", f"{base}/releases/17", complete)
        for asset_id, content in ((1, self.archive), (2, checksum)):
            responses.append({"command": ["api", f"{base}/releases/assets/{asset_id}", "--header", "Accept: application/octet-stream"],
                              "binary": base64.b64encode(content).decode(), "status": 0})
        api("GET", f"{base}/git/ref/tags/v2.0.0", reference)
        api("GET", f"{base}/releases/latest", {"tag_name": "v1.3.0"})
        api("PATCH", f"{base}/releases/17", published, payload={"draft": False, "make_latest": "true"})
        api("GET", f"{base}/releases/tags/v2.0.0", published)
        api("GET", f"{base}/releases/latest", published)
        queue = self.root / "responses.json"
        queue.write_text(json.dumps(responses))
        gh = binaries / "gh"
        gh.write_text(
            f"#!{sys.executable}\n"
            "import base64, json, os, sys\n"
            "from pathlib import Path\n"
            "path = Path(os.environ['TEST_GH_RESPONSES'])\n"
            "responses = json.loads(path.read_text())\n"
            "response = responses.pop(0)\n"
            "assert sys.argv[1:] == response['command'], sys.argv\n"
            "if '--input' in sys.argv:\n"
            " payload = json.load(sys.stdin)\n"
            " if response.get('payload') is not None: assert payload == response['payload'], payload\n"
            "path.write_text(json.dumps(responses))\n"
            "if response['status']:\n"
            " print('gh: Not Found (HTTP 404)', file=sys.stderr)\n"
            " sys.exit(response['status'])\n"
            "if 'binary' in response: sys.stdout.buffer.write(base64.b64decode(response['binary']))\n"
            "else: print(response['stdout'])\n"
        )
        gh.chmod(0o755)
        output = self.root / "outputs.txt"
        env = {**os.environ, "PATH": str(binaries), "TEST_GH_RESPONSES": str(queue)}
        result = subprocess.run([sys.executable, str(SCRIPT), "publish", "--tag", "v2.0.0", "--commit", COMMIT,
                                 "--output-dir", str(self.root), "--github-output", str(output)], env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(queue.read_text()), [])
        self.assertEqual(output.read_text(), "update_tap=true\n")
        self.assertEqual(self.publisher.checksums.read_bytes(), checksum)


if __name__ == "__main__":
    unittest.main()
