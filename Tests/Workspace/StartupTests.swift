import Foundation
import Testing
@testable import Bigboard

@Suite @MainActor
struct StartupTests {
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
            let store = StartupCredentials(try fixture.credentials(account: account))
            let workspace = fixture.workspace(store: store)
            await workspace.start()

            #expect(!workspace.checkingSignIn && !workspace.busy)
            #expect(workspace.authStatus.signedIn && workspace.authStatus.accountID == account)
            #expect(workspace.configuration == fixture.configuration(account: account))
            #expect(workspace.snapshot.accountID == account)
            #expect(workspace.snapshot.viewer == "user" + account)
            #expect(workspace.snapshot.bookmark == fixture.bookmark(account: account))
            #expect(workspace.snapshot.repositories.map(\.id) == [fixture.repository(account: account)])
            #expect(workspace.snapshot.commits.count == 2)
            #expect(Set(workspace.snapshot.commits.map(\.repo)) == [fixture.repository(account: account)])
            #expect(workspace.cachedRepositoryRows.map(\.id) == [fixture.repository(account: account)])
            #expect(workspace.snapshot.cachedOnly && workspace.snapshot.coverage.phase == "ready")
            #expect(workspace.error.isEmpty)
            #expect(store.mutations == 0)
            await workspace.shutdown()
            #expect(try fixture.storage.bookmarks(account)["all"] == fixture.bookmark(account: account))
        }
        #expect(await fixture.network.requests == 0)
    }

    @Test(arguments: ["missing", "malformed", "unreadable"])
    func unavailableCredentialsLeaveSavedWorkspaceUnopened(condition: String) async throws {
        let fixture = StartupFixture()
        defer { fixture.remove() }
        try fixture.saveWorkspace()
        let before = try fixture.savedFiles()
        let data: Data?
        switch condition {
        case "missing": data = nil
        case "malformed": data = Data("invalid credential data".utf8)
        default: data = try fixture.credentials()
        }
        let store = StartupCredentials(data, unreadable: condition == "unreadable")
        let workspace = fixture.workspace(store: store)
        await workspace.start()

        #expect(!workspace.checkingSignIn && !workspace.busy && !workspace.authStatus.signedIn)
        #expect(workspace.snapshot.coverage.phase == "signin")
        #expect(workspace.snapshot.accountID.isEmpty && workspace.snapshot.repositories.isEmpty)
        #expect(workspace.snapshot.commits.isEmpty && workspace.cachedRepositoryRows.isEmpty)
        #expect(workspace.configuration == WorkspaceConfiguration())
        #expect(workspace.error == (condition == "missing" ? "" : AuthFailure.storage.localizedDescription))
        #expect(await fixture.network.requests == 0)
        #expect(store.mutations == 0)
        #expect(try fixture.savedFiles() == before)
    }

    @Test(arguments: ["workspace.json", "access.json", "bookmarks.json"])
    func damagedSavedFileStopsStartupWithoutReplacingData(filename: String) async throws {
        let fixture = StartupFixture()
        defer { fixture.remove() }
        try fixture.saveWorkspace()
        let file = try fixture.storage.accountRoot("123").appendingPathComponent(filename)
        try Data("{broken".utf8).write(to: file)
        let before = try fixture.savedFiles()
        let store = StartupCredentials(try fixture.credentials())
        let workspace = fixture.workspace(store: store)
        await workspace.start()

        #expect(workspace.authStatus.signedIn)
        #expect(!workspace.checkingSignIn && !workspace.busy && !workspace.error.isEmpty)
        #expect(workspace.snapshot.repositories.isEmpty && workspace.snapshot.commits.isEmpty)
        #expect(workspace.cachedRepositoryRows.isEmpty)
        #expect(await fixture.network.requests == 0)
        #expect(store.mutations == 0)
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
        let workspace = fixture.workspace(store: StartupCredentials(try fixture.credentials()))
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
        let workspace = fixture.workspace(store: StartupCredentials(try fixture.credentials()))
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
    func bookmark(account: String = "123") -> Date {
        Date(timeIntervalSince1970: account == "123" ? 1_780_000_000 : 1_790_000_000)
    }
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
        try storage.saveBookmarks(["all": bookmark(account: account)], account: account)
    }
    func savedFiles() throws -> [Data] {
        try ["workspace.json", "access.json", "bookmarks.json"].map {
            try Data(contentsOf: storage.accountRoot("123").appendingPathComponent($0))
        }
    }
    func credentials(account: String = "123") throws -> Data {
        let value = AuthCredentials(
            accessToken: "fixture-access", refreshToken: "fixture-refresh",
            expiresAt: Self.now.addingTimeInterval(3600), refreshExpiresAt: Self.now.addingTimeInterval(86400),
            login: "user" + account, accountID: account, clientID: "fixture-client")
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return try encoder.encode(value)
    }
    func workspace(store: StartupCredentials) -> WorkspaceModel {
        let now = Self.now
        let auth = AuthManager(
            configuration: AuthConfiguration(clientID: "fixture-client", clientSecret: "fixture-secret", appSlug: "fixture-app"),
            store: store, transport: network, now: { now })
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

private final class StartupCredentials: CredentialStore, @unchecked Sendable {
    private let lock = NSLock()
    private let data: Data?
    private let unreadable: Bool
    private var mutationCount = 0
    var mutations: Int { lock.withLock { mutationCount } }
    init(_ data: Data?, unreadable: Bool = false) {
        self.data = data
        self.unreadable = unreadable
    }
    func load() throws -> Data? {
        if unreadable { throw AuthFailure.storage }
        return data
    }
    func save(_ data: Data) throws { lock.withLock { mutationCount += 1 } }
    func delete() throws { lock.withLock { mutationCount += 1 } }
}

private actor StartupTransport: GitHubTransport {
    private(set) var requests = 0
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        requests += 1
        throw GitHubFailure.unavailable
    }
}
