import Foundation
import Testing
@testable import Bigboard

@Suite @MainActor struct GrantRecoveryTests {
    @Test(arguments: [false, true])
    func orderlyQuitPreservesOfflineHistory(settingsDiscovery: Bool) async throws {
        let history = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: history.base) }
        let fixture = try GrantRecoveryFixture(base: history.base, repositories: ["owner/repo"])
        let destination = try fixture.storage.repositoryRoot("123").appendingPathComponent("github.com/owner/repo.git")
        try FileManager.default.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true)
        try FileManager.default.copyItem(at: history.bare, to: destination)
        await fixture.workspace.start()
        let commits = fixture.workspace.snapshot.commits.map(\.id)
        #expect(!commits.isEmpty)
        var config = fixture.workspace.configuration; config.cachedOnly = false
        try fixture.storage.saveConfiguration(config, account: "123")
        fixture.workspace.configuration = config
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        #expect(try fixture.storage.loadGrant("123").map(\.id) == ["owner/repo"])
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        await fixture.network.release(.failure(CancellationError()))
        await shutdown.value; await refresh.value
        #expect(try fixture.storage.loadGrant("123").map(\.id) == ["owner/repo"])
        let restarted = WorkspaceModel(storage: fixture.storage, authentication: fixture.authentication(), transport: UnavailableGrantTransport())
        await restarted.start()
        #expect(restarted.snapshot.repositories.map(\.id) == ["owner/repo"])
        #expect(restarted.snapshot.commits.map(\.id) == commits)
        #expect(restarted.snapshot.repositories.allSatisfy { $0.stale })
        config.cachedOnly = true
        try fixture.storage.saveConfiguration(config, account: "123")
        let cached = WorkspaceModel(storage: fixture.storage, authentication: fixture.authentication(), transport: UnavailableGrantTransport())
        await cached.start()
        #expect(cached.snapshot.cachedOnly)
        #expect(cached.snapshot.commits.map(\.id) == commits)
    }

    @Test(arguments: [false, true], [401, 403, 404])
    func shutdownRecordsDeliveredDenial(settingsDiscovery: Bool, status: Int) async throws {
        let fixture = try GrantRecoveryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        await fixture.network.release(.success(GrantRecoveryTransport.response(status: status)))
        await shutdown.value; await refresh.value
        #expect(try fixture.storage.loadGrant("123").isEmpty)
        #expect(try fixture.storage.loadGrant("456").map(\.id) == ["separate/private"])
    }

    @Test(arguments: [false, true])
    func shutdownRecordsCompletedNarrowerInventory(settingsDiscovery: Bool) async throws {
        let fixture = try GrantRecoveryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        let response = GrantRecoveryTransport.response(body: #"[{"full_name":"other/keep","default_branch":"main","archived":false}]"#)
        await fixture.network.release(.success(response))
        await shutdown.value; await refresh.value
        #expect(try fixture.storage.loadGrant("123").map(\.id) == ["other/keep"])
        #expect(try fixture.storage.loadGrant("456").map(\.id) == ["separate/private"])
    }

    @Test(arguments: [false, true])
    func shutdownDoesNotHideDeliveredViewerAccountMismatch(settingsDiscovery: Bool) async throws {
        let fixture = try GrantRecoveryFixture(gate: "/user")
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        await fixture.network.release(.success(GrantRecoveryTransport.response(body: #"{"login":"other","id":456}"#)))
        await shutdown.value; await refresh.value
        #expect(try fixture.storage.loadGrant("123").isEmpty)
        #expect(try fixture.storage.loadGrant("456").map(\.id) == ["separate/private"])
    }

    @Test(arguments: [false, true])
    func accountChangeSupersedesShutdownAccessResult(settingsDiscovery: Bool) async throws {
        let fixture = try GrantRecoveryFixture()
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
        #expect(try fixture.storage.loadGrant("456").map(\.id) == ["other/new"])
        await fixture.network.release(.success(GrantRecoveryTransport.response(body: "[]")))
        await shutdown.value; await refresh.value
        #expect(fixture.workspace.snapshot.accountID == "456")
        #expect(try fixture.storage.loadGrant("123").map(\.id) == ["owner/repo", "other/keep"])
        #expect(try fixture.storage.loadGrant("456").map(\.id) == ["other/new"])
    }

    @Test(arguments: [false, true])
    func settingsAndRefreshCancellationPreserveGrantWithoutShutdown(settingsDiscovery: Bool) async throws {
        let fixture = try GrantRecoveryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        if settingsDiscovery { refresh.cancel(); await fixture.network.waitUntilCancelled() }
        await fixture.network.release(.failure(CancellationError()))
        await refresh.value
        #expect(try fixture.storage.loadGrant("123").map(\.id) == ["owner/repo", "other/keep"])
        #expect(!fixture.workspace.busy)
        #expect(fixture.workspace.authStatus.signedIn)
    }

    @Test(arguments: [false, true])
    func authenticationDenialStillClearsSessionWhenGrantWriteFails(settingsDiscovery: Bool) async throws {
        let fixture = try GrantRecoveryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin(settingsDiscovery: settingsDiscovery)
        await fixture.network.waitUntilBlocked()
        let grant = try fixture.storage.accountRoot("123").appendingPathComponent("access.json")
        try FileManager.default.removeItem(at: grant)
        try FileManager.default.createDirectory(at: grant, withIntermediateDirectories: false)
        await fixture.network.release(.success(GrantRecoveryTransport.response(status: 401)))
        await refresh.value
        #expect(!fixture.workspace.authStatus.signedIn)
        #expect(fixture.workspace.snapshot.accountID.isEmpty)
        #expect(fixture.workspace.snapshot.repositories.isEmpty)
        #expect(!fixture.workspace.error.isEmpty)
        #expect(!fixture.workspace.busy)
    }
}

@MainActor private struct GrantRecoveryFixture {
    let base: URL
    let storage: WorkspaceStorage
    let token = CLITokenSource()
    let user = CLIUserTransport()
    let network: GrantRecoveryTransport
    let workspace: WorkspaceModel
    init(base: URL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString), repositories: [String] = [], gate: String = "/user/repos") throws {
        self.base = base
        storage = WorkspaceStorage(supportRoot: base.appendingPathComponent("support"), cacheRoot: base.appendingPathComponent("workspace-cache"))
        var config = WorkspaceConfiguration(); config.cachedOnly = true; config.repositories = repositories
        try storage.saveConfiguration(config, account: "123")
        let grant = repositories.isEmpty ? ["owner/repo", "other/keep"] : repositories
        try storage.saveGrant(grant.map { Repository(id: $0) }, account: "123")
        try storage.saveGrant([Repository(id: "separate/private")], account: "456")
        network = GrantRecoveryTransport(gate: gate)
        workspace = WorkspaceModel(storage: storage, authentication: cliAuth(storage: storage, token: token, user: user), transport: network)
    }
    func authentication() -> GitHubCLIAuth { cliAuth(storage: storage) }
    func begin(settingsDiscovery: Bool) -> Task<Void, Never> {
        if !settingsDiscovery { workspace.configuration.cachedOnly = false }
        return Task { if settingsDiscovery { await workspace.discover() } else { await workspace.refresh() } }
    }
    func remove() { try? FileManager.default.removeItem(at: base) }
}

private actor GrantRecoveryTransport: GitHubTransport {
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
            case "/user/repos": return Self.response(body: #"[{"full_name":"other/new","default_branch":"","archived":false}]"#)
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
        case "/user/repos": return Self.response(body: #"[{"full_name":"owner/repo","default_branch":"main","archived":false}]"#)
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

private struct UnavailableGrantTransport: GitHubTransport {
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse { throw GitHubFailure.unavailable }
}
