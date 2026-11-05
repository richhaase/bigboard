import Foundation
import Testing
@testable import Bigboard

struct GitHubCLIAuthTests {
    private func storage() -> (WorkspaceStorage, URL) {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        return (
            WorkspaceStorage(
                supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache")),
            root
        )
    }
    @Test func signedInStatusComesFromGitHubCLIAndViewer() async throws {
        let (storage, root) = storage()
        defer { try? FileManager.default.removeItem(at: root) }
        let token = CLITokenSource("gho_fixture")
        let auth = cliAuth(storage: storage, login: "octo", account: "42", token: token)
        let state = await auth.status()
        #expect(state.signedIn && state.configured && state.error.isEmpty)
        #expect(state.login == "octo" && state.accountID == "42")
        #expect(try await auth.token() == "gho_fixture")
        #expect(token.count == 1)
        #expect(try storage.loadIdentity() == GitHubIdentity(login: "octo", accountID: "42"))
    }
    @Test func tokenIsCachedBrieflyAndRereadAfterInvalidation() async throws {
        let clock = TestClock()
        let token = CLITokenSource("first")
        let auth = cliAuth(token: token, now: clock.now)
        #expect(try await auth.token() == "first")
        token.set("second")
        clock.advance(299)
        #expect(try await auth.token() == "first")
        clock.advance(2)
        #expect(try await auth.token() == "second")
        token.set("third")
        await auth.invalidate()
        #expect(try await auth.token() == "third")
        #expect(token.count == 3)
    }
    @Test func signedOutAndMissingCLIAreActionable() async {
        let signedOut = await cliAuth(token: CLITokenSource(nil)).status()
        #expect(!signedOut.signedIn && signedOut.configured)
        #expect(signedOut.error.contains("gh auth login"))
        let missingSource = CLITokenSource()
        missingSource.set(nil, failure: GitHubCLIFailure.missing)
        let missing = await cliAuth(token: missingSource).status()
        #expect(!missing.signedIn && !missing.configured)
        #expect(missing.error.contains("brew install gh"))
        do {
            _ = try await cliAuth(token: CLITokenSource("bad token\n")).token()
            Issue.record("Accepted a malformed token")
        } catch { #expect(error as? GitHubCLIFailure == .signedOut) }
    }
    @Test func rejectedTokenIsNotSignedIn() async {
        let user = CLIUserTransport(status: 401)
        let state = await cliAuth(user: user).status()
        #expect(!state.signedIn)
        #expect(state.error.contains("gh auth refresh"))
    }
    @Test func offlineStartupUsesLastVerifiedIdentity() async throws {
        let (storage, root) = storage()
        defer { try? FileManager.default.removeItem(at: root) }
        try storage.saveIdentity(GitHubIdentity(login: "octo", accountID: "42"))
        let offline = await cliAuth(storage: storage, user: CLIUserTransport(status: 0)).status()
        #expect(offline.signedIn && offline.accountID == "42" && offline.login == "octo")
        let (empty, emptyRoot) = self.storage()
        defer { try? FileManager.default.removeItem(at: emptyRoot) }
        let unknown = await cliAuth(storage: empty, user: CLIUserTransport(status: 0)).status()
        #expect(!unknown.signedIn && !unknown.error.isEmpty)
    }
    @Test func verifyRejectsChangedAccountAndRecordsRenamedLogin() async throws {
        let (storage, root) = storage()
        defer { try? FileManager.default.removeItem(at: root) }
        let user = CLIUserTransport(login: "octo", account: "42")
        let auth = cliAuth(storage: storage, user: user)
        _ = await auth.status()
        user.respond(login: "octo-renamed")
        let github = GitHubService(token: { try await auth.token() }, transport: user)
        let state = try await auth.verify(using: github, account: "42")
        #expect(state.login == "octo-renamed")
        #expect(try storage.loadIdentity()?.login == "octo-renamed")
        user.respond(account: "43")
        await #expect(throws: GitHubFailure.auth) { try await auth.verify(using: github, account: "42") }
    }
    @Test func cliEnvironmentKeepsConfigLocationAndDropsTokens() {
        let environment = GitHubCLIRunner.environment([
            "HOME": "/fixture/home", "GH_CONFIG_DIR": "/fixture/gh", "GH_TOKEN": "leak", "GITHUB_TOKEN": "leak",
            "GH_HOST": "evil.example", "PATH": "/fixture/bin",
        ])
        #expect(environment["HOME"] == "/fixture/home")
        #expect(environment["GH_CONFIG_DIR"] == "/fixture/gh")
        #expect(environment["GH_TOKEN"] == nil && environment["GITHUB_TOKEN"] == nil && environment["GH_HOST"] == nil)
        #expect(environment["PATH"] == "/usr/bin:/bin:/usr/sbin:/sbin")
        #expect(environment["GH_PROMPT_DISABLED"] == "1")
    }
    @Test func missingGitHubCLIIsReportedDistinctly() throws {
        do {
            _ = try GitExecutable.select("gh", environment: [:], executable: { _ in false })
            Issue.record("Selected missing gh")
        } catch { #expect(error as? GitHubCLIFailure == .missing) }
        #expect(try GitExecutable.select("gh", environment: [:], executable: { $0 == "/opt/homebrew/bin/gh" }).path == "/opt/homebrew/bin/gh")
    }
}
final class TestClock: @unchecked Sendable {
    private let lock = NSLock()
    private var date = Date(timeIntervalSince1970: 1_800_000_000)
    func now() -> Date { lock.withLock { date } }
    func advance(_ seconds: TimeInterval) { lock.withLock { date.addTimeInterval(seconds) } }
}
