import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("release-target.py")


class ReleaseTargetTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.env = {**os.environ, "GIT_AUTHOR_NAME": "Fixture", "GIT_AUTHOR_EMAIL": "fixture@example.com", "GIT_COMMITTER_NAME": "Fixture", "GIT_COMMITTER_EMAIL": "fixture@example.com"}
        self.git("init", "-b", "main")
        self.git("-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "main")
        self.commit = self.git("rev-parse", "HEAD").strip()
        self.git("update-ref", "refs/remotes/origin/main", self.commit)

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.root, env=self.env, stderr=subprocess.PIPE, text=True)

    def resolve(self, tag):
        output = self.root / "output"
        output.unlink(missing_ok=True)
        result = subprocess.run(["python3", str(SCRIPT), "--github-output", str(output), "--", tag], cwd=self.root, env=self.env, capture_output=True, text=True)
        return result, output.read_text() if output.exists() else ""

    def test_lightweight_and_annotated_tags_resolve_same_exact_source(self):
        self.git("tag", "v2.0.0")
        self.git("-c", "tag.gpgsign=false", "tag", "-a", "v2.0.1-beta.1", "-m", "fixture")
        for tag in ("v2.0.0", "v2.0.1-beta.1"):
            result, output = self.resolve(tag)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(output, f"commit={self.commit}\n")

    def test_missing_and_nonrelease_refs_are_rejected(self):
        for tag in ("main", "v2.0.0", "v2.0.0\ncommit=injected", "--help", "v02.0.0"):
            result, output = self.resolve(tag)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(output, "")

    def test_tag_outside_main_is_rejected(self):
        self.git("checkout", "-b", "side")
        self.git("-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "side")
        self.git("tag", "v2.0.0")
        result, output = self.resolve("v2.0.0")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(output, "")
