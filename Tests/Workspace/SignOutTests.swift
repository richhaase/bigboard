import Foundation
import Testing
@testable import Bigboard

private final class SignOutCredentialStore: CredentialStore, @unchecked Sendable {
    private let lock = NSLock()
    private var data: Data?
    private var saveFails = false
    private var deleteFails = false
    init(_ data: Data? = nil) { self.data = data }
    func load() throws -> Data? { lock.withLock { data } }
    func save(_ data: Data) throws { try lock.withLock { if saveFails { throw AuthFailure.storage }; self.data = data } }
    func delete() throws { try lock.withLock { if deleteFails { throw AuthFailure.storage }; data = nil } }
    func failSave(_ value: Bool) { lock.withLock { saveFails = value } }
    func failDelete(_ value: Bool) { lock.withLock { deleteFails = value } }
}
private let signOutNow = Date(timeIntervalSince1970: 1_800_000_000)
private let signOutConfiguration = AuthConfiguration(clientID: "fixture-client", clientSecret: "fixture-secret", appSlug: "fixture-app")
private func signOutCredentials(expired: Bool = false) throws -> Data {
    let value = AuthCredentials(accessToken: "fixture-access", refreshToken: "fixture-refresh", expiresAt: signOutNow.addingTimeInterval(expired ? -10 : 3600), refreshExpiresAt: signOutNow.addingTimeInterval(86400), login: "fixture", accountID: "123", clientID: "fixture-client")
    let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
    return try encoder.encode(value)
}
private func signOutAuth(_ store: SignOutCredentialStore, transport: any GitHubTransport) -> AuthManager {
    AuthManager(configuration: signOutConfiguration, store: store, transport: transport, now: { signOutNow })
}
private func signOutResponse(_ json: String) -> GitHubHTTPResponse { GitHubHTTPResponse(status: 200, headers: [:], data: Data(json.utf8)) }
private actor SignOutAuthTransport: GitHubTransport {
    private var waiter: CheckedContinuation<GitHubHTTPResponse, Never>?
    private let delayed: Bool
    private(set) var exchanges = 0
    init(delayed: Bool = false) { self.delayed = delayed }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        if request.url?.path == "/user" { return signOutResponse(#"{"login":"fixture","id":123}"#) }
        guard request.url?.path == "/login/oauth/access_token" else { throw GitHubFailure.unavailable }
        exchanges += 1
        if delayed { return await withCheckedContinuation { waiter = $0 } }
        return replacement()
    }
    func release() { waiter?.resume(returning: replacement()); waiter = nil }
    private func replacement() -> GitHubHTTPResponse { signOutResponse(#"{"access_token":"fixture-new-access","refresh_token":"fixture-new-refresh","token_type":"bearer","expires_in":28800,"refresh_token_expires_in":15811200}"#) }
}

@Test func failedSignOutPreservesUnsavedRotatedCredential() async throws {
    let store = SignOutCredentialStore(try signOutCredentials(expired: true))
    store.failSave(true); store.failDelete(true)
    let transport = SignOutAuthTransport()
    let auth = signOutAuth(store, transport: transport)
    do { _ = try await auth.token(); Issue.record("Expected credential persistence failure") }
    catch { #expect(error as? AuthFailure == .storage) }
    do { try await auth.signOut(); Issue.record("Expected credential deletion failure") }
    catch { #expect(error as? AuthFailure == .storage) }
    #expect(await auth.status().signedIn)
    store.failSave(false)
    #expect(try await auth.token() == "fixture-new-access")
    #expect(await transport.exchanges == 1)
    let reloaded = signOutAuth(store, transport: transport)
    #expect(try await reloaded.token() == "fixture-new-access")
    store.failDelete(false)
    try await auth.signOut()
    #expect(try store.load() == nil)
}
@Test func failedSignOutDoesNotDiscardInFlightRotation() async throws {
    let store = SignOutCredentialStore(try signOutCredentials(expired: true)); store.failDelete(true)
    let transport = SignOutAuthTransport(delayed: true)
    let auth = signOutAuth(store, transport: transport)
    let pending = Task { try await auth.token() }
    for _ in 0..<100 {
        if await transport.exchanges > 0 { break }
        try await Task.sleep(for: .milliseconds(5))
    }
    #expect(await transport.exchanges == 1)
    do { try await auth.signOut(); Issue.record("Expected credential deletion failure") }
    catch { #expect(error as? AuthFailure == .storage) }
    await transport.release()
    #expect(try await pending.value == "fixture-new-access")
    #expect(try await signOutAuth(store, transport: transport).token() == "fixture-new-access")
    #expect(await transport.exchanges == 1)
}
@Test(arguments: [false, true]) func successfulSignOutPreventsLateAuthenticationPersistence(exchange: Bool) async throws {
    let store = SignOutCredentialStore(exchange ? nil : try signOutCredentials(expired: true))
    let transport = SignOutAuthTransport(delayed: true)
    let auth = signOutAuth(store, transport: transport)
    let pending: Task<Void, Error>
    if exchange {
        let url = try await auth.start()
        let state = try #require(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "state" }?.value)
        let callback = try #require(URL(string: "com.richhaase.bigboard://oauth/github?state=\(state)&code=fixture-code"))
        pending = Task { try await auth.complete(callback) }
    } else { pending = Task { _ = try await auth.token() } }
    for _ in 0..<100 {
        if await transport.exchanges > 0 { break }
        try await Task.sleep(for: .milliseconds(5))
    }
    #expect(await transport.exchanges == 1)
    try await auth.signOut()
    await transport.release()
    do { try await pending.value; Issue.record("A signed-out authentication task completed") }
    catch { #expect(error is CancellationError) }
    #expect(try store.load() == nil)
    #expect(!((await auth.status()).signedIn))
    #expect(!((await signOutAuth(store, transport: transport).status()).signedIn))
}

private actor SignOutWorkspaceTransport: GitHubTransport {
    private var waiter: CheckedContinuation<GitHubHTTPResponse, Never>?
    private(set) var requests = 0
    private(set) var inventoryStarted = false
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        requests += 1
        if request.url?.path == "/user" { return signOutResponse(#"{"login":"fixture","id":123}"#) }
        guard request.url?.path == "/user/installations" else { throw GitHubFailure.unavailable }
        inventoryStarted = true
        return await withCheckedContinuation { waiter = $0 }
    }
    func release() { waiter?.resume(returning: signOutResponse(#"{"total_count":0,"installations":[]}"#)); waiter = nil }
}
@Test @MainActor func failedWorkspaceSignOutRemainsHonestAcrossRestartAndAllowsRetry() async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    var config = try storage.loadConfiguration("123"); config.cachedOnly = true; config.repositories = ["owner/repo"]
    try storage.saveConfiguration(config, account: "123")
    try storage.saveGrant([Repository(id: "owner/repo")], account: "123")
    let saved = try signOutCredentials()
    let store = SignOutCredentialStore(saved); store.failDelete(true)
    let transport = SignOutWorkspaceTransport()
    let workspace = WorkspaceModel(storage: storage, authentication: signOutAuth(store, transport: transport), transport: transport)
    await workspace.start()
    #expect(workspace.authStatus.signedIn)
    #expect(await workspace.signOut() == false)
    #expect(workspace.authStatus.signedIn && workspace.authStatus.accountID == "123")
    #expect(workspace.snapshot.accountID == "123" && workspace.snapshot.repositories.map(\.id) == ["owner/repo"])
    #expect(workspace.configuration == config)
    #expect(workspace.error.contains("Could not sign out") && workspace.error.contains("still signed in") && !workspace.busy)
    #expect(try store.load() == saved)
    let restarted = WorkspaceModel(storage: storage, authentication: signOutAuth(store, transport: transport), transport: transport)
    await restarted.start()
    #expect(restarted.authStatus.signedIn == workspace.authStatus.signedIn)
    store.failDelete(false)
    #expect(await workspace.signOut())
    #expect(!workspace.authStatus.signedIn && workspace.authStatus.error.isEmpty && workspace.error.isEmpty)
    #expect(workspace.snapshot.accountID.isEmpty && workspace.snapshot.repositories.isEmpty)
    #expect(workspace.configuration == WorkspaceConfiguration())
    #expect(try store.load() == nil)
    #expect(!((await signOutAuth(store, transport: transport).status()).signedIn))
    #expect(await transport.requests == 0)
}
@Test @MainActor func failedWorkspaceSignOutDoesNotResumeCanceledCollection() async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    let store = SignOutCredentialStore(try signOutCredentials()); store.failDelete(true)
    let transport = SignOutWorkspaceTransport()
    let workspace = WorkspaceModel(storage: storage, authentication: signOutAuth(store, transport: transport), transport: transport)
    let startup = Task { await workspace.start() }
    for _ in 0..<100 {
        if await transport.inventoryStarted { break }
        try await Task.sleep(for: .milliseconds(5))
    }
    #expect(await transport.inventoryStarted)
    #expect(workspace.busy)
    #expect(await workspace.signOut() == false)
    #expect(workspace.authStatus.signedIn && !workspace.busy)
    #expect(workspace.snapshot.accountID == "123")
    #expect(workspace.snapshot.coverage.phase == "partial")
    let coverage = workspace.snapshot.coverage, error = workspace.error
    await transport.release()
    await startup.value
    #expect(workspace.snapshot.coverage == coverage && workspace.error == error)
    #expect(workspace.authStatus.signedIn && !workspace.busy)
    #expect(await transport.requests == 2)
}

private final class BlockingSignOutCredentialStore: CredentialStore, @unchecked Sendable {
    private let condition = NSCondition()
    private var data: Data?
    private var released = false
    private var deletions = 0
    init(_ data: Data) { self.data = data }
    func load() throws -> Data? { condition.withLock { data } }
    func save(_ data: Data) throws { condition.withLock { self.data = data } }
    func delete() throws {
        condition.lock(); defer { condition.unlock() }
        deletions += 1
        guard deletions == 1 else { throw AuthFailure.storage }
        while !released { condition.wait() }
        data = nil
    }
    var deletionCount: Int { condition.withLock { deletions } }
    func release() { condition.lock(); released = true; condition.broadcast(); condition.unlock() }
}
@Test @MainActor func overlappingSignOutRequestsPerformOneDeletionAndClearWorkspace() async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    var config = try storage.loadConfiguration("123"); config.cachedOnly = true
    try storage.saveConfiguration(config, account: "123")
    let store = BlockingSignOutCredentialStore(try signOutCredentials())
    defer { store.release() }
    let transport = SignOutWorkspaceTransport()
    let auth = AuthManager(configuration: signOutConfiguration, store: store, transport: transport, now: { signOutNow })
    let workspace = WorkspaceModel(storage: storage, authentication: auth, transport: transport)
    await workspace.start()
    let first = Task { await workspace.signOut() }
    for _ in 0..<100 {
        if store.deletionCount > 0 { break }
        try await Task.sleep(for: .milliseconds(5))
    }
    #expect(store.deletionCount == 1)
    #expect(workspace.signingOut && workspace.busy)
    var secondResult: Bool?
    let second = Task { let result = await workspace.signOut(); secondResult = result; return result }
    for _ in 0..<100 {
        if secondResult != nil { break }
        try await Task.sleep(for: .milliseconds(5))
    }
    #expect(secondResult == false)
    store.release()
    #expect(await first.value)
    #expect(await second.value == false)
    #expect(store.deletionCount == 1)
    #expect(!workspace.signingOut && !workspace.busy && !workspace.authStatus.signedIn)
    #expect(workspace.snapshot.accountID.isEmpty && workspace.configuration == WorkspaceConfiguration())
    #expect(try store.load() == nil)
    #expect(await transport.requests == 0)
}
