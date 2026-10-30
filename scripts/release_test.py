import base64
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPTS = Path(__file__).resolve().parent
SECRET_NAMES = (
    "QUILL_SIGN_P12", "QUILL_SIGN_PASSWORD", "QUILL_NOTARY_KEY",
    "QUILL_NOTARY_KEY_ID", "QUILL_NOTARY_ISSUER",
)
MOCK_TOOL = '''#!/usr/bin/env python3
import json
import os
from pathlib import Path
import sys

name = Path(sys.argv[0]).name
args = sys.argv[1:]
with Path(os.environ["TEST_LOG"]).open("a") as log:
    log.write(json.dumps([name, *args]) + "\\n")
if name == "uname":
    print("Darwin")
elif name == "security" and args[0] == "list-keychains":
    state = Path(os.environ["TEST_KEYCHAINS"])
    if "-s" in args:
        state.write_text(json.dumps(args[args.index("-s") + 1:]))
    else:
        for keychain in json.loads(state.read_text()):
            print("    " + json.dumps(keychain))
elif name == "security" and args[0] == "find-identity":
    print('1) ' + 'A' * 40 + ' "Developer ID Application: Test Fixture"')
elif name == "openssl":
    print("fixture-password")
elif name == "codesign" and "--sign" in args:
    keychains = json.loads(Path(os.environ["TEST_KEYCHAINS"]).read_text())
    keychain = args[args.index("--keychain") + 1]
    if keychain not in keychains or os.environ.get("TEST_CODESIGN_FAILURE"):
        print("error: The specified item could not be found in the keychain.", file=sys.stderr)
        raise SystemExit(1)
elif name == "xcrun" and args[:2] == ["notarytool", "submit"]:
    print(json.dumps({"status": os.environ.get("TEST_NOTARY_STATUS", "Accepted")}))
elif name == "xcrun" and args[:2] == ["stapler", "staple"]:
    (Path(args[2]) / "stapled").touch()
elif name == "ditto":
    Path(args[-1]).write_text("stapled" if (Path(args[-2]) / "stapled").exists() else "unstapled")
'''


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="bigboard-release-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.app = self.root / "Bigboard.app"
        self.app.mkdir()
        self.archive = self.root / "Bigboard_1.2.3_darwin_universal.zip"
        self.env = os.environ.copy()
        for key in SECRET_NAMES:
            self.env.pop(key, None)
        self.env["TMPDIR"] = str(self.root)
        self.env["TEST_LOG"] = str(self.root / "commands.jsonl")
        self.original_keychains = [
            str(self.root / "Keychain fixtures/login.keychain-db"),
            str(self.root / "Keychain fixtures/Shared Signing.keychain-db"),
        ]
        self.keychain_state = self.root / "keychains.json"
        self.keychain_state.write_text(json.dumps(self.original_keychains))
        self.env["TEST_KEYCHAINS"] = str(self.keychain_state)
        self.env["QUILL_SIGN_P12"] = base64.b64encode(b"fixture certificate").decode()
        self.env["QUILL_SIGN_PASSWORD"] = "fixture certificate password"
        encoded_key = base64.b64encode(b"fixture notary key" * 32).decode()
        self.env["QUILL_NOTARY_KEY"] = "\r\n".join(encoded_key[index:index + 64] for index in range(0, len(encoded_key), 64))
        self.env["QUILL_NOTARY_KEY_ID"] = "fixture-key"
        self.env["QUILL_NOTARY_ISSUER"] = "fixture-issuer"
        binaries = self.root / "bin"
        binaries.mkdir()
        for name in ("uname", "security", "openssl", "codesign", "ditto", "xcrun", "spctl"):
            tool = binaries / name
            tool.write_text(MOCK_TOOL)
            tool.chmod(0o755)
        self.env["PATH"] = str(binaries) + os.pathsep + self.env.get("PATH", "/usr/bin:/bin")

    def run_script(self, name, *args):
        return subprocess.run(["bash", str(SCRIPTS / name), *map(str, args)], env=self.env, capture_output=True, text=True)

    def commands(self):
        return [json.loads(line) for line in Path(self.env["TEST_LOG"]).read_text().splitlines()]

    def assert_search_list_restored(self, commands):
        created = next(command for command in commands if command[:2] == ["security", "create-keychain"])
        keychain = created[-1]
        capture = ["security", "list-keychains", "-d", "user"]
        registered = [*capture, "-s", keychain, *self.original_keychains]
        restored = [*capture, "-s", *self.original_keychains]
        deleted = ["security", "delete-keychain", keychain]
        signing = next(command for command in commands if command[0] == "codesign" and "--sign" in command)
        self.assertLess(commands.index(capture), commands.index(created))
        self.assertLess(commands.index(registered), commands.index(signing))
        self.assertLess(commands.index(signing), commands.index(restored))
        self.assertLess(commands.index(restored), commands.index(deleted))
        self.assertEqual(json.loads(self.keychain_state.read_text()), self.original_keychains)
        self.assertFalse(list(self.root.glob("bigboard-sign.*")))

    def test_archive_contains_stapled_app_and_temporary_keychain_is_removed(self):
        result = self.run_script("sign-macos.sh", self.app, self.archive)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.archive.read_text(), "stapled")
        commands = self.commands()
        self.assertTrue(any(command[:3] == ["xcrun", "stapler", "validate"] for command in commands))
        self.assertTrue(any(command[:2] == ["security", "delete-keychain"] for command in commands))
        self.assertFalse(list(self.root.glob("bigboard-sign.*")))
        signing = next(command for command in commands if command[0] == "codesign" and "--sign" in command)
        self.assertIn("runtime", signing)
        self.assertIn("--timestamp", signing)
        self.assert_search_list_restored(commands)

    def test_signing_failure_restores_search_list_without_exposing_secrets(self):
        self.env["TEST_CODESIGN_FAILURE"] = "1"
        result = self.run_script("sign-macos.sh", self.app, self.archive)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Signing failed during: sign application", result.stderr)
        output = result.stdout + result.stderr
        for name in SECRET_NAMES:
            self.assertNotIn(self.env[name], output)
        self.assertNotIn("fixture-password", output)
        self.assertFalse(self.archive.exists())
        commands = self.commands()
        self.assertFalse(any(command[0] == "ditto" for command in commands))
        self.assertFalse(any(command[0] == "xcrun" for command in commands))
        self.assert_search_list_restored(commands)

    def test_long_unwrapped_base64_key_is_not_treated_as_a_path(self):
        self.env["QUILL_NOTARY_KEY"] = "".join(self.env["QUILL_NOTARY_KEY"].split())
        self.assertGreater(len(self.env["QUILL_NOTARY_KEY"]), 255)
        result = self.run_script("sign-macos.sh", self.app, self.archive)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.archive.read_text(), "stapled")

    def test_rejected_notarization_does_not_produce_release_archive(self):
        self.env["TEST_NOTARY_STATUS"] = "Invalid"
        result = self.run_script("sign-macos.sh", self.app, self.archive)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("did not accept", result.stderr)
        self.assertFalse(self.archive.exists())
        self.assertFalse(any(command[:3] == ["xcrun", "stapler", "staple"] for command in self.commands()))
        self.assertTrue(any(command[:2] == ["security", "delete-keychain"] for command in self.commands()))
        self.assertFalse(list(self.root.glob("bigboard-sign.*")))

    def test_missing_credentials_fail_before_keychain_access(self):
        self.env.pop("QUILL_SIGN_P12")
        result = self.run_script("sign-macos.sh", self.app, self.archive)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any(command[0] == "security" for command in self.commands()))

    def test_cask_installs_only_app_with_runtime_dependencies_and_exact_checksum(self):
        import hashlib
        self.archive.write_bytes(b"fixture app archive")
        result = self.run_script("homebrew-cask.sh", "1.2.3", self.archive)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(hashlib.sha256(self.archive.read_bytes()).hexdigest(), result.stdout)
        self.assertIn('app "Bigboard.app"', result.stdout)
        self.assertIn('depends_on formula: "git"', result.stdout)
        self.assertNotIn('depends_on formula: "gh"', result.stdout)
        self.assertNotIn("gh auth", result.stdout)
        self.assertNotIn('binary ', result.stdout)
        self.assertIn('darwin_universal.zip', result.stdout)
        self.assertIn('depends_on macos: ">= :ventura"', result.stdout)
        syntax = subprocess.run(["ruby", "-c"], input=result.stdout, capture_output=True, text=True)
        self.assertEqual(syntax.returncode, 0, syntax.stderr)

    def test_prerelease_cannot_replace_stable_homebrew_cask(self):
        self.archive.write_bytes(b"fixture app archive")
        result = self.run_script("homebrew-cask.sh", "1.2.3-beta.1", self.archive)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def test_invalid_build_version_fails_before_building(self):
        result = self.run_script("build-macos.sh", "not-a-version")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Invalid release version", result.stderr)

    def test_release_auth_configuration_is_required_and_validated(self):
        for name in ("BIGBOARD_GITHUB_CLIENT_ID", "BIGBOARD_GITHUB_CLIENT_SECRET", "BIGBOARD_GITHUB_APP_SLUG"):
            self.env.pop(name, None)
        result = self.run_script("auth-ldflags.sh", "required")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertEqual(self.run_script("auth-ldflags.sh").returncode, 0)
        self.env.update(BIGBOARD_GITHUB_CLIENT_ID="fixture-client", BIGBOARD_GITHUB_CLIENT_SECRET="fixture-public-parameter", BIGBOARD_GITHUB_APP_SLUG="fixture-app")
        result = self.run_script("auth-ldflags.sh", "required")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("-X main.githubClientID=fixture-client", result.stdout)
        self.assertIn("-X main.githubAppSlug=fixture-app", result.stdout)
        self.env["BIGBOARD_GITHUB_CLIENT_SECRET"] = "invalid whitespace parameter"
        result = self.run_script("auth-ldflags.sh", "required")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertNotIn(self.env["BIGBOARD_GITHUB_CLIENT_SECRET"], result.stderr)
