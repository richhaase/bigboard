import Foundation
import Testing
@testable import Bigboard

private final class WorkspaceTestCredentials: CredentialStore, @unchecked Sendable {
    private let lock = NSLock()
    private var value: Data?
    private var calls = 0
    init(_ value: Data? = nil) { self.value = value }
    func load() throws -> Data? { lock.withLock { calls += 1; return value } }
    func save(_ data: Data) throws { lock.withLock { calls += 1; value = data } }
    func delete() throws { lock.withLock { calls += 1; value = nil } }
    var count: Int { lock.withLock { calls } }
}
private actor DelayedWorkspaceTransport: GitHubTransport {
    private var waiter: CheckedContinuation<GitHubHTTPResponse, Never>?
    private(set) var count = 0
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        count += 1
        return await withCheckedContinuation { waiter = $0 }
    }
    func release() {
        waiter?.resume(returning: GitHubHTTPResponse(status: 200, headers: [:], data: Data(#"{"total_count":0,"installations":[]}"#.utf8)))
        waiter = nil
    }
}
private func workspaceTestAuth(_ store: WorkspaceTestCredentials) -> AuthManager {
    AuthManager(configuration: AuthConfiguration(clientID: "fixture-client", clientSecret: "fixture-secret", appSlug: "fixture-app"), store: store)
}
private func workspaceTestStore() throws -> WorkspaceTestCredentials {
    let now = Date()
    let value = AuthCredentials(accessToken: "fixture-access", refreshToken: "fixture-refresh", expiresAt: now.addingTimeInterval(3600), refreshExpiresAt: now.addingTimeInterval(86400), login: "fixture", accountID: "123", clientID: "fixture-client")
    let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
    return WorkspaceTestCredentials(try encoder.encode(value))
}
@Test @MainActor func signOutSuppressesLateWorkspaceRefreshAndClearsAccountMetadata() async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    let store = try workspaceTestStore()
    let network = DelayedWorkspaceTransport()
    let workspace = WorkspaceModel(storage: storage, authentication: workspaceTestAuth(store), transport: network)
    let startup = Task { await workspace.start() }
    for _ in 0..<100 {
        if await network.count > 0 { break }
        try await Task.sleep(for: .milliseconds(5))
    }
    #expect(await network.count == 1)
    await workspace.refresh()
    #expect(await network.count == 1)
    workspace.cachedRepositoryRows = [Repository(id: "private/repo")]
    workspace.discovered = [Repository(id: "private/repo")]
    workspace.activeGroup = "Old account"
    await workspace.signOut()
    await network.release()
    await startup.value
    #expect(workspace.authStatus.signedIn == false)
    #expect(workspace.snapshot.accountID.isEmpty)
    #expect(workspace.snapshot.repositories.isEmpty && workspace.snapshot.commits.isEmpty)
    #expect(workspace.cachedRepositoryRows.isEmpty && workspace.discovered.isEmpty)
    #expect(workspace.removableCacheIDs.isEmpty && workspace.activeGroup.isEmpty)
    #expect(workspace.busy == false)
    #expect(try storage.loadGrant("123").isEmpty)
}
private actor SequenceWorkspaceTransport: GitHubTransport {
    private var responses: [GitHubHTTPResponse]
    init(_ statuses: [Int]) {
        responses = statuses.map { GitHubHTTPResponse(status: $0, headers: [:], data: Data(($0 == 200 ? #"{"total_count":0,"installations":[]}"# : "{}").utf8)) }
    }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        if request.url?.path == "/user" { return GitHubHTTPResponse(status: 200, headers: [:], data: Data(#"{"login":"fixture","id":123}"#.utf8)) }
        guard !responses.isEmpty else { throw GitHubFailure.unavailable }
        return responses.removeFirst()
    }
}
@Test(arguments: [401, 403]) @MainActor func workspaceSeparatesAuthenticationAndRepositoryPermissionDenials(status: Int) async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    try storage.saveGrant([Repository(id: "private/repo")], account: "123")
    let store = try workspaceTestStore()
    let auth = workspaceTestAuth(store)
    let workspace = WorkspaceModel(storage: storage, authentication: auth, transport: SequenceWorkspaceTransport([status]))
    await workspace.start()
    #expect(workspace.authStatus.signedIn == (status == 403))
    #expect(await auth.status().signedIn == (status == 403))
    #expect(workspace.snapshot.repositories.isEmpty && workspace.snapshot.commits.isEmpty)
    #expect(try storage.loadGrant("123").isEmpty)
    #expect(try store.load() != nil)
    #expect(workspace.busy == false)
    if status == 401 {
        #expect(workspace.snapshot.coverage.phase == "signin")
        #expect(workspace.snapshot.accountID.isEmpty)
        #expect(workspace.cachedRepositoryRows.isEmpty && workspace.discovered.isEmpty)
    }
}
@Test @MainActor func settingsDiscoveryAuthenticationFailureInvalidatesSession() async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    let store = try workspaceTestStore()
    let workspace = WorkspaceModel(storage: storage, authentication: workspaceTestAuth(store), transport: SequenceWorkspaceTransport([200, 401]))
    await workspace.start()
    #expect(workspace.authStatus.signedIn)
    workspace.cachedRepositoryRows = [Repository(id: "private/repo")]
    workspace.discovered = [Repository(id: "private/repo")]
    await workspace.discover()
    #expect(workspace.authStatus.signedIn == false)
    #expect(workspace.snapshot.coverage.phase == "signin")
    #expect(workspace.cachedRepositoryRows.isEmpty && workspace.discovered.isEmpty)
    #expect(try storage.loadGrant("123").isEmpty)
    #expect(try store.load() != nil)
}
private final class WaitingWorkspaceCredentials: CredentialStore, @unchecked Sendable {
    private let condition = NSCondition()
    private var released = false
    private var loading = false
    func load() throws -> Data? {
        condition.lock(); defer { condition.unlock() }
        loading = true
        while !released { condition.wait() }
        return nil
    }
    func save(_ data: Data) throws {}
    func delete() throws {}
    var isLoading: Bool { condition.withLock { loading } }
    func release() { condition.lock(); released = true; condition.broadcast(); condition.unlock() }
}
@Test @MainActor func initialSignInCheckRemainsExplicitUntilCredentialReadCompletes() async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let store = WaitingWorkspaceCredentials()
    defer { store.release() }
    let auth = AuthManager(configuration: AuthConfiguration(clientID: "fixture-client", clientSecret: "fixture-secret", appSlug: "fixture-app"), store: store)
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    let workspace = WorkspaceModel(storage: storage, authentication: auth, transport: SequenceWorkspaceTransport([]))
    #expect(workspace.checkingSignIn)
    let startup = Task { await workspace.start() }
    for _ in 0..<100 {
        if store.isLoading { break }
        try await Task.sleep(for: .milliseconds(5))
    }
    #expect(store.isLoading)
    #expect(workspace.checkingSignIn)
    store.release()
    await startup.value
    #expect(workspace.checkingSignIn == false)
    #expect(workspace.authStatus.configured)
    #expect(workspace.authStatus.signedIn == false)
}
private actor ConfigurationWorkspaceTransport: GitHubTransport {
    private var responses: [GitHubHTTPResponse]
    private var waiter: CheckedContinuation<GitHubHTTPResponse, Never>?
    private(set) var count = 0
    init(initialFailure: Bool = false) {
        responses = [
            GitHubHTTPResponse(status: 200, headers: [:], data: Data(#"{"total_count":1,"installations":[{"id":1,"account":{"login":"owner","type":"User"}}]}"#.utf8)),
            GitHubHTTPResponse(status: 200, headers: [:], data: Data(#"{"total_count":1,"repositories":[{"full_name":"owner/repo","default_branch":"main","archived":false,"pushed_at":"2026-10-31T12:00:00Z"}]}"#.utf8))
        ]
        if initialFailure { responses.insert(GitHubHTTPResponse(status: 503, headers: [:], data: Data("{}".utf8)), at: 0) }
    }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        count += 1
        if request.url?.path == "/user" { return GitHubHTTPResponse(status: 200, headers: [:], data: Data(#"{"login":"fixture","id":123}"#.utf8)) }
        if !responses.isEmpty { return responses.removeFirst() }
        return await withCheckedContinuation { waiter = $0 }
    }
    func release() {
        waiter?.resume(returning: GitHubHTTPResponse(status: 503, headers: [:], data: Data("{}".utf8)))
        waiter = nil
    }
}
@Test(arguments: [false, true]) @MainActor func configurationApplyReturnsAfterLocalSaveAndRefreshesIndependently(initialFailure: Bool) async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    let network = ConfigurationWorkspaceTransport(initialFailure: initialFailure)
    let workspace = WorkspaceModel(storage: storage, authentication: workspaceTestAuth(try workspaceTestStore()), transport: network)
    await workspace.start()
    #expect(workspace.configuration.repositories.isEmpty)
    var draft = workspace.configuration; draft.repositories = ["owner/repo"]
    let saved = await workspace.saveConfiguration(draft)
    #expect(saved)
    #expect(workspace.configuration == draft)
    #expect(try storage.loadConfiguration("123") == draft)
    #expect(workspace.busy)
    #expect(workspace.snapshot.coverage.phase == "refreshing")
    let expected = initialFailure ? 7 : 5
    for _ in 0..<100 {
        if await network.count == expected { break }
        try await Task.sleep(for: .milliseconds(5))
    }
    #expect(await network.count == expected)
    await workspace.signOut()
    await network.release()
}
@Test @MainActor func configurationApplyFailurePreservesDraftAndSavedConfiguration() async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    let network = ConfigurationWorkspaceTransport()
    let workspace = WorkspaceModel(storage: storage, authentication: workspaceTestAuth(try workspaceTestStore()), transport: network)
    await workspace.start()
    let original = workspace.configuration
    var unavailable = original; unavailable.repositories = ["other/private"]
    #expect(await workspace.saveConfiguration(unavailable) == false)
    #expect(unavailable.repositories == ["other/private"])
    #expect(workspace.configuration == original)
    #expect(workspace.error.contains("other/private"))
    var draft = original; draft.repositories = ["owner/repo"]
    let settingsURL = try storage.accountRoot("123").appendingPathComponent("workspace.json")
    try FileManager.default.createDirectory(at: settingsURL.deletingLastPathComponent(), withIntermediateDirectories: true)
    try JSONEncoder().encode(WorkspaceConfiguration()).write(to: settingsURL)
    #expect(await workspace.saveConfiguration(draft) == false)
    #expect(workspace.configuration == original)
    #expect(draft.repositories == ["owner/repo"])
    #expect(workspace.error.contains("changed outside"))
    #expect(workspace.busy == false)
    #expect(await network.count == 3)
}

private actor ViewerWorkspaceTransport: GitHubTransport {
    private var users: [String]
    private(set) var count = 0
    init(_ users: [String]) { self.users = users }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        count += 1
        if request.url?.path == "/user", !users.isEmpty {
            return GitHubHTTPResponse(status: 200, headers: [:], data: Data(users.removeFirst().utf8))
        }
        if request.url?.path == "/user/installations" {
            return GitHubHTTPResponse(status: 200, headers: [:], data: Data(#"{"total_count":0,"installations":[]}"#.utf8))
        }
        throw GitHubFailure.unavailable
    }
}
@Test(arguments: [false, true]) @MainActor func liveCollectionRefreshesViewerForMineAndAttention(settingsDiscovery: Bool) async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    let store = try workspaceTestStore()
    let auth = workspaceTestAuth(store)
    let network = ViewerWorkspaceTransport([#"{"login":"fixture","id":123}"#, #"{"login":"renamed","id":123}"#])
    let workspace = WorkspaceModel(storage: storage, authentication: auth, transport: network)
    await workspace.start()
    #expect(workspace.snapshot.viewer == "fixture")
    if settingsDiscovery { await workspace.discover() } else { await workspace.refresh() }
    #expect(workspace.authStatus.login == "renamed")
    #expect(workspace.snapshot.viewer == "renamed" && workspace.snapshot.accountID == "123")
    #expect(await workspaceTestAuth(store).status().login == "renamed")
    var authored = PullRequest(repo: "owner/repo", number: 1, title: "Own change", author: "renamed", url: "", updatedAt: Date())
    authored.checks = "FAILURE"
    var requested = PullRequest(repo: "owner/repo", number: 2, title: "Review change", author: "other", url: "", updatedAt: Date())
    requested.reviewers = ["renamed"]
    let rows = [authored, requested]
    #expect(Analysis.filterPRs(rows, viewer: workspace.snapshot.viewer, filters: PRFilters(mine: ["authored"])).map(\.number) == [1])
    #expect(Analysis.filterPRs(rows, viewer: workspace.snapshot.viewer, filters: PRFilters(mine: ["requested"])).map(\.number) == [2])
    #expect(Set(Analysis.filterPRs(rows, viewer: workspace.snapshot.viewer, filters: PRFilters(mine: ["attention"])).map(\.number)) == [1, 2])
    #expect(await network.count == 4)
}
@Test @MainActor func changedLiveViewerAccountInvalidatesWorkspace() async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    try storage.saveGrant([Repository(id: "private/repo")], account: "123")
    let store = try workspaceTestStore()
    let network = ViewerWorkspaceTransport([#"{"login":"other","id":456}"#])
    let workspace = WorkspaceModel(storage: storage, authentication: workspaceTestAuth(store), transport: network)
    await workspace.start()
    #expect(!workspace.authStatus.signedIn)
    #expect(workspace.snapshot.accountID.isEmpty && workspace.snapshot.viewer.isEmpty)
    #expect(workspace.snapshot.repositories.isEmpty && workspace.discovered.isEmpty)
    #expect(try storage.loadGrant("123").isEmpty)
    #expect(try store.load() != nil)
    #expect(await network.count == 1)
}
@Test(arguments: [false, true]) @MainActor func removingActiveGroupRestoresAllRepositories(rename: Bool) async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    var config = try storage.loadConfiguration("123")
    config.repositories = ["fixture/one", "fixture/two"]
    config.groups = ["Focus": ["fixture/one"]]
    config.cachedOnly = true
    try storage.saveConfiguration(config, account: "123")
    try storage.saveGrant(config.repositories.map { Repository(id: $0) }, account: "123")
    let network = ViewerWorkspaceTransport([])
    let workspace = WorkspaceModel(storage: storage, authentication: workspaceTestAuth(try workspaceTestStore()), transport: network)
    await workspace.start()
    await workspace.selectGroup("Focus")
    #expect(workspace.snapshot.repositories.map(\.id) == ["fixture/one"])
    config.groups = rename ? ["Renamed": ["fixture/one"]] : [:]
    #expect(await workspace.saveConfiguration(config))
    #expect(workspace.activeGroup.isEmpty)
    for _ in 0..<100 {
        if !workspace.busy { break }
        try await Task.sleep(for: .milliseconds(5))
    }
    #expect(!workspace.busy)
    #expect(Set(workspace.snapshot.repositories.map(\.id)) == Set(config.repositories))
    await workspace.selectGroup("Focus")
    #expect(workspace.activeGroup.isEmpty)
    #expect(Set(workspace.snapshot.repositories.map(\.id)) == Set(config.repositories))
    #expect(await network.count == 0)
    await workspace.shutdown()
}
