import Foundation
import Darwin
import Testing
@testable import Bigboard

struct GitExecutableTests {
    private func trustedMetadata(_ url: URL) -> GitExecutableMetadata {
        GitExecutableMetadata(owner: 0, mode: (url == GitExecutable.path ? mode_t(S_IFREG) : mode_t(S_IFDIR)) | 0o755, writable: false, executable: true)
    }
    @Test func selectsOnlyVerifiedCommandLineToolsGit() throws {
        var inspected: [URL] = [], verified: [URL] = []
        let selected = try GitExecutable.select(inspect: { url in inspected.append(url); return trustedMetadata(url) }, verify: { verified.append($0) })
        #expect(selected.path == "/Library/Developer/CommandLineTools/usr/bin/git")
        #expect(inspected.first == selected && inspected.last?.path == "/")
        #expect(verified == [selected])
        #expect(!inspected.contains { $0.path.contains("homebrew") || $0.path == "/usr/bin/git" })
    }
    @Test func missingOrInvalidSignatureFailsClosed() {
        #expect(throws: (any Error).self) { try GitExecutable.select(inspect: { _ in throw POSIXError(.ENOENT) }, verify: { _ in Issue.record("Verified missing Git") }) }
        do {
            _ = try GitExecutable.select(inspect: trustedMetadata, verify: { _ in throw BigboardError.message("Invalid signature") })
            Issue.record("Selected Git with an invalid signature")
        } catch { #expect(error.localizedDescription.contains("xcode-select --install")) }
    }
    @Test func rejectsWritableOwnedAndRedirectedExecutablesOrParents() {
        let parent = GitExecutable.path.deletingLastPathComponent()
        for target in [GitExecutable.path, parent] {
            for defect in 0..<5 {
                #expect(throws: (any Error).self) {
                    try GitExecutable.select(inspect: { url in
                        var value = trustedMetadata(url)
                        if url == target {
                            switch defect {
                            case 0: value.owner = 501
                            case 1: value.mode |= 0o020
                            case 2: value.mode |= 0o002
                            case 3: value.writable = true
                            default: value.mode = mode_t(S_IFLNK) | 0o755
                            }
                        }
                        return value
                    }, verify: { _ in Issue.record("Verified a replaceable executable") })
                }
            }
        }
        #expect(throws: (any Error).self) { try GitExecutable.select(inspect: { url in var value = trustedMetadata(url); if url == GitExecutable.path { value.executable = false }; return value }, verify: { _ in Issue.record("Verified nonexecutable Git") }) }
    }
    @Test func environmentCannotRedirectGitOrDeveloperToolSelection() {
        let input = ["HOME": "/fixture/home", "TMPDIR": "/fixture/tmp", "XDG_CONFIG_HOME": "/fixture/config", "PATH": "/fixture/bin", "DEVELOPER_DIR": "/fixture/tools", "TOOLCHAINS": "fixture", "SDKROOT": "/fixture/sdk", "GIT_EXEC_PATH": "/fixture/helpers", "GIT_CONFIG_GLOBAL": "/fixture/gitconfig", "GIT_CONFIG_COUNT": "1", "GITHUB_TOKEN": "fixture-token", "BIGBOARD_GIT_AUTHORIZATION": "fixture-header", "DYLD_INSERT_LIBRARIES": "/fixture/library", "SSH_ASKPASS": "/fixture/askpass", "SSLKEYLOGFILE": "/fixture/keylog", "HTTPS_PROXY": "https://fixture.invalid", "CURL_CA_BUNDLE": "/fixture/ca"]
        let result = GitProcess.environment(input)
        #expect(result["HOME"] == input["HOME"] && result["TMPDIR"] == input["TMPDIR"] && result["XDG_CONFIG_HOME"] == input["XDG_CONFIG_HOME"])
        #expect(result["PATH"] == "/usr/bin:/bin:/usr/sbin:/sbin")
        #expect(result["GIT_CONFIG_GLOBAL"] == "/dev/null" && result["GIT_CONFIG_SYSTEM"] == "/dev/null")
        #expect(result["GIT_CONFIG_NOSYSTEM"] == "1" && result["GIT_TERMINAL_PROMPT"] == "0")
        #expect(result["SSH_ASKPASS"] == "" && result["SSH_ASKPASS_REQUIRE"] == "never")
        for key in ["DEVELOPER_DIR", "TOOLCHAINS", "SDKROOT", "GIT_EXEC_PATH", "GIT_CONFIG_COUNT", "GITHUB_TOKEN", "BIGBOARD_GIT_AUTHORIZATION", "DYLD_INSERT_LIBRARIES", "SSLKEYLOGFILE", "HTTPS_PROXY", "CURL_CA_BUNDLE"] { #expect(result[key] == nil) }
    }
    @Test func installedAppleGitSupportsPinnedCommands() async throws {
        #expect(try GitProcess.executable() == GitExecutable.path)
        let version = try await GitProcess.text(["--version"])
        #expect(version.hasPrefix("git version ") && version.contains("Apple Git"))
        _ = try await GitProcess.run(["check-ref-format", "refs/heads/main"])
    }
}
