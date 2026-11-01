import hashlib
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPTS = Path(__file__).parent


class UpdateHomebrewTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.archive = self.root / "Bigboard_2.0.0_darwin_universal.zip"
        self.archive.write_bytes(b"verified archive fixture")
        self.cask = self.root / "Casks/bigboard.rb"
        self.cask.parent.mkdir()

    def update(self):
        return subprocess.run(["python3", str(SCRIPTS / "update-homebrew-cask.py"), "v2.0.0", str(self.archive), str(self.cask), str(SCRIPTS / "homebrew-cask.sh")], capture_output=True, text=True)

    def test_new_and_older_casks_receive_verified_archive_checksum(self):
        for previous in (None, 'version "1.3.0"\n', 'version "2.0.0"\n'):
            self.cask.unlink(missing_ok=True)
            if previous:
                self.cask.write_text(previous)
            result = self.update()
            self.assertEqual(result.returncode, 0, result.stderr)
            content = self.cask.read_text()
            self.assertIn('version "2.0.0"', content)
            self.assertIn(hashlib.sha256(self.archive.read_bytes()).hexdigest(), content)
            self.assertIn('app "Bigboard.app"', content)

    def test_newer_cask_is_not_downgraded(self):
        original = 'version "2.1.0"\nsha256 "keep-existing"\n'
        self.cask.write_text(original)
        result = self.update()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.cask.read_text(), original)
        self.assertIn("will not downgrade", result.stdout)

    def test_unrecognized_existing_version_fails_without_change(self):
        original = 'version :latest\n'
        self.cask.write_text(original)
        self.assertNotEqual(self.update().returncode, 0)
        self.assertEqual(self.cask.read_text(), original)
