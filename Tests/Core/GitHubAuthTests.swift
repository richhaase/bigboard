import Foundation
import CryptoKit
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
private final class GitHubTestClock: @unchecked Sendable {
    private let lock = NSLock()
    private var date = testNow
    func now() -> Date { lock.withLock { date } }
    func advance(by seconds: TimeInterval) { lock.withLock { date.addTimeInterval(seconds) } }
}
private let testConfiguration = AuthConfiguration(clientID: "client", clientSecret: "secret", appSlug: "bigboard-desktop")
private func reply(_ json: String, status: Int = 200, headers: [String: String] = [:]) -> GitHubHTTPResponse { GitHubHTTPResponse(status: status, headers: headers, data: Data(json.utf8)) }
private func storedCredentials(expired: Bool = false) throws -> Data {
    let value = AuthCredentials(accessToken: "old-access", refreshToken: "old-refresh", expiresAt: testNow.addingTimeInterval(expired ? -10 : 3600), refreshExpiresAt: testNow.addingTimeInterval(86400), login: "rich", accountID: "123", clientID: "client")
    let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
    return try encoder.encode(value)
}
private let tokenResponse = #"{"access_token":"new-access","refresh_token":"new-refresh","token_type":"bearer","expires_in":28800,"refresh_token_expires_in":15811200}"#
private func callback(for url: URL) throws -> URL {
    let query = try #require(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems)
    let state = try #require(query.first(where: { $0.name == "state" })?.value)
    let redirect = try #require(query.first(where: { $0.name == "redirect_uri" })?.value)
    var callback = try #require(URLComponents(string: redirect))
    callback.queryItems = [URLQueryItem(name: "state", value: state), URLQueryItem(name: "code", value: "authorization-code")]
    return try #require(callback.url)
}

@Test(arguments: ApplicationIdentity.allCases) func authUsesPKCEAndSingleUseCallback(identity: ApplicationIdentity) async throws {
    let store = MemoryCredentials()
    let network = ScriptedGitHub([reply(tokenResponse), reply(#"{"login":"rich","id":123}"#)])
    var configuration = testConfiguration
    configuration.identity = identity
    let auth = AuthManager(configuration: configuration, store: store, transport: network, now: { testNow })
    let url = try await auth.start()
    let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
    #expect(query.first(where: { $0.name == "code_challenge_method" })?.value == "S256")
    #expect(query.first(where: { $0.name == "code_challenge" })?.value?.count == 43)
    #expect(query.first(where: { $0.name == "redirect_uri" })?.value == identity.callbackURI)
    #expect(!url.absoluteString.contains("secret"))
    let redirect = try callback(for: url)
    try await auth.complete(redirect)
    let exchange = try #require(await network.first)
    let body = try #require(exchange.httpBody.flatMap { String(data: $0, encoding: .utf8) })
    let fields = try #require(URLComponents(string: "?" + body)?.queryItems)
    #expect(fields.first(where: { $0.name == "redirect_uri" })?.value == identity.callbackURI)
    let verifier = try #require(fields.first(where: { $0.name == "code_verifier" })?.value)
    #expect(verifier.count == 43)
    let challenge = Data(SHA256.hash(data: Data(verifier.utf8))).base64EncodedString()
        .replacingOccurrences(of: "+", with: "-")
        .replacingOccurrences(of: "/", with: "_")
        .replacingOccurrences(of: "=", with: "")
    #expect(query.first(where: { $0.name == "code_challenge" })?.value == challenge)
    let state = await auth.status()
    #expect(state.signedIn && state.accountID == "123")
    #expect(try await auth.token() == "new-access")
    do { try await auth.complete(redirect); Issue.record("A callback was replayed") } catch { #expect(error is AuthFailure) }
    #expect(await network.count == 2)
}
@Test(arguments: ApplicationIdentity.allCases, ["other-build", "host", "path", "encoded-path", "port", "user", "password", "fragment", "duplicate-state", "duplicate-code"])
func authRejectsOtherBuildAndMalformedCallback(identity: ApplicationIdentity, alteration: String) async throws {
    var configuration = testConfiguration
    configuration.identity = identity
    let network = ScriptedGitHub([reply(tokenResponse), reply(#"{"login":"rich","id":123}"#)])
    let auth = AuthManager(configuration: configuration, store: MemoryCredentials(), transport: network, now: { testNow })
    let redirect = try callback(for: await auth.start())
    var parts = try #require(URLComponents(url: redirect, resolvingAgainstBaseURL: false))
    switch alteration {
    case "other-build": parts.scheme = identity == .production ? ApplicationIdentity.development.rawValue : ApplicationIdentity.production.rawValue
    case "host": parts.host = "other"
    case "path": parts.path = "/other"
    case "encoded-path": parts.percentEncodedPath = "/%67ithub"
    case "port": parts.port = 443
    case "user": parts.user = "user"
    case "password": parts.password = "password"
    case "fragment": parts.fragment = "fragment"
    case "duplicate-state": parts.queryItems?.append(URLQueryItem(name: "state", value: "other"))
    default: parts.queryItems?.append(URLQueryItem(name: "code", value: "other"))
    }
    do { try await auth.complete(try #require(parts.url)); Issue.record("Accepted a mismatched or malformed callback") }
    catch { #expect(error as? AuthFailure == .callback) }
    #expect(await network.count == 0)
    if alteration != "duplicate-code" {
        try await auth.complete(redirect)
        #expect(await auth.status().signedIn)
    }
}
@Test func developmentSignInAndSignOutLeaveProductionCredentialsIntact() async throws {
    let original = try storedCredentials()
    let productionStore = MemoryCredentials(original)
    let developmentStore = MemoryCredentials()
    let production = AuthManager(configuration: testConfiguration, store: productionStore, transport: ScriptedGitHub([]), now: { testNow })
    var configuration = testConfiguration
    configuration.identity = .development
    let development = AuthManager(configuration: configuration, store: developmentStore, transport: ScriptedGitHub([reply(tokenResponse), reply(#"{"login":"rich","id":123}"#)]), now: { testNow })
    #expect(await development.status().signedIn == false)
    try await development.complete(callback(for: await development.start()))
    #expect(await development.status().signedIn)
    #expect(try productionStore.load() == original)
    try await development.signOut()
    #expect(try developmentStore.load() == nil)
    #expect(try productionStore.load() == original)
    #expect(await production.status().signedIn)
    #expect(try await production.token() == "old-access")
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
@Test(arguments: [599.0, 600.0])
func signInCallbackExpiresAtTenMinutes(elapsed: TimeInterval) async throws {
    let clock = GitHubTestClock()
    let store = MemoryCredentials()
    let network = ScriptedGitHub([reply(tokenResponse), reply(#"{"login":"rich","id":123}"#)])
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: network, now: clock.now)
    let redirect = try callback(for: await auth.start())
    clock.advance(by: elapsed)
    if elapsed < 600 {
        try await auth.complete(redirect)
        #expect(await auth.status().signedIn)
        #expect(await network.count == 2)
    } else {
        await #expect(throws: AuthFailure.callback) { try await auth.complete(redirect) }
        #expect(await network.count == 0)
        #expect(try store.load() == nil)
        let status = await auth.status()
        #expect(!status.pending && !status.signedIn)
    }
}

@Test func expiredRefreshCredentialCannotStartAnExchange() async throws {
    let clock = GitHubTestClock()
    let saved = try storedCredentials()
    let store = MemoryCredentials(saved)
    let network = ScriptedGitHub([])
    let auth = AuthManager(configuration: testConfiguration, store: store, transport: network, now: clock.now)
    #expect(await auth.status().signedIn)
    clock.advance(by: 86400)
    #expect(!(await auth.status()).signedIn)
    await #expect(throws: GitHubFailure.auth) { try await auth.token() }
    #expect(await network.count == 0)
    #expect(try store.load() == saved)
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
@Test func rateLimitPausesRequestsUntilCooldownEnds() async throws {
    let clock = GitHubTestClock()
    let network = ScriptedGitHub([reply("{}", status: 429, headers: ["retry-after": "60"]), reply(onePR)])
    let service = GitHubService(token: { "token" }, transport: network, now: clock.now)
    _ = await service.fetch([Repository(id: "owner/one"), Repository(id: "owner/two")], budget: 128, previous: [])
    clock.advance(by: 59)
    _ = await service.fetch([Repository(id: "owner/one")], budget: 128, previous: [])
    #expect(await network.count == 1)
    clock.advance(by: 1)
    let recovered = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    #expect(await network.count == 2)
    #expect(recovered.coverage.phase == "ready")
    #expect(recovered.pullRequests.map(\.number) == [1])
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

private func inventoryPage(_ numbers: [Int], total: Int, next: String? = nil) throws -> GitHubHTTPResponse {
    var document = try #require(JSONSerialization.jsonObject(with: Data(onePR.utf8)) as? [String: Any])
    var data = try #require(document["data"] as? [String: Any])
    var repository = try #require(data["repository"] as? [String: Any])
    var connection = try #require(repository["pullRequests"] as? [String: Any])
    let template = try #require((connection["nodes"] as? [[String: Any]])?.first)
    connection["nodes"] = numbers.map { number in
        var node = template
        node["number"] = number
        node["title"] = "Current PR \(number)"
        return node
    }
    connection["totalCount"] = total
    connection["pageInfo"] = ["hasNextPage": next != nil, "endCursor": next as Any? ?? NSNull()]
    repository["pullRequests"] = connection
    data["repository"] = repository
    document["data"] = data
    return GitHubHTTPResponse(status: 200, headers: [:], data: try JSONSerialization.data(withJSONObject: document))
}

private func graphCursor(in request: URLRequest) throws -> String? {
    let body = try #require(request.httpBody)
    let document = try #require(JSONSerialization.jsonObject(with: body) as? [String: Any])
    let variables = try #require(document["variables"] as? [String: Any])
    return variables["cursor"] as? String
}

@Test func pullRequestInventoryFollowsCursorAndCombinesPages() async throws {
    let network = ScriptedGitHub([
        try inventoryPage([1], total: 2, next: "second-page"),
        try inventoryPage([2], total: 2)
    ])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 2, previous: [])
    #expect(result.pullRequests.map(\.number) == [1, 2])
    #expect(result.pullRequests.allSatisfy { $0.inventoryComplete })
    #expect(result.repositories.first?.prInventoryComplete == true)
    #expect(result.coverage.phase == "ready")
    #expect(result.coverage.inventoryRequests == 2)
    let requests = await network.requests
    try #require(requests.count == 2)
    #expect(try graphCursor(in: requests[0]) == nil)
    #expect(try graphCursor(in: requests[1]) == "second-page")
}

@Test(arguments: [false, true])
func interruptedInventoryRetainsOldEvidenceUntilCompleteRefresh(exhaustBudget: Bool) async throws {
    var replies = [try inventoryPage([1], total: 2, next: "second-page")]
    if !exhaustBudget { replies.append(reply("{}", status: 503)) }
    replies.append(try inventoryPage([1, 2], total: 2))
    let network = ScriptedGitHub(replies)
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let previous = [1, 99].map {
        PullRequest(repo: "owner/repo", number: $0, title: "Previously saved", author: "author",
                    url: "https://github.com/owner/repo/pull/\($0)", updatedAt: testNow)
    }
    let partial = await service.fetch(
        [Repository(id: "owner/repo")], budget: exhaustBudget ? 1 : 10, previous: previous)
    #expect(partial.pullRequests.map(\.number) == [1, 99])
    #expect(partial.pullRequests.first?.title == "Current PR 1")
    #expect(partial.pullRequests.allSatisfy { !$0.inventoryComplete })
    #expect(partial.repositories.first?.prInventoryComplete == false)
    #expect(partial.repositories.first?.prStale == true)
    #expect(partial.coverage.phase == "partial")
    #expect(partial.deniedRepositories.isEmpty)
    #expect(await network.count == (exhaustBudget ? 1 : 2))

    let recovered = await service.fetch(partial.repositories, budget: 10, previous: partial.pullRequests)
    #expect(recovered.pullRequests.map(\.number) == [1, 2])
    #expect(recovered.pullRequests.allSatisfy { $0.inventoryComplete })
    #expect(recovered.repositories.first?.prInventoryComplete == true)
    #expect(recovered.repositories.first?.prStale == false)
    #expect(recovered.coverage.phase == "ready")
    #expect(await network.count == (exhaustBudget ? 2 : 3))
}

@Test(arguments: ["headRefOid", "baseRefOid"])
func changingPullRequestRevisionCannotCombineFilePages(changedField: String) async throws {
    let inventory = try alteredPR {
        $0["files"] = ["totalCount": 2, "nodes": [], "pageInfo": ["hasNextPage": true]]
    }
    func filePage(path: String, next: String?, changed: Bool) throws -> GitHubHTTPResponse {
        var node: [String: Any] = [
            "number": 1, "headRefOid": "head", "baseRefOid": "base", "updatedAt": "2026-10-31T12:00:00Z",
            "files": ["totalCount": 2, "nodes": [["path": path]],
                      "pageInfo": ["hasNextPage": next != nil, "endCursor": next as Any? ?? NSNull()]]
        ]
        if changed { node[changedField] = "changed-revision" }
        let data = try JSONSerialization.data(withJSONObject: [
            "data": ["repository": ["nameWithOwner": "owner/repo", "pullRequest": node]]
        ])
        return GitHubHTTPResponse(status: 200, headers: [:], data: data)
    }
    let network = ScriptedGitHub([
        inventory,
        try filePage(path: "src/original.swift", next: "more-files", changed: false),
        try filePage(path: "src/changed.swift", next: nil, changed: true)
    ])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 10, previous: [])
    let pr = try #require(result.pullRequests.first)
    #expect(pr.files == ["src/original.swift"])
    #expect(!pr.filesComplete && pr.inventoryComplete)
    #expect(result.repositories.first?.prFilesComplete == false)
    #expect(result.repositories.first?.prInventoryComplete == true)
    #expect(result.coverage.phase == "partial")
    #expect(result.deniedRepositories.isEmpty)
    #expect(result.coverage.fileRequests == 2)
    let requests = await network.requests
    try #require(requests.count == 3)
    #expect(try graphCursor(in: requests[2]) == "more-files")
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
