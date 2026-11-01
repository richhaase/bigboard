import Foundation
import Testing
@testable import Bigboard

@Suite @MainActor struct BookmarkRecoveryTests {
    @Test(arguments: BookmarkInterruption.allCases)
    func partialRefreshPreservesRecoveryAfterRestart(_ interruption: BookmarkInterruption) async throws {
        let fixture = try BookmarkFixture(missing: interruption == .history ? [BookmarkFixture.second] : [])
        defer { fixture.remove() }
        if interruption == .history { await fixture.historyRefresh.fail(BookmarkFixture.second) }
        else { await fixture.network.setMode(.incomplete, for: BookmarkFixture.second) }
        let workspace = fixture.workspace()
        await workspace.start()
        #expect(workspace.snapshot.coverage.phase == "partial")
        #expect(workspace.snapshot.repositories.contains { $0.id == BookmarkFixture.first && !$0.stale && $0.prInventoryComplete })
        if interruption == .history {
            #expect(workspace.snapshot.repositories.contains { $0.id == BookmarkFixture.second && $0.stale })
            #expect(!workspace.snapshot.commits.contains { $0.repo == BookmarkFixture.second })
        } else {
            #expect(workspace.snapshot.repositories.contains { $0.id == BookmarkFixture.second && !$0.prInventoryComplete && $0.prStale })
            #expect(!workspace.snapshot.pullRequests.contains { $0.repo == BookmarkFixture.second })
        }
        await workspace.shutdown()
        #expect(try fixture.storage.bookmarks("123")["all"] == BookmarkFixture.oldBookmark)
        if interruption == .history { try fixture.install(BookmarkFixture.second) }
        await fixture.historyRefresh.recover()
        await fixture.network.setMode(.complete, for: BookmarkFixture.second)
        let restarted = fixture.workspace()
        await restarted.start()
        #expect(restarted.snapshot.bookmark == BookmarkFixture.oldBookmark)
        #expect(restarted.snapshot.repositories.allSatisfy { !$0.stale && $0.prInventoryComplete && !$0.prStale })
        let board = BoardState(); board.sinceLastLook = true
        #expect(board.filteredCommits(restarted.snapshot).contains { $0.repo == BookmarkFixture.second })
        #expect(board.pullRequests(restarted.snapshot).contains { $0.repo == BookmarkFixture.second })
        let recoveredAt = BookmarkFixture.persistedDate(restarted.snapshot.generatedAt)
        await restarted.shutdown()
        #expect(try fixture.storage.bookmarks("123")["all"] == recoveredAt)
    }

    @Test func laterPartialRefreshKeepsEarlierEligibleBookmark() async throws {
        let fixture = try BookmarkFixture()
        defer { fixture.remove() }
        let workspace = fixture.workspace()
        await workspace.start()
        #expect(workspace.snapshot.coverage.phase == "ready")
        let eligibleAt = BookmarkFixture.persistedDate(workspace.snapshot.generatedAt)
        try await Task.sleep(for: .milliseconds(1100))
        await fixture.historyRefresh.fail(BookmarkFixture.second)
        await workspace.refresh()
        #expect(BookmarkFixture.persistedDate(workspace.snapshot.generatedAt) > eligibleAt)
        #expect(workspace.snapshot.repositories.contains { $0.id == BookmarkFixture.second && $0.stale })
        await workspace.shutdown()
        #expect(try fixture.storage.bookmarks("123")["all"] == eligibleAt)
        var config = try fixture.storage.loadConfiguration("123"); config.cachedOnly = true
        try fixture.storage.saveConfiguration(config, account: "123")
        let restarted = fixture.workspace()
        await restarted.start()
        #expect(restarted.snapshot.bookmark == eligibleAt)
        await restarted.shutdown()
    }

    @Test func boundedSupportingDetailsDoNotPreventEligibleInventory() async throws {
        var config = BookmarkFixture.configuration
        config.repositories = [BookmarkFixture.first]; config.requestBudget = 1
        let fixture = try BookmarkFixture(configuration: config)
        defer { fixture.remove() }
        await fixture.network.setMode(.supportingDetails, for: BookmarkFixture.first)
        let workspace = fixture.workspace()
        await workspace.start()
        let repo = try #require(workspace.snapshot.repositories.first)
        #expect(!repo.stale && repo.prInventoryComplete && !repo.prStale)
        #expect(!repo.prFilesComplete && !repo.prReviewersComplete)
        #expect(workspace.snapshot.coverage.phase == "partial")
        #expect(workspace.snapshot.coverage.inventoryRequests == 1)
        let eligibleAt = BookmarkFixture.persistedDate(workspace.snapshot.generatedAt)
        await workspace.shutdown()
        #expect(try fixture.storage.bookmarks("123")["all"] == eligibleAt)
    }

    @Test func groupsEmptyScopesAndOfflineRefreshKeepIndependentBookmarks() async throws {
        var config = BookmarkFixture.configuration
        config.groups = ["Focus": [BookmarkFixture.first], "Empty": []]
        let fixture = try BookmarkFixture(configuration: config)
        defer { fixture.remove() }
        try fixture.storage.saveBookmarks(["group:Focus": BookmarkFixture.oldBookmark, "group:Empty": BookmarkFixture.oldBookmark], account: "123")
        await fixture.historyRefresh.fail(BookmarkFixture.second)
        let workspace = fixture.workspace()
        await workspace.start()
        await workspace.selectGroup("Focus")
        #expect(workspace.snapshot.repositories.map(\.id) == [BookmarkFixture.first])
        #expect(workspace.snapshot.coverage.phase == "ready")
        let focusedAt = BookmarkFixture.persistedDate(workspace.snapshot.generatedAt)
        await workspace.selectGroup("Empty")
        #expect(workspace.snapshot.repositories.isEmpty && workspace.snapshot.commits.isEmpty)
        await workspace.shutdown()
        let expected = ["all": BookmarkFixture.oldBookmark, "group:Focus": focusedAt, "group:Empty": BookmarkFixture.oldBookmark]
        #expect(try fixture.storage.bookmarks("123") == expected)
        config = try fixture.storage.loadConfiguration("123"); config.cachedOnly = true
        try fixture.storage.saveConfiguration(config, account: "123")
        let requests = await fixture.network.requests
        let offline = fixture.workspace()
        await offline.start()
        #expect(offline.snapshot.cachedOnly && !offline.snapshot.commits.isEmpty)
        #expect(offline.snapshot.bookmark == BookmarkFixture.oldBookmark)
        await offline.selectGroup("Focus")
        #expect(offline.snapshot.bookmark == focusedAt)
        await offline.shutdown()
        #expect(await fixture.network.requests == requests)
        #expect(try fixture.storage.bookmarks("123") == expected)
    }

    @Test(arguments: BookmarkInterruption.allCases)
    func accessDenialCannotTurnPartialScopeIntoEligibleRefresh(_ interruption: BookmarkInterruption) async throws {
        let fixture = try BookmarkFixture()
        defer { fixture.remove() }
        if interruption == .history { await fixture.historyRefresh.fail(BookmarkFixture.second, failure: .permission) }
        else { await fixture.network.setMode(.denied, for: BookmarkFixture.second) }
        let workspace = fixture.workspace()
        await workspace.start()
        #expect(workspace.snapshot.repositories.map(\.id) == [BookmarkFixture.first])
        #expect(!workspace.snapshot.commits.contains { $0.repo == BookmarkFixture.second })
        #expect(!workspace.snapshot.pullRequests.contains { $0.repo == BookmarkFixture.second })
        #expect(try fixture.storage.loadGrant("123").map(\.id) == [BookmarkFixture.first])
        await workspace.shutdown()
        #expect(try fixture.storage.bookmarks("123")["all"] == BookmarkFixture.oldBookmark)
    }
}

enum BookmarkInterruption: CaseIterable, Sendable { case history, inventory }

@MainActor private struct BookmarkFixture {
    static let first = "owner/one"
    static let second = "owner/two"
    static let oldBookmark = Date(timeIntervalSince1970: 1_780_000_000)
    static var configuration: WorkspaceConfiguration {
        var value = WorkspaceConfiguration(); value.repositories = [first, second]; return value
    }
    let history: HistoryFixture
    let storage: WorkspaceStorage
    let network: BookmarkTransport
    let historyRefresh = BookmarkHistoryRefresh()
    let credentials: BookmarkCredentials
    init(missing: Set<String> = [], configuration: WorkspaceConfiguration = BookmarkFixture.configuration) throws {
        history = try HistoryFixture()
        storage = WorkspaceStorage(supportRoot: history.base.appendingPathComponent("support"), cacheRoot: history.base.appendingPathComponent("workspace-cache"))
        network = BookmarkTransport(repositories: [Self.first, Self.second])
        credentials = try BookmarkCredentials()
        try storage.saveConfiguration(configuration, account: "123")
        try storage.saveGrant([Repository(id: Self.first), Repository(id: Self.second)], account: "123")
        try storage.saveBookmarks(["all": Self.oldBookmark], account: "123")
        for id in [Self.first, Self.second] where !missing.contains(id) { try install(id) }
    }
    func install(_ id: String) throws {
        let destination = try storage.repositoryRoot("123").appendingPathComponent("github.com/" + id + ".git")
        try FileManager.default.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true)
        try FileManager.default.copyItem(at: history.bare, to: destination)
        try Data((id + "\n").utf8).write(to: destination.appendingPathComponent("bigboard-cache"))
        _ = try history.git(["--git-dir=" + destination.path, "config", "remote.origin.url", "https://github.com/\(id).git"])
    }
    func workspace() -> WorkspaceModel {
        let auth = AuthManager(configuration: AuthConfiguration(clientID: "fixture-client", clientSecret: "fixture-secret", appSlug: "fixture-app"), store: credentials, transport: network)
        let refresh = historyRefresh
        return WorkspaceModel(storage: storage, authentication: auth, transport: network, refreshHistory: { _, repo, _ in try await refresh.refresh(repo) })
    }
    func remove() { try? FileManager.default.removeItem(at: history.base) }
    static func persistedDate(_ value: Date) -> Date { Date(timeIntervalSince1970: floor(value.timeIntervalSince1970)) }
}

private actor BookmarkHistoryRefresh {
    private var failures: [String: GitHubFailure] = [:]
    func fail(_ id: String, failure: GitHubFailure = .unavailable) { failures[id] = failure }
    func recover() { failures = [:] }
    func refresh(_ repo: Repository) throws -> Repository {
        if let failure = failures[repo.id] { throw failure }
        var value = repo; value.stale = false; value.error = ""; return value
    }
}

private actor BookmarkTransport: GitHubTransport {
    enum Mode: Sendable { case complete, incomplete, supportingDetails, denied }
    private let repositories: [String]
    private var modes: [String: Mode] = [:]
    private(set) var requests = 0
    init(repositories: [String]) { self.repositories = repositories }
    func setMode(_ mode: Mode, for id: String) { modes[id] = mode }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        requests += 1
        let object: [String: Any]
        switch request.url?.path {
        case "/user": object = ["login": "fixture", "id": 123]
        case "/user/installations":
            object = ["total_count": 1, "installations": [["id": 1, "account": ["login": "owner", "type": "Organization"]]]]
        case "/user/installations/1/repositories":
            object = ["total_count": repositories.count, "repositories": repositories.map { ["full_name": $0, "default_branch": "main", "archived": false] as [String: Any] }]
        case "/graphql":
            guard let body = request.httpBody, let document = try JSONSerialization.jsonObject(with: body) as? [String: Any], let variables = document["variables"] as? [String: Any], let owner = variables["owner"] as? String, let name = variables["name"] as? String else { throw GitHubFailure.response }
            let id = owner + "/" + name
            let mode = modes[id] ?? .complete
            if mode == .denied { return GitHubHTTPResponse(status: 403, headers: [:], data: Data("{}".utf8)) }
            guard variables["number"] == nil else { throw GitHubFailure.unavailable }
            let empty: [String: Any] = ["totalCount": 0, "nodes": [], "pageInfo": ["hasNextPage": false]]
            var pr: [String: Any] = ["number": 1, "title": "Recovered work", "author": ["login": "fixture"], "updatedAt": "2026-10-02T12:00:00Z", "headRefOid": "head", "baseRefOid": "base", "files": empty, "reviewRequests": empty, "latestReviews": empty]
            if mode == .supportingDetails {
                pr["files"] = ["totalCount": 1001, "nodes": [["path": "first.swift"]], "pageInfo": ["hasNextPage": true, "endCursor": "files"]]
                pr["reviewRequests"] = ["totalCount": 51, "nodes": [["requestedReviewer": ["login": "reviewer"]]], "pageInfo": ["hasNextPage": true, "endCursor": "reviewers"]]
            }
            let nodes: [[String: Any]] = mode == .incomplete ? [] : [pr]
            object = ["data": ["repository": ["nameWithOwner": id, "pullRequests": ["totalCount": 1, "nodes": nodes, "pageInfo": ["hasNextPage": false]]]]]
        default: throw GitHubFailure.unavailable
        }
        return GitHubHTTPResponse(status: 200, headers: [:], data: try JSONSerialization.data(withJSONObject: object))
    }
}

private final class BookmarkCredentials: CredentialStore, @unchecked Sendable {
    private let lock = NSLock()
    private var data: Data?
    init() throws {
        let value = AuthCredentials(accessToken: "fixture-access", refreshToken: "fixture-refresh", expiresAt: Date().addingTimeInterval(3600), refreshExpiresAt: Date().addingTimeInterval(86400), login: "fixture", accountID: "123", clientID: "fixture-client")
        let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
        data = try encoder.encode(value)
    }
    func load() throws -> Data? { lock.withLock { data } }
    func save(_ data: Data) throws { lock.withLock { self.data = data } }
    func delete() throws { lock.withLock { data = nil } }
}
