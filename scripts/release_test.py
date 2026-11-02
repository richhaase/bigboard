import base64
import json
import os
import plistlib
import shutil
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
elif name == "codesign" and "--sign" in args and "--keychain" in args:
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
elif name == "swift" and "--show-bin-path" in args:
    print(os.environ["TEST_SWIFT_BIN"])
elif name == "sips":
    Path(args[args.index("--out") + 1]).write_bytes(b"fixture icon")
elif name == "iconutil":
    Path(args[args.index("-o") + 1]).write_bytes(b"fixture icns")
elif name == "lipo":
    print("arm64 x86_64")
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
        for name in ("uname", "security", "openssl", "codesign", "ditto", "xcrun", "spctl", "swift", "sips", "iconutil", "lipo", "gh"):
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
        self.assertNotIn('depends_on formula: "git"', result.stdout)
        self.assertIn("Apple Command Line Tools", result.stdout)
        self.assertIn("xcode-select --install", result.stdout)
        self.assertNotIn('depends_on formula: "gh"', result.stdout)
        self.assertNotIn("gh auth", result.stdout)
        self.assertNotIn('binary ', result.stdout)
        self.assertIn('darwin_universal.zip', result.stdout)
        self.assertIn('depends_on macos: :tahoe', result.stdout)
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

    def test_release_builds_checkout_and_publishes_zip_and_checksum(self):
        import hashlib
        repo = self.root / "release-repo"
        (repo / "scripts").mkdir(parents=True)
        shutil.copyfile(SCRIPTS / "release.sh", repo / "scripts/release.sh")
        build = repo / "scripts/build-macos.sh"
        build.write_text('set -eu\nmkdir -p "$(dirname "$0")/../native/build/Bigboard.app"\nprintf tagged > "$(dirname "$0")/../native/build/Bigboard.app/content"\n')
        (repo / "scripts/sign-macos.sh").write_text('set -eu\ncp "$1/content" "$2"\n')
        result = subprocess.run(["/bin/bash", str(repo / "scripts/release.sh"), "v2.0.1"], env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        archive = repo / "native/build/Bigboard_2.0.1_darwin_universal.zip"
        self.assertEqual(archive.read_bytes(), b"tagged")
        checksums = repo / "native/build/checksums.txt"
        self.assertEqual(checksums.read_text(), f"{hashlib.sha256(b'tagged').hexdigest()}  {archive.name}\n")
        publish = next(command for command in self.commands() if command[0] == "gh")
        self.assertEqual(publish[:4], ["gh", "release", "create", "v2.0.1"])
        self.assertIn(str(archive), publish)
        self.assertIn(str(checksums), publish)
        self.assertIn("--verify-tag", publish)
        self.assertNotIn("--target", publish)

    def test_native_publisher_json_is_validated_without_printing_values(self):
        path = self.root / "publisher.json"
        secret = "invalid private value"
        path.write_text(json.dumps({"BIGBOARD_GITHUB_CLIENT_ID": "fixture-client", "BIGBOARD_GITHUB_CLIENT_SECRET": secret, "BIGBOARD_GITHUB_APP_SLUG": "fixture-app"}))
        for key in ("BIGBOARD_GITHUB_CLIENT_ID", "BIGBOARD_GITHUB_CLIENT_SECRET", "BIGBOARD_GITHUB_APP_SLUG", "BIGBOARD_PUBLISHER_LOADED"):
            self.env.pop(key, None)
        self.env["BIGBOARD_PUBLISHER_CONFIG"] = str(path)
        result = self.run_script("build-swift.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Missing or malformed publisher setting: BIGBOARD_GITHUB_CLIENT_SECRET", result.stderr)
        self.assertNotIn(secret, result.stdout + result.stderr)

    def test_native_publisher_json_requires_string_fields(self):
        path = self.root / "publisher.json"
        path.write_text(json.dumps({"BIGBOARD_GITHUB_CLIENT_ID": ["wrong type"]}))
        self.env["BIGBOARD_PUBLISHER_CONFIG"] = str(path)
        self.env.pop("BIGBOARD_PUBLISHER_LOADED", None)
        result = self.run_script("build-swift.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Cannot read publisher settings JSON", result.stderr)

    def test_native_release_requires_publisher_configuration_before_build(self):
        for key in ("BIGBOARD_GITHUB_CLIENT_ID", "BIGBOARD_GITHUB_CLIENT_SECRET", "BIGBOARD_GITHUB_APP_SLUG", "BIGBOARD_PUBLISHER_LOADED"):
            self.env.pop(key, None)
        self.env["BIGBOARD_PUBLISHER_CONFIG"] = str(self.root / "absent.json")
        result = self.run_script("build-macos.sh", "1.2.3")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Missing publisher setting:", result.stderr)

    def test_native_default_and_release_builds_use_native_assets_and_artifact_paths(self):
        repo = self.root / "native-repo"
        (repo / "scripts").mkdir(parents=True)
        (repo / "native/Assets").mkdir(parents=True)
        for name in ("build-swift.sh", "build-macos.sh"):
            shutil.copyfile(SCRIPTS / name, repo / "scripts" / name)
        source_icon = SCRIPTS.parent / "native/Assets/AppIcon.png"
        self.assertTrue(source_icon.is_file())
        shutil.copyfile(source_icon, repo / "native/Assets/AppIcon.png")
        binary_root = self.root / "compiled"
        binary_root.mkdir()
        (binary_root / "Bigboard").write_bytes(b"fixture native executable")
        self.env.update(TEST_SWIFT_BIN=str(binary_root), BIGBOARD_PUBLISHER_CONFIG=str(self.root / "absent.json"), BIGBOARD_GITHUB_CLIENT_ID="fixture-client", BIGBOARD_GITHUB_CLIENT_SECRET="fixture-public-parameter", BIGBOARD_GITHUB_APP_SLUG="fixture-app")
        for key in ("BIGBOARD_PUBLISHER_LOADED", "UNIVERSAL", "CONFIGURATION", "REQUIRE_AUTH"):
            self.env.pop(key, None)
        result = subprocess.run(["/bin/bash", str(repo / "scripts/build-swift.sh"), "1.2.3"], env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        default_builds = [command for command in self.commands() if command[:2] == ["swift", "build"]]
        self.assertEqual(len(default_builds), 2)
        self.assertTrue(all("debug" in command and "--arch" not in command for command in default_builds))
        Path(self.env["TEST_LOG"]).unlink()
        result = subprocess.run(["/bin/bash", str(repo / "scripts/build-macos.sh"), "1.2.3"], env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        app = repo / "native/build/Bigboard.app"
        self.assertEqual((app / "Contents/MacOS/Bigboard").read_bytes(), b"fixture native executable")
        self.assertTrue((app / "Contents/Resources/AppIcon.icns").is_file())
        with (app / "Contents/Info.plist").open("rb") as source:
            info = plistlib.load(source)
        self.assertEqual(info["CFBundleIdentifier"], "com.richhaase.bigboard")
        self.assertEqual(info["LSMinimumSystemVersion"], "26.0")
        self.assertEqual(info["BIGBOARD_GITHUB_CLIENT_ID"], "fixture-client")
        self.assertEqual(info["BIGBOARD_GITHUB_CLIENT_SECRET"], "fixture-public-parameter")
        self.assertEqual(info["BIGBOARD_GITHUB_APP_SLUG"], "fixture-app")
        commands = self.commands()
        builds = [command for command in commands if command[:2] == ["swift", "build"]]
        self.assertTrue(builds)
        self.assertTrue(all("arm64" in command and "x86_64" in command and "release" in command for command in builds))
        self.assertTrue(any(command[:2] == ["lipo", "-archs"] and command[-1] == str(app / "Contents/MacOS/Bigboard") for command in commands))
        self.assertFalse((repo / "desktop").exists())
        definition = (SCRIPTS.parent / ".github/workflows/release.yml").read_text()
        self.assertIn('bash scripts/release.sh "$RELEASE_TAG"', definition)
        self.assertNotIn("desktop/build", definition)
        self.assertFalse((SCRIPTS.parent / ".goreleaser.yaml").exists())
