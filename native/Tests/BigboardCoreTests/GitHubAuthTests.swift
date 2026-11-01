import Foundation
import Testing
@testable import Bigboard

private final class MemoryCredentials: CredentialStore, @unchecked Sendable {
    private let lock = NSLock()
    private var value: Data?
    private var failWrites = false
    private var failDeletion = false
    init(_ value: Data? = nil) { self.value = value }
    func load() throws -> Data? { lock.withLock { value } }
    func save(_ data: Data) throws { try lock.withLock { if failWrites { throw AuthFailure.storage }; value = data } }
    func delete() throws { try lock.withLock { if failDeletion { throw AuthFailure.storage }; value = nil } }
    func failingWrites(_ flag: Bool) { lock.withLock { failWrites = flag } }
    func failingDelete(_ flag: Bool) { lock.withLock { failDeletion = flag } }
}
private actor ScriptedGitHub: GitHubTransport {
    var replies: [GitHubHTTPResponse]
    var requests: [URLRequest] = []
    var delay: Duration
    init(_ replies: [GitHubHTTPResponse], delay: Duration = .zero) { self.replies = replies; self.delay = delay }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        requests.append(request)
        guard !replies.isEmpty else { throw GitHubFailure.unavailable }
        let reply = replies.removeFirst()
        if delay > .zero { try await Task.sleep(for: delay) }
        if reply.data.count > maximumBytes { throw GitHubFailure.outputLimit }
        return reply
    }
    var count: Int { requests.count }
    var first: URLRequest? { requests.first }
}
private let testNow = Date(timeIntervalSince1970: 1_800_000_000)
private let testConfiguration = AuthConfiguration(clientID: "client", clientSecret: "secret", appSlug: "bigboard-desktop")
private func reply(_ json: String, status: Int = 200, headers: [String: String] = [:]) -> GitHubHTTPResponse { GitHubHTTPResponse(status: status, headers: headers, data: Data(json.utf8)) }
private func storedCredentials(expired: Bool = false) throws -> Data {
    let value = AuthCredentials(accessToken: "old-access", refreshToken: "old-refresh", expiresAt: testNow.addingTimeInterval(expired ? -10 : 3600), refreshExpiresAt: testNow.addingTimeInterval(86400), login: "rich", accountID: "123", clientID: "client")
    let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
    return try encoder.encode(value)
}
private let tokenResponse = #"{"access_token":"new-access","refresh_token":"new-refresh","token_type":"bearer","expires_in":28800,"refresh_token_expires_in":15811200}"#
private func callback(for url: URL) throws -> URL {
    let state = try #require(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first(where: { $0.name == "state" })?.value)
    return try #require(URL(string: "com.richhaase.bigboard://oauth/github?state=\(state)&code=authorization-code"))
}

@Test func authUsesPKCEAndSingleUseCallback() async throws {
    let store = MemoryCredentials()
    let network = ScriptedGitHub([reply(tokenResponse), reply(#"{"login":"rich","id":123}"#)])
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: network, now: { testNow })
    let url = try await auth.start()
    let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
    #expect(query.first(where: { $0.name == "code_challenge_method" })?.value == "S256")
    #expect(query.first(where: { $0.name == "code_challenge" })?.value?.count == 43)
    #expect(!url.absoluteString.contains("secret"))
    let redirect = try callback(for: url)
    try await auth.complete(redirect)
    let state = await auth.status()
    #expect(state.signedIn && state.accountID == "123")
    #expect(try await auth.token() == "new-access")
    do { try await auth.complete(redirect); Issue.record("A callback was replayed") } catch { #expect(error is AuthFailure) }
    #expect(await network.count == 2)
}
@Test func authRejectsWrongCallbackWithoutConsumingAttempt() async throws {
    let network = ScriptedGitHub([reply(tokenResponse), reply(#"{"login":"rich","id":123}"#)])
    let auth = AuthManager(configuration: testConfiguration, store: MemoryCredentials(), transport: network, now: { testNow })
    let url = try await auth.start()
    do { try await auth.complete(URL(string: "com.richhaase.bigboard://oauth/github?state=wrong&code=abc")!); Issue.record("Accepted wrong state") } catch {}
    #expect(await network.count == 0)
    try await auth.complete(callback(for: url))
    #expect(await auth.status().signedIn)
}
@Test func tokenRotationIsSharedAndPersisted() async throws {
    let store = MemoryCredentials(try storedCredentials(expired: true))
    let network = ScriptedGitHub([reply(tokenResponse)], delay: .milliseconds(25))
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: network, now: { testNow })
    async let first = auth.token()
    async let second = auth.token()
    #expect(try await first == "new-access")
    #expect(try await second == "new-access")
    #expect(await network.count == 1)
    let saved = String(data: try #require(try store.load()), encoding: .utf8) ?? ""
    #expect(saved.contains("new-refresh"))
    #expect(!saved.contains("old-refresh"))
}
@Test func rotationStorageFailureRetainsReplacementForRetry() async throws {
    let store = MemoryCredentials(try storedCredentials(expired: true)); store.failingWrites(true)
    let network = ScriptedGitHub([reply(tokenResponse)])
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: network, now: { testNow })
    do { _ = try await auth.token(); Issue.record("Returned unsaved token") } catch { #expect(error is AuthFailure) }
    store.failingWrites(false)
    #expect(try await auth.token() == "new-access")
    #expect(await network.count == 1)
}
@Test func signOutFailurePreservesCredentialForRetry() async throws {
    let store = MemoryCredentials(try storedCredentials()); store.failingDelete(true)
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: ScriptedGitHub([]), now: { testNow })
    #expect(await auth.status().signedIn)
    do { try await auth.signOut(); Issue.record("Expected storage error") } catch {}
    #expect(await auth.status().signedIn)
    #expect(!(await auth.status().error).isEmpty)
    #expect(try await auth.token() == "old-access")
    store.failingDelete(false)
    try await auth.signOut()
    #expect(try store.load() == nil)
    #expect(await auth.status().signedIn == false)
    do { _ = try await auth.token(); Issue.record("Credential remained usable") } catch {}
}
@Test func canceledSignInCannotPersistLateCredentials() async throws {
    let store = MemoryCredentials()
    let network = ScriptedGitHub([reply(tokenResponse), reply(#"{"login":"rich","id":123}"#)], delay: .milliseconds(100))
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: network, now: { testNow })
    let redirect = try callback(for: await auth.start())
    let task = Task { try await auth.complete(redirect) }
    try await Task.sleep(for: .milliseconds(20)); await auth.cancel()
    do { try await task.value; Issue.record("Canceled exchange completed") } catch {}
    #expect(try store.load() == nil)
    #expect(await auth.status().signedIn == false)
}
@Test func storedCredentialIsBoundToPublisherClient() async throws {
    let auth = AuthManager(configuration: AuthConfiguration(clientID: "other-client", clientSecret: "secret", appSlug: "bigboard"), store: MemoryCredentials(try storedCredentials()), transport: ScriptedGitHub([]), now: { testNow })
    #expect(await auth.status().signedIn == false)
}
@Test func discoveryUsesInstallationAccessAndRejectsTruncatedPages() async throws {
    let network = ScriptedGitHub([
        reply(#"{"total_count":1,"installations":[{"id":1,"account":{"login":"Owner","type":"User"}}]}"#),
        reply(#"{"total_count":1,"repositories":[{"full_name":"Owner/Repo","default_branch":"trunk","archived":false,"pushed_at":"2026-10-31T12:00:00Z"}]}"#)
    ])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let repositories = try await service.discover()
    #expect(repositories.map(\.id) == ["owner/repo"])
    #expect(repositories.first?.branch == "trunk")
    let broken = GitHubService(token: { "token" }, transport: ScriptedGitHub([reply(#"{"total_count":2,"installations":[]}"#)]), now: { testNow })
    do { _ = try await broken.discover(); Issue.record("Accepted incomplete discovery") } catch { #expect(error as? GitHubFailure == .incomplete) }
}
@Test func discoveryScopesAuthoritativeDenialToInstallation() async throws {
    let network = ScriptedGitHub([reply(#"{"total_count":1,"installations":[{"id":1,"account":{"login":"Owner","type":"Organization"}}]}"#), reply("{}", status: 403)])
    do { _ = try await GitHubService(token: { "token" }, transport: network, now: { testNow }).discover(); Issue.record("Ignored denial") }
    catch let failure as GitHubFailure { #expect(failure.installationOwner == "owner"); #expect(failure.isAccessDenied) }
}
private let onePR = #"{"data":{"repository":{"nameWithOwner":"owner/repo","defaultBranchRef":{"name":"main","target":{"oid":"new-head"}},"pullRequests":{"totalCount":1,"pageInfo":{"hasNextPage":false},"nodes":[{"number":1,"title":"Fix","author":{"login":"rich"},"isDraft":false,"reviewDecision":"APPROVED","mergeable":"MERGEABLE","updatedAt":"2026-10-31T12:00:00Z","headRefOid":"head","baseRefOid":"base","files":{"totalCount":1,"pageInfo":{"hasNextPage":false},"nodes":[{"path":"src/a.swift"}]},"reviewRequests":{"totalCount":1,"pageInfo":{"hasNextPage":false},"nodes":[{"requestedReviewer":{"login":"reviewer"}}]},"latestReviews":{"totalCount":1,"pageInfo":{"hasNextPage":false},"nodes":[{"author":{"login":"reviewer"},"state":"APPROVED","submittedAt":"2026-10-30T12:00:00Z"}]},"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}]}}}}"#
@Test func pullRequestCollectionPreservesScopedCoverageAndScanRevision() async throws {
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([reply(onePR)]), now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo", revision: "scanned-head")], budget: 128, previous: [])
    let pr = try #require(result.pullRequests.first)
    #expect(pr.checks == "SUCCESS" && pr.review == "APPROVED")
    #expect(pr.inventoryComplete && pr.filesComplete && pr.reviewersComplete && pr.reviewsComplete)
    #expect(pr.files == ["src/a.swift"])
    #expect(result.repositories.first?.revision == "scanned-head")
    #expect(result.coverage.inventoryRequests == 1)
}
@Test func transientFailureRetainsPRsButAuthoritativeDenialDropsThem() async throws {
    let old = PullRequest(repo: "owner/repo", number: 1, title: "Old", author: "author", url: "https://github.com/owner/repo/pull/1", updatedAt: testNow)
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([reply("{}", status: 503), reply("{}", status: 403)]), now: { testNow })
    let transient = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [old])
    #expect(transient.pullRequests.count == 1)
    #expect(transient.repositories.first?.prInventoryComplete == false)
    let denial = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [old])
    #expect(denial.pullRequests.isEmpty)
    #expect(denial.deniedRepositories == ["owner/repo"])
}
@Test func rateLimitPausesFurtherRequests() async throws {
    let network = ScriptedGitHub([reply("{}", status: 429, headers: ["retry-after": "60"])])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    _ = await service.fetch([Repository(id: "owner/one"), Repository(id: "owner/two")], budget: 128, previous: [])
    _ = await service.fetch([Repository(id: "owner/one")], budget: 128, previous: [])
    #expect(await network.count == 1)
}
@Test func repositoryIdentifiersRejectPathEscape() {
    #expect(canonicalGitHubRepository("Owner/Repo") == "owner/repo")
    #expect(canonicalGitHubRepository("owner/..") == nil)
    #expect(canonicalGitHubRepository("owner/repo/path") == nil)
    #expect(canonicalGitHubRepository("owner/repo\n") == nil)
}
private func alteredPR(_ patch: (inout [String: Any]) -> Void, errors: [[String: Any]] = []) throws -> GitHubHTTPResponse {
    var document = try #require(JSONSerialization.jsonObject(with: Data(onePR.utf8)) as? [String: Any])
    var data = try #require(document["data"] as? [String: Any])
    var repository = try #require(data["repository"] as? [String: Any])
    var connection = try #require(repository["pullRequests"] as? [String: Any])
    var node = try #require((connection["nodes"] as? [[String: Any]])?.first)
    patch(&node)
    connection["nodes"] = [node]; repository["pullRequests"] = connection; data["repository"] = repository; document["data"] = data
    if !errors.isEmpty { document["errors"] = errors }
    return GitHubHTTPResponse(status: 200, headers: [:], data: try JSONSerialization.data(withJSONObject: document))
}
@Test func supportingFieldDenialDoesNotRevokeRepositoryOrInventory() async throws {
    let partial = try alteredPR({ $0["reviewRequests"] = NSNull() }, errors: [["type": "FORBIDDEN", "path": ["repository", "pullRequests", "nodes", 0, "reviewRequests"]]])
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([partial]), now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    #expect(result.deniedRepositories.isEmpty)
    #expect(result.repositories.first?.prInventoryComplete == true)
    #expect(result.repositories.first?.prReviewersComplete == false)
    #expect(result.pullRequests.count == 1)
    #expect(result.coverage.phase == "partial")
}
@Test func nullCheckRollupStateIsUnknownWithoutDroppingPR() async throws {
    let partial = try alteredPR { $0["commits"] = ["nodes": [["commit": ["statusCheckRollup": ["state": NSNull()]]]]] }
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([partial]), now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    #expect(result.pullRequests.first?.checks == "UNKNOWN")
    #expect(result.repositories.first?.prInventoryComplete == true)
}
@Test func missingInventoryNodesIsNotCompleteZero() async throws {
    let malformed = reply(#"{"data":{"repository":{"nameWithOwner":"owner/repo","pullRequests":{"totalCount":0,"pageInfo":{"hasNextPage":false}}}}}"#)
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([malformed]), now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    #expect(result.repositories.first?.prInventoryComplete == false)
    #expect(result.coverage.phase == "partial")
}
@Test func fileLimitRetainsThousandPathsAndIncompleteCoverage() async throws {
    var responses = [try alteredPR { $0["files"] = ["totalCount": 1001, "pageInfo": ["hasNextPage": true, "endCursor": "initial"], "nodes": [["path": "file0"]]] }]
    for page in 0..<10 {
        let raw: [String: Any] = ["number": 1, "headRefOid": "head", "baseRefOid": "base", "updatedAt": "2026-10-31T12:00:00Z", "files": ["totalCount": 1001, "pageInfo": ["hasNextPage": true, "endCursor": "page\(page)"], "nodes": (0..<100).map { ["path": "file\(page * 100 + $0)"] }]]
        responses.append(GitHubHTTPResponse(status: 200, headers: [:], data: try JSONSerialization.data(withJSONObject: ["data": ["repository": ["nameWithOwner": "owner/repo", "pullRequest": raw]]])))
    }
    let network = ScriptedGitHub(responses)
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    #expect(result.pullRequests.first?.files.count == 1000)
    #expect(result.pullRequests.first?.filesComplete == false)
    #expect(result.repositories.first?.prInventoryComplete == true)
    #expect(result.coverage.fileRequests == 10)
    #expect(result.coverage.phase == "partial")
    #expect(await network.count == 11)
}
@Test func canceledRefreshWaiterStillPersistsRotatedCredential() async throws {
    let store = MemoryCredentials(try storedCredentials(expired: true))
    let network = ScriptedGitHub([reply(tokenResponse)], delay: .milliseconds(30))
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: network, now: { testNow })
    let pending = Task { try await auth.token() }
    try await Task.sleep(for: .milliseconds(10)); pending.cancel()
    do { _ = try await pending.value; Issue.record("Canceled waiter returned a token") } catch {}
    #expect(try await auth.token() == "new-access")
    #expect(await network.count == 1)
}

@Test func authenticationDenialBlocksRemainingRepositoryRequests() async throws {
    let network = ScriptedGitHub([reply("{}", status: 401)])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let result = await service.fetch([Repository(id: "owner/one"), Repository(id: "owner/two")], budget: 128, previous: [])
    #expect(result.authenticationRequired)
    #expect(result.deniedRepositories == ["owner/one", "owner/two"])
    #expect(await network.count == 1)
}
@Test func invalidatedSessionStopsTokenUseWithoutDeletingSavedCredential() async throws {
    let store = MemoryCredentials(try storedCredentials())
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: ScriptedGitHub([]), now: { testNow })
    #expect(await auth.status().signedIn)
    await auth.invalidateSession()
    #expect(await auth.status().signedIn == false)
    #expect(try store.load() != nil)
    do { _ = try await auth.token(); Issue.record("Invalidated session remained usable") } catch { #expect(error as? GitHubFailure == .auth) }
}

@Test func liveViewerRefreshPersistsRenamedLoginAlongsideRotatedCredentials() async throws {
    let store = MemoryCredentials(try storedCredentials(expired: true))
    let network = ScriptedGitHub([reply(tokenResponse), reply(#"{"login":"renamed","id":123}"#)])
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: network, now: { testNow })
    let service = GitHubService(token: { try await auth.token() }, transport: network, now: { testNow })
    let state = try await auth.refreshViewer(using: service)
    #expect(state.signedIn && state.login == "renamed" && state.accountID == "123")
    let reloaded = AuthManager(configuration: testConfiguration, store: store, transport: ScriptedGitHub([]), now: { testNow })
    #expect(await reloaded.status().login == "renamed")
    #expect(try await reloaded.token() == "new-access")
    let saved = String(data: try #require(try store.load()), encoding: .utf8) ?? ""
    #expect(saved.contains("new-refresh"))
    #expect(await network.count == 2)
}
@Test func liveViewerRefreshRejectsChangedAccountWithoutOverwritingCredential() async throws {
    let original = try storedCredentials()
    let store = MemoryCredentials(original)
    let network = ScriptedGitHub([reply(#"{"login":"someone-else","id":456}"#)])
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: network, now: { testNow })
    let service = GitHubService(token: { try await auth.token() }, transport: network, now: { testNow })
    do { _ = try await auth.refreshViewer(using: service); Issue.record("Accepted a different GitHub account") }
    catch { #expect(error as? GitHubFailure == .auth) }
    #expect(try store.load() == original)
    #expect(await auth.status().accountID == "123")
}
@Test func failedViewerPersistenceDoesNotRollBackTokenRotation() async throws {
    let store = MemoryCredentials(try storedCredentials(expired: true))
    let network = ScriptedGitHub([reply(tokenResponse), reply(#"{"login":"renamed","id":123}"#)])
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: network, now: { testNow })
    #expect(try await auth.token() == "new-access")
    store.failingWrites(true)
    let service = GitHubService(token: { try await auth.token() }, transport: network, now: { testNow })
    do { _ = try await auth.refreshViewer(using: service); Issue.record("Ignored a Keychain write failure") }
    catch { #expect(error as? AuthFailure == .storage) }
    store.failingWrites(false)
    #expect(try await auth.token() == "new-access")
    #expect(await auth.status().login == "rich")
    #expect(await network.count == 2)
}
private actor DelayedViewerGitHub: GitHubTransport {
    private var waiter: CheckedContinuation<GitHubHTTPResponse, Never>?
    private(set) var started = false
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        started = true
        return await withCheckedContinuation { waiter = $0 }
    }
    func release() {
        waiter?.resume(returning: reply(#"{"login":"renamed","id":123}"#))
        waiter = nil
    }
}
@Test(arguments: [false, true]) func canceledViewerRefreshCannotPersistLateAccountMetadata(signOut: Bool) async throws {
    let original = try storedCredentials()
    let store = MemoryCredentials(original)
    let network = DelayedViewerGitHub()
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: network, now: { testNow })
    let service = GitHubService(token: { try await auth.token() }, transport: network, now: { testNow })
    let pending = Task { try await auth.refreshViewer(using: service) }
    for _ in 0..<100 {
        if await network.started { break }
        try await Task.sleep(for: .milliseconds(5))
    }
    #expect(await network.started)
    if signOut { try await auth.signOut() } else { pending.cancel() }
    await network.release()
    do { _ = try await pending.value; Issue.record("Applied a canceled viewer response") }
    catch { #expect(error is CancellationError) }
    #expect(try store.load() == (signOut ? nil : original))
}
