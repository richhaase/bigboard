import Foundation
import Testing
@testable import Bigboard

@Suite @MainActor struct CloneInventoryTests {
    @Test(arguments: [false, true])
    func orderlyQuitPreservesOfflineHistory(settingsDiscovery: Bool) async throws {
        let fixture = try CloneInventoryFixture(repositories: ["owner/repo"])
        defer { fixture.remove() }
        await fixture.workspace.start()
        let commits = fixture.workspace.snapshot.commits.map(\.id)
        #expect(!commits.isEmpty)
        var config = fixture.workspace.configuration; config.cachedOnly = false
        try fixture.storage.saveConfiguration(config, account: "123")
        fixture.workspace.configuration = config
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        #expect(try fixture.storage.loadClones("123").map(\.id) == ["owner/repo"])
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        await fixture.network.release(.failure(CancellationError()))
        await shutdown.value; await refresh.value
        #expect(try fixture.storage.loadClones("123").map(\.id) == ["owner/repo"])
        let restarted = WorkspaceModel(storage: fixture.storage, authentication: fixture.authentication(), transport: UnavailableTransport(), refreshHistory: { _, repo, _ in repo })
        await restarted.start()
        #expect(restarted.snapshot.repositories.map(\.id) == ["owner/repo"])
        #expect(restarted.snapshot.commits.map(\.id) == commits)
        #expect(restarted.snapshot.repositories.allSatisfy { $0.stale })
        config.cachedOnly = true
        try fixture.storage.saveConfiguration(config, account: "123")
        let cached = WorkspaceModel(storage: fixture.storage, authentication: fixture.authentication(), transport: UnavailableTransport(), refreshHistory: { _, repo, _ in repo })
        await cached.start()
        #expect(cached.snapshot.cachedOnly)
        #expect(cached.snapshot.commits.map(\.id) == commits)
    }

    @Test(arguments: [false, true], [401, 403, 404])
    func deniedViewerCheckKeepsCloneList(settingsDiscovery: Bool, status: Int) async throws {
        let fixture = try CloneInventoryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        await fixture.network.release(.success(CloneInventoryTransport.response(status: status)))
        await shutdown.value; await refresh.value
        #expect(try fixture.storage.loadClones("123").map(\.id) == ["owner/repo", "other/keep"])
        #expect(try fixture.storage.loadClones("456").map(\.id) == ["separate/private"])
    }

    @Test(arguments: [false, true])
    func shutdownBeforeLocalDiscoveryPreservesPreviousInventory(settingsDiscovery: Bool) async throws {
        let fixture = try CloneInventoryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        await fixture.network.release(.success(CloneInventoryTransport.response(body: #"{"login":"fixture","id":123}"#)))
        await shutdown.value; await refresh.value
        #expect(try fixture.storage.loadClones("123").map(\.id) == ["owner/repo", "other/keep"])
        #expect(try fixture.storage.loadClones("456").map(\.id) == ["separate/private"])
    }

    @Test(arguments: [false, true])
    func viewerAccountMismatchDuringShutdownKeepsEachAccountsClones(settingsDiscovery: Bool) async throws {
        let fixture = try CloneInventoryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        await fixture.network.release(.success(CloneInventoryTransport.response(body: #"{"login":"other","id":456}"#)))
        await shutdown.value; await refresh.value
        #expect(try fixture.storage.loadClones("123").map(\.id) == ["owner/repo", "other/keep"])
        #expect(try fixture.storage.loadClones("456").map(\.id) == ["separate/private"])
    }

    @Test(arguments: [false, true])
    func accountChangeSupersedesShutdownAccessResult(settingsDiscovery: Bool) async throws {
        let fixture = try CloneInventoryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        fixture.token.set("switched-access")
        fixture.user.respond(login: "other", account: "456")
        await fixture.workspace.checkConnection()
        #expect(fixture.workspace.authStatus.accountID == "456")
        #expect(try fixture.storage.loadClones("456").map(\.id) == ["other/new"])
        await fixture.network.release(.success(CloneInventoryTransport.response(body: #"{"login":"fixture","id":123}"#)))
        await shutdown.value; await refresh.value
        #expect(fixture.workspace.snapshot.accountID == "456")
        #expect(try fixture.storage.loadClones("123").map(\.id) == ["owner/repo", "other/keep"])
        #expect(try fixture.storage.loadClones("456").map(\.id) == ["other/new"])
    }

    @Test(arguments: [false, true])
    func settingsAndRefreshCancellationPreserveCloneListWithoutShutdown(settingsDiscovery: Bool) async throws {
        let fixture = try CloneInventoryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        if settingsDiscovery { refresh.cancel(); await fixture.network.waitUntilCancelled() }
        await fixture.network.release(.failure(CancellationError()))
        await refresh.value
        #expect(try fixture.storage.loadClones("123").map(\.id) == ["owner/repo", "other/keep"])
        #expect(!fixture.workspace.busy)
        #expect(fixture.workspace.authStatus.signedIn)
    }

    @Test(arguments: [false, true])
    func authenticationDenialClearsSessionWithoutWritingCloneList(settingsDiscovery: Bool) async throws {
        let fixture = try CloneInventoryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        let inventory = try fixture.storage.accountRoot("123").appendingPathComponent("clones.json")
        try FileManager.default.removeItem(at: inventory)
        try FileManager.default.createDirectory(at: inventory, withIntermediateDirectories: false)
        await fixture.network.release(.success(CloneInventoryTransport.response(status: 401)))
        await refresh.value
        #expect(!fixture.workspace.authStatus.signedIn)
        #expect(fixture.workspace.snapshot.accountID.isEmpty)
        #expect(fixture.workspace.snapshot.repositories.isEmpty)
        #expect(!fixture.workspace.error.isEmpty)
        #expect(!fixture.workspace.busy)
    }
}

@MainActor private struct CloneInventoryFixture {
    let history: HistoryFixture
    let storage: WorkspaceStorage
    let token = CLITokenSource()
    let user = CLIUserTransport()
    let network: CloneInventoryTransport
    let workspace: WorkspaceModel
    init(repositories: [String] = [], gate: String = "/user") throws {
        let history = try HistoryFixture()
        self.history = history
        let base = history.base
        storage = WorkspaceStorage(supportRoot: base.appendingPathComponent("support"), cacheRoot: base.appendingPathComponent("workspace-cache"))
        var config = WorkspaceConfiguration(); config.cachedOnly = true; config.repositories = repositories
        config.sourceRoots = [history.sources.path]
        try storage.saveConfiguration(config, account: "123")
        let saved = repositories.isEmpty ? ["owner/repo", "other/keep"] : repositories
        try storage.saveClones(try saved.map { $0 == history.id ? try history.repository() : Repository(id: $0) }, account: "123")
        let other = base.appendingPathComponent("other-src")
        try history.addClone("other/new", at: other.appendingPathComponent("other/new"))
        var second = WorkspaceConfiguration(); second.sourceRoots = [other.path]
        try storage.saveConfiguration(second, account: "456")
        try storage.saveClones([Repository(id: "separate/private")], account: "456")
        network = CloneInventoryTransport(gate: gate)
        workspace = WorkspaceModel(storage: storage, authentication: cliAuth(storage: storage, token: token, user: user), transport: network, refreshHistory: { _, repo, _ in repo })
    }
    func authentication() -> GitHubCLIAuth { cliAuth(storage: storage) }
    func begin(settingsDiscovery: Bool) -> Task<Void, Never> {
        if !settingsDiscovery { workspace.configuration.cachedOnly = false }
        return Task { if settingsDiscovery { await workspace.discover() } else { await workspace.refresh() } }
    }
    func remove() { try? FileManager.default.removeItem(at: history.base) }
}

private actor CloneInventoryTransport: GitHubTransport {
    private let gate: String
    private var waiter: CheckedContinuation<GitHubHTTPResponse, any Error>?
    private var cancelled = false
    private var released: Result<GitHubHTTPResponse, any Error>?
    init(gate: String) { self.gate = gate }
    static func response(status: Int = 200, body: String = "{}") -> GitHubHTTPResponse { GitHubHTTPResponse(status: status, headers: [:], data: Data(body.utf8)) }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        if request.value(forHTTPHeaderField: "Authorization") == "Bearer switched-access" {
            switch request.url?.path {
            case "/user": return Self.response(body: #"{"login":"other","id":456}"#)
            default: throw GitHubFailure.unavailable
            }
        }
        if request.url?.path == gate {
            if let released { return try released.get() }
            return try await withTaskCancellationHandler {
                try await withCheckedThrowingContinuation { waiter = $0 }
            } onCancel: { Task { await self.recordCancellation() } }
        }
        switch request.url?.path {
        case "/user": return Self.response(body: #"{"login":"fixture","id":123}"#)
        default: throw GitHubFailure.unavailable
        }
    }
    private func recordCancellation() { cancelled = true }
    func waitUntilBlocked() async {
        let deadline = ContinuousClock.now.advanced(by: .seconds(5))
        while waiter == nil && ContinuousClock.now < deadline { try? await Task.sleep(for: .milliseconds(5)) }
        #expect(waiter != nil)
    }
    func waitUntilCancelled() async {
        let deadline = ContinuousClock.now.advanced(by: .seconds(5))
        while !cancelled && ContinuousClock.now < deadline { try? await Task.sleep(for: .milliseconds(5)) }
        #expect(cancelled)
    }
    func release(_ result: Result<GitHubHTTPResponse, any Error>) { released = result; waiter?.resume(with: result); waiter = nil }
}

private struct UnavailableTransport: GitHubTransport {
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse { throw GitHubFailure.unavailable }
}
