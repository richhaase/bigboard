import Foundation
import Darwin
import Testing
@testable import Bigboard

struct GitExecutableTests {
    @Test func discoversGitFromPathAndStandardLocations() throws {
        let homebrew = "/opt/homebrew/bin/git"
        #expect(try GitExecutable.select(environment: ["PATH": "/custom/bin:/usr/bin"], executable: { $0 == "/custom/bin/git" }).path == "/custom/bin/git")
        #expect(try GitExecutable.select(environment: [:], executable: { $0 == homebrew }).path == homebrew)
        #expect(try GitExecutable.select(environment: [:], executable: { $0 == "/usr/bin/git" }).path == "/usr/bin/git")
    }
    @Test func missingGitHasActionableError() {
        do {
            _ = try GitExecutable.select(environment: [:], executable: { _ in false })
            Issue.record("Selected missing Git")
        } catch { #expect(error.localizedDescription.contains("Install Git")) }
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
    @Test func installedGitSupportsPinnedCommands() async throws {
        let version = try await GitProcess.text(["--version"])
        #expect(version.hasPrefix("git version "))
        _ = try await GitProcess.run(["check-ref-format", "refs/heads/main"])
    }
}
