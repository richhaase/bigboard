import Foundation
import Testing
@testable import Bigboard

@Suite @MainActor
struct StartupTests {
    @Test(arguments: [false, true])
    func obsoleteBookmarksDoNotAffectStartupOrShutdown(existingFile: Bool) async throws {
        let fixture = StartupFixture()
        defer { fixture.remove() }
        try fixture.saveWorkspace()
        let file = try fixture.storage.accountRoot("123").appendingPathComponent("bookmarks.json")
        let obsolete = Data("{broken".utf8)
        if existingFile { try obsolete.write(to: file) }
        let saved = try fixture.savedFiles()
        let workspace = fixture.workspace()
        await workspace.start()
        #expect(workspace.error.isEmpty && workspace.authStatus.signedIn)
        #expect(workspace.configuration == fixture.configuration())
        await workspace.shutdown()
        #expect(try fixture.savedFiles() == saved)
        if existingFile { #expect(try Data(contentsOf: file) == obsolete) }
        else { #expect(!FileManager.default.fileExists(atPath: file.path)) }
        #expect(await fixture.network.requests == 0)
    }
    @Test func restoresOnlyTheSignedInAccountsSavedState() async throws {
        let fixture = StartupFixture()
        defer { fixture.remove() }
        let history = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: history.base) }
        for account in ["123", "456"] {
            try fixture.saveWorkspace(account: account)
            try fixture.installCache(history, account: account)
        }

        for account in ["123", "456"] {
            let workspace = fixture.workspace(account: account)
            await workspace.start()

            #expect(!workspace.checkingSignIn && !workspace.busy)
            #expect(workspace.authStatus.signedIn && workspace.authStatus.accountID == account)
            #expect(workspace.configuration == fixture.configuration(account: account))
            #expect(workspace.snapshot.accountID == account)
            #expect(workspace.snapshot.viewer == "user" + account)
            #expect(workspace.snapshot.repositories.map(\.id) == [fixture.repository(account: account)])
            #expect(workspace.snapshot.commits.count == 2)
            #expect(Set(workspace.snapshot.commits.map(\.repo)) == [fixture.repository(account: account)])
            #expect(workspace.cachedRepositoryRows.map(\.id) == [fixture.repository(account: account)])
            #expect(workspace.snapshot.cachedOnly && workspace.snapshot.coverage.phase == "ready")
            #expect(workspace.error.isEmpty)
            await workspace.shutdown()
        }
        #expect(await fixture.network.requests == 0)
    }

    @Test(arguments: [GitHubCLIFailure.signedOut, .missing, .rejected])
    func unavailableCredentialsLeaveSavedWorkspaceUnopened(condition: GitHubCLIFailure) async throws {
        let fixture = StartupFixture()
        defer { fixture.remove() }
        try fixture.saveWorkspace()
        let before = try fixture.savedFiles()
        let token = CLITokenSource()
        if condition == .signedOut { token.set(nil) }
        if condition == .missing { token.set(nil, failure: GitHubCLIFailure.missing) }
        let workspace = fixture.workspace(token: token, status: condition == .rejected ? 401 : 200)
        await workspace.start()

        #expect(!workspace.checkingSignIn && !workspace.busy && !workspace.authStatus.signedIn)
        #expect(workspace.snapshot.coverage.phase == "signin")
        #expect(workspace.snapshot.accountID.isEmpty && workspace.snapshot.repositories.isEmpty)
        #expect(workspace.snapshot.commits.isEmpty && workspace.cachedRepositoryRows.isEmpty)
        #expect(workspace.configuration == WorkspaceConfiguration())
        #expect(workspace.error == condition.localizedDescription)
        #expect(workspace.authStatus.configured == (condition != .missing))
        #expect(await fixture.network.requests == 0)
        #expect(try fixture.savedFiles() == before)
    }

    @Test(arguments: ["workspace.json", "access.json"])
    func damagedSavedFileStopsStartupWithoutReplacingData(filename: String) async throws {
        let fixture = StartupFixture()
        defer { fixture.remove() }
        try fixture.saveWorkspace()
        let file = try fixture.storage.accountRoot("123").appendingPathComponent(filename)
        try Data("{broken".utf8).write(to: file)
        let before = try fixture.savedFiles()
        let workspace = fixture.workspace()
        await workspace.start()

        #expect(workspace.authStatus.signedIn)
        #expect(!workspace.checkingSignIn && !workspace.busy && !workspace.error.isEmpty)
        #expect(workspace.snapshot.repositories.isEmpty && workspace.snapshot.commits.isEmpty)
        #expect(workspace.cachedRepositoryRows.isEmpty)
        #expect(await fixture.network.requests == 0)
        #expect(try fixture.savedFiles() == before)
    }

    @Test func anotherAccountsAccessFileCannotAuthorizeStartup() async throws {
        let fixture = StartupFixture()
        defer { fixture.remove() }
        try fixture.saveWorkspace()
        try fixture.saveWorkspace(account: "456")
        let file = try fixture.storage.accountRoot("123").appendingPathComponent("access.json")
        let other = try fixture.storage.accountRoot("456").appendingPathComponent("access.json")
        try Data(contentsOf: other).write(to: file)
        let before = try fixture.savedFiles()
        let workspace = fixture.workspace()
        await workspace.start()

        #expect(workspace.authStatus.signedIn && workspace.authStatus.accountID == "123")
        #expect(!workspace.checkingSignIn && !workspace.busy)
        #expect(workspace.error == "Stored access does not match this GitHub account.")
        #expect(workspace.snapshot.repositories.isEmpty && workspace.snapshot.commits.isEmpty)
        #expect(await fixture.network.requests == 0)
        #expect(try fixture.savedFiles() == before)
        #expect(try Data(contentsOf: file) == Data(contentsOf: other))
    }

    @Test(arguments: [false, true])
    func cachedRepositoryDoesNotReplaceMissingOrRevokedAccess(revoked: Bool) async throws {
        let fixture = StartupFixture()
        defer { fixture.remove() }
        let history = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: history.base) }
        try fixture.saveWorkspace()
        try fixture.installCache(history)
        let file = try fixture.storage.accountRoot("123").appendingPathComponent("access.json")
        if revoked { try fixture.storage.invalidateGrant("123") }
        else { try FileManager.default.removeItem(at: file) }
        let workspace = fixture.workspace()
        await workspace.start()

        #expect(workspace.authStatus.signedIn && !workspace.checkingSignIn && !workspace.busy)
        #expect(workspace.configuration == fixture.configuration())
        #expect(workspace.cachedRepositoryRows.map(\.id) == [fixture.repository()])
        #expect(workspace.snapshot.repositories.isEmpty && workspace.snapshot.commits.isEmpty)
        #expect(workspace.removableCacheIDs.isEmpty)
        #expect(workspace.error.isEmpty)
        #expect(try fixture.storage.loadGrant("123").isEmpty)
        #expect(await fixture.network.requests == 0)
        if revoked { #expect(try Data(contentsOf: file).isEmpty) }
        else { #expect(!FileManager.default.fileExists(atPath: file.path)) }
    }
}

@MainActor private struct StartupFixture {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    let network = StartupTransport()
    static let now = Date(timeIntervalSince1970: 1_800_000_000)
    var storage: WorkspaceStorage {
        WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    }

    func repository(account: String = "123") -> String { "user\(account)/repo" }
    func configuration(account: String = "123") -> WorkspaceConfiguration {
        var config = WorkspaceConfiguration()
        config.repositories = [repository(account: account)]
        config.groups = ["Focus": config.repositories]
        config.workAreas = [repository(account: account): [WorkArea(name: "Sources", paths: ["src/"])]]
        config.cachedOnly = true
        config.sinceDays = account == "123" ? 30 : 90
        return config
    }
    func saveWorkspace(account: String = "123") throws {
        let storage = storage
        try storage.saveConfiguration(configuration(account: account), account: account)
        try storage.saveGrant([Repository(id: repository(account: account))], account: account)
    }
    func savedFiles() throws -> [Data] {
        try ["workspace.json", "access.json"].map {
            try Data(contentsOf: storage.accountRoot("123").appendingPathComponent($0))
        }
    }
    func workspace(account: String = "123", token: CLITokenSource = CLITokenSource(), status: Int = 200) -> WorkspaceModel {
        let now = Self.now
        let user = CLIUserTransport(login: "user" + account, account: account, status: status)
        let auth = cliAuth(storage: storage, token: token, user: user, now: { now })
        return WorkspaceModel(storage: storage, authentication: auth, transport: network)
    }
    func installCache(_ history: HistoryFixture, account: String = "123") throws {
        let id = repository(account: account)
        let destination = try storage.repositoryRoot(account).appendingPathComponent("github.com/" + id + ".git")
        try FileManager.default.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true)
        try FileManager.default.copyItem(at: history.bare, to: destination)
        try Data((id + "\n").utf8).write(to: destination.appendingPathComponent("bigboard-cache"))
        _ = try history.git(["--git-dir=" + destination.path, "config", "remote.origin.url", "https://github.com/\(id).git"])
    }
    func remove() { try? FileManager.default.removeItem(at: root) }
}

private actor StartupTransport: GitHubTransport {
    private(set) var requests = 0
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        requests += 1
        throw GitHubFailure.unavailable
    }
}
