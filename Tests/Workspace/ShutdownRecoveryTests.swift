import Foundation
import Testing
@testable import Bigboard

@Suite @MainActor struct ShutdownRecoveryTests {
    @Test func orderlyQuitPreservesOfflineHistory() async throws {
        let fixture = try ShutdownRecoveryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let commits = fixture.workspace.snapshot.commits.map(\.id)
        #expect(!commits.isEmpty)
        var config = fixture.workspace.configuration; config.cachedOnly = false
        try fixture.storage.saveConfiguration(config)
        fixture.workspace.configuration = config
        let refresh = fixture.begin()
        await fixture.network.waitUntilBlocked()
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        await fixture.network.release(.failure(CancellationError()))
        await shutdown.value; await refresh.value
        #expect(try fixture.storage.loadConfiguration() == config)
        let restarted = WorkspaceModel(storage: fixture.storage, authentication: fixture.authentication(), transport: UnavailableTransport(), refreshHistory: { _, repo in repo })
        await restarted.start()
        #expect(restarted.snapshot.repositories.map(\.id) == ["owner/repo"])
        #expect(restarted.snapshot.commits.map(\.id) == commits)
        #expect(restarted.snapshot.repositories.allSatisfy { $0.stale })
        config.cachedOnly = true
        try fixture.storage.saveConfiguration(config)
        let cached = WorkspaceModel(storage: fixture.storage, authentication: fixture.authentication(), transport: UnavailableTransport(), refreshHistory: { _, repo in repo })
        await cached.start()
        #expect(cached.snapshot.cachedOnly)
        #expect(cached.snapshot.commits.map(\.id) == commits)
    }

    @Test(arguments: [200, 401, 403, 404])
    func shutdownDuringViewerCheckLeavesSettingsUntouched(status: Int) async throws {
        let fixture = try ShutdownRecoveryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let saved = try Data(contentsOf: fixture.storage.configurationFile)
        let refresh = fixture.begin()
        await fixture.network.waitUntilBlocked()
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        let body = status == 200 ? #"{"login":"fixture","id":123}"# : "{}"
        await fixture.network.release(.success(ShutdownRecoveryTransport.response(status: status, body: body)))
        await shutdown.value; await refresh.value
        #expect(try Data(contentsOf: fixture.storage.configurationFile) == saved)
        #expect(fixture.workspace.discovered.map(\.id) == ["owner/repo"])
    }

    @Test func accountChangeDuringShutdownOpensTheSameSettings() async throws {
        let fixture = try ShutdownRecoveryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin()
        await fixture.network.waitUntilBlocked()
        let shutdown = Task { await fixture.workspace.shutdown() }
        await fixture.network.waitUntilCancelled()
        fixture.token.set("switched-access")
        fixture.user.respond(login: "other", account: "456")
        await fixture.workspace.checkConnection()
        #expect(fixture.workspace.authStatus.accountID == "456")
        #expect(fixture.workspace.snapshot.accountID == "456")
        #expect(fixture.workspace.configuration == (try fixture.storage.loadConfiguration()))
        #expect(fixture.workspace.discovered.map(\.id) == ["owner/repo"])
        await fixture.network.release(.success(ShutdownRecoveryTransport.response(body: #"{"login":"fixture","id":123}"#)))
        await shutdown.value; await refresh.value
        #expect(fixture.workspace.snapshot.accountID == "456")
    }

    @Test func refreshCancellationWithoutShutdownKeepsTheSession() async throws {
        let fixture = try ShutdownRecoveryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let refresh = fixture.begin()
        await fixture.network.waitUntilBlocked()
        refresh.cancel()
        await fixture.network.release(.failure(CancellationError()))
        await refresh.value
        #expect(!fixture.workspace.busy)
        #expect(fixture.workspace.authStatus.signedIn)
        #expect(fixture.workspace.discovered.map(\.id) == ["owner/repo"])
    }

    @Test func authenticationDenialClearsSessionButKeepsSettings() async throws {
        let fixture = try ShutdownRecoveryFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let saved = try Data(contentsOf: fixture.storage.configurationFile)
        let refresh = fixture.begin()
        await fixture.network.waitUntilBlocked()
        await fixture.network.release(.success(ShutdownRecoveryTransport.response(status: 401)))
        await refresh.value
        #expect(!fixture.workspace.authStatus.signedIn)
        #expect(fixture.workspace.snapshot.accountID.isEmpty)
        #expect(fixture.workspace.snapshot.repositories.isEmpty && fixture.workspace.discovered.isEmpty)
        #expect(!fixture.workspace.error.isEmpty)
        #expect(!fixture.workspace.busy)
        #expect(try Data(contentsOf: fixture.storage.configurationFile) == saved)
    }
}

@MainActor private struct ShutdownRecoveryFixture {
    let history: HistoryFixture
    let storage: WorkspaceStorage
    let token = CLITokenSource()
    let user = CLIUserTransport()
    let network: ShutdownRecoveryTransport
    let workspace: WorkspaceModel
    init() throws {
        let history = try HistoryFixture()
        self.history = history
        let base = history.base
        storage = WorkspaceStorage(configRoot: base.appendingPathComponent("support"), cacheRoot: base.appendingPathComponent("workspace-cache"))
        var config = WorkspaceConfiguration(); config.cachedOnly = true; config.repositories = [history.id]
        config.sourceRoots = [history.sources.path]
        try storage.saveConfiguration(config)
        network = ShutdownRecoveryTransport(gate: "/user")
        workspace = WorkspaceModel(storage: storage, authentication: cliAuth(storage: storage, token: token, user: user), transport: network, refreshHistory: { _, repo in repo })
    }
    func authentication() -> GitHubCLIAuth { cliAuth(storage: storage) }
    func begin() -> Task<Void, Never> {
        workspace.configuration.cachedOnly = false
        return Task { await workspace.refresh() }
    }
    func remove() { try? FileManager.default.removeItem(at: history.base) }
}

private actor ShutdownRecoveryTransport: GitHubTransport {
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
