import Foundation
import Testing
@testable import Bigboard

@Suite @MainActor struct RefreshRecoveryTests {
    @Test(arguments: RefreshRecoveryInterruption.allCases)
    func partialRefreshPreservesRecoveryAfterRestart(_ interruption: RefreshRecoveryInterruption) async throws {
        let fixture = try RefreshRecoveryFixture()
        defer { fixture.remove() }
        if interruption == .history { await fixture.historyRefresh.fail(RefreshRecoveryFixture.second) }
        else { await fixture.network.setMode(.incomplete, for: RefreshRecoveryFixture.second) }
        let workspace = fixture.workspace()
        await workspace.start()
        #expect(workspace.snapshot.coverage.phase == "partial")
        #expect(workspace.snapshot.repositories.contains { $0.id == RefreshRecoveryFixture.first && !$0.stale && $0.prInventoryComplete })
        if interruption == .history {
            #expect(workspace.snapshot.repositories.contains { $0.id == RefreshRecoveryFixture.second && $0.stale })
            #expect(workspace.snapshot.commits.contains { $0.repo == RefreshRecoveryFixture.second })
        } else {
            #expect(workspace.snapshot.repositories.contains { $0.id == RefreshRecoveryFixture.second && !$0.prInventoryComplete && $0.prStale })
            #expect(!workspace.snapshot.pullRequests.contains { $0.repo == RefreshRecoveryFixture.second })
        }
        await workspace.shutdown()
        await fixture.historyRefresh.recover()
        await fixture.network.setMode(.complete, for: RefreshRecoveryFixture.second)
        let restarted = fixture.workspace()
        await restarted.start()
        #expect(restarted.snapshot.repositories.allSatisfy { !$0.stale && $0.prInventoryComplete && !$0.prStale })
        let board = BoardState(); board.dateRange = .days(0)
        #expect(board.filteredCommits(restarted.snapshot).contains { $0.repo == RefreshRecoveryFixture.second })
        #expect(board.pullRequests(restarted.snapshot).contains { $0.repo == RefreshRecoveryFixture.second })
        await restarted.shutdown()
    }

    @Test func laterPartialRefreshRetainsCachedHistory() async throws {
        let fixture = try RefreshRecoveryFixture()
        defer { fixture.remove() }
        let workspace = fixture.workspace()
        await workspace.start()
        #expect(workspace.snapshot.coverage.phase == "ready")
        await fixture.historyRefresh.fail(RefreshRecoveryFixture.second)
        await workspace.refresh()
        #expect(workspace.snapshot.repositories.contains { $0.id == RefreshRecoveryFixture.second && $0.stale })
        await workspace.shutdown()
        var config = try fixture.storage.loadConfiguration(); config.cachedOnly = true
        try fixture.storage.saveConfiguration(config)
        let restarted = fixture.workspace()
        await restarted.start()
        #expect(restarted.snapshot.cachedOnly && !restarted.snapshot.commits.isEmpty)
        await restarted.shutdown()
    }

    @Test func boundedSupportingDetailsKeepInventoryAvailable() async throws {
        var config = RefreshRecoveryFixture.configuration
        config.repositories = [RefreshRecoveryFixture.first]; config.requestBudget = 1
        let fixture = try RefreshRecoveryFixture(configuration: config)
        defer { fixture.remove() }
        await fixture.network.setMode(.supportingDetails, for: RefreshRecoveryFixture.first)
        let workspace = fixture.workspace()
        await workspace.start()
        let repo = try #require(workspace.snapshot.repositories.first)
        #expect(!repo.stale && repo.prInventoryComplete && !repo.prStale)
        #expect(!repo.prFilesComplete && !repo.prReviewersComplete)
        #expect(workspace.snapshot.coverage.phase == "partial")
        #expect(workspace.snapshot.coverage.inventoryRequests == 1)
        await workspace.shutdown()
    }

    @Test func groupsAndEmptyScopesWorkOffline() async throws {
        var config = RefreshRecoveryFixture.configuration
        config.groups = ["Focus": [RefreshRecoveryFixture.first], "Empty": []]
        let fixture = try RefreshRecoveryFixture(configuration: config)
        defer { fixture.remove() }
        await fixture.historyRefresh.fail(RefreshRecoveryFixture.second)
        let workspace = fixture.workspace()
        await workspace.start()
        await workspace.selectGroup("Focus")
        #expect(workspace.snapshot.repositories.map(\.id) == [RefreshRecoveryFixture.first])
        #expect(workspace.snapshot.coverage.phase == "ready")
        await workspace.selectGroup("Empty")
        #expect(workspace.snapshot.repositories.isEmpty && workspace.snapshot.commits.isEmpty)
        await workspace.shutdown()
        config = try fixture.storage.loadConfiguration(); config.cachedOnly = true
        try fixture.storage.saveConfiguration(config)
        let requests = await fixture.network.requests
        let offline = fixture.workspace()
        await offline.start()
        #expect(offline.snapshot.cachedOnly && !offline.snapshot.commits.isEmpty)
        await offline.selectGroup("Focus")
        #expect(offline.snapshot.repositories.map(\.id) == [RefreshRecoveryFixture.first])
        await offline.shutdown()
        #expect(await fixture.network.requests == requests)
    }

    @Test(arguments: RefreshRecoveryInterruption.allCases)
    func accessDenialKeepsLocalHistory(_ interruption: RefreshRecoveryInterruption) async throws {
        let fixture = try RefreshRecoveryFixture()
        defer { fixture.remove() }
        if interruption == .history { await fixture.historyRefresh.fail(RefreshRecoveryFixture.second, failure: .permission) }
        else { await fixture.network.setMode(.denied, for: RefreshRecoveryFixture.second) }
        let workspace = fixture.workspace()
        await workspace.start()
        let both = [RefreshRecoveryFixture.first, RefreshRecoveryFixture.second]
        #expect(workspace.authStatus.signedIn)
        #expect(workspace.snapshot.repositories.map(\.id) == both)
        #expect(workspace.snapshot.commits.contains { $0.repo == RefreshRecoveryFixture.second })
        #expect(workspace.snapshot.pullRequests.contains { $0.repo == RefreshRecoveryFixture.second } == (interruption == .history))
        if interruption == .history {
            let row = workspace.snapshot.repositories.first { $0.id == RefreshRecoveryFixture.second }
            #expect(row?.stale == true && row?.error.hasPrefix("GitHub denied the fetch; showing local history.") == true)
        }
        #expect(workspace.discovered.map(\.id) == both)
        await workspace.shutdown()
    }
}

enum RefreshRecoveryInterruption: CaseIterable, Sendable { case history, inventory }

@MainActor private struct RefreshRecoveryFixture {
    static let first = "owner/one"
    static let second = "owner/two"
    static var configuration: WorkspaceConfiguration {
        var value = WorkspaceConfiguration(); value.repositories = [first, second]; return value
    }
    let history: HistoryFixture
    let storage: WorkspaceStorage
    let network: RefreshRecoveryTransport
    let historyRefresh = RefreshRecoveryHistoryRefresh()
    init(configuration original: WorkspaceConfiguration = RefreshRecoveryFixture.configuration) throws {
        history = try HistoryFixture()
        storage = WorkspaceStorage(configRoot: history.base.appendingPathComponent("support"), cacheRoot: history.base.appendingPathComponent("workspace-cache"))
        network = RefreshRecoveryTransport(repositories: [Self.first, Self.second])
        let sources = history.base.appendingPathComponent("recovery-src")
        var configuration = original
        configuration.sourceRoots = [sources.path]
        try storage.saveConfiguration(configuration)
        for id in [Self.first, Self.second] { try history.addClone(id, at: sources.appendingPathComponent(id)) }
    }
    func workspace() -> WorkspaceModel {
        let auth = cliAuth(storage: storage)
        let refresh = historyRefresh
        return WorkspaceModel(storage: storage, authentication: auth, transport: network, refreshHistory: { _, repo, _ in try await refresh.refresh(repo) })
    }
    func remove() { try? FileManager.default.removeItem(at: history.base) }
}

private actor RefreshRecoveryHistoryRefresh {
    private var failures: [String: GitHubFailure] = [:]
    func fail(_ id: String, failure: GitHubFailure = .unavailable) { failures[id] = failure }
    func recover() { failures = [:] }
    func refresh(_ repo: Repository) throws -> Repository {
        if let failure = failures[repo.id] { throw failure }
        var value = repo; value.stale = false; value.error = ""; return value
    }
}

private actor RefreshRecoveryTransport: GitHubTransport {
    enum Mode: Sendable { case complete, incomplete, supportingDetails, denied }
    private let repositories: [String]
    private var modes: [String: Mode] = [:]
    private(set) var requests = 0
    init(repositories: [String]) { self.repositories = repositories }
    func setMode(_ mode: Mode, for id: String) { modes[id] = mode }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        requests += 1
        let object: Any
        switch request.url?.path {
        case "/user": object = ["login": "fixture", "id": 123]
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
