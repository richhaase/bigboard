import json
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("verify-release.sh")
MOCK = '''#!/usr/bin/env python3
import json, os, shutil, sys
from pathlib import Path
name = Path(sys.argv[0]).name
args = sys.argv[1:]
with Path(os.environ["VERIFY_LOG"]).open("a") as log:
    log.write(json.dumps([name, *args]) + "\\n")
if name == "ditto":
    shutil.copytree(os.environ["VERIFY_APP"], Path(args[-1]) / "Bigboard.app")
if name == "lipo":
    print(os.environ.get("VERIFY_ARCHS", "arm64 x86_64"))
if name == os.environ.get("VERIFY_FAIL"):
    raise SystemExit(1)
'''


class VerifyReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.app = self.root / "Bigboard.app"
        (self.app / "Contents/MacOS").mkdir(parents=True)
        (self.app / "Contents/MacOS/Bigboard").touch()
        self.info = {"CFBundleIdentifier": "com.richhaase.bigboard", "CFBundleShortVersionString": "2.0.0", "CFBundleVersion": "2.0.0", "LSMinimumSystemVersion": "26.0"}
        self.package = self.root / "Package.swift"
        self.package.write_text('platforms: [.macOS("26.0")]')
        self.log = self.root / "commands"
        self.binaries = self.root / "bin"
        self.binaries.mkdir()
        for name in ("ditto", "lipo", "codesign", "xcrun", "spctl"):
            path = self.binaries / name
            path.write_text(MOCK)
            path.chmod(0o755)
        self.env = {**os.environ, "PATH": str(self.binaries) + os.pathsep + os.environ["PATH"], "VERIFY_LOG": str(self.log), "VERIFY_APP": str(self.app), "TMPDIR": str(self.root)}

    def verify(self):
        with (self.app / "Contents/Info.plist").open("wb") as target:
            plistlib.dump(self.info, target)
        return subprocess.run(["bash", str(SCRIPT), "v2.0.0", str(self.root / "archive.zip"), str(self.package)], env=self.env, capture_output=True, text=True)

    def test_verifies_both_architectures_signature_developer_id_ticket_and_gatekeeper(self):
        result = self.verify()
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = [json.loads(line) for line in self.log.read_text().splitlines()]
        self.assertTrue(any(call[:4] == ["codesign", "--verify", "--deep", "--strict"] for call in calls))
        self.assertTrue(any(call[0] == "codesign" and any("1.2.840.113635.100.6.1.13" in value for value in call) for call in calls))
        self.assertTrue(any(call[:3] == ["xcrun", "stapler", "validate"] for call in calls))
        self.assertTrue(any(call[:2] == ["spctl", "--assess"] for call in calls))
        self.assertFalse(list(self.root.glob("bigboard-verify.*")))

    def test_rejects_wrong_version_identity_or_source_deployment_target(self):
        for key in self.info:
            original = self.info[key]
            self.info[key] = "wrong"
            self.assertNotEqual(self.verify().returncode, 0)
            self.info[key] = original
        self.package.write_text('platforms: [.macOS("27.0")]')
        self.assertNotEqual(self.verify().returncode, 0)

    def test_rejects_single_architecture_and_signature_or_notarization_failures(self):
        self.env["VERIFY_ARCHS"] = "arm64"
        self.assertNotEqual(self.verify().returncode, 0)
        del self.env["VERIFY_ARCHS"]
        for tool in ("codesign", "xcrun", "spctl"):
            self.env["VERIFY_FAIL"] = tool
            self.assertNotEqual(self.verify().returncode, 0)
            self.assertFalse(list(self.root.glob("bigboard-verify.*")))
