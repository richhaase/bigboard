import Foundation
import CryptoKit
import Testing
@testable import Bigboard

@MainActor struct CollectionScopeTests {
    @Test func previewGroupsPreserveIsolatedFixtureEvidence() async {
        let workspace = PreviewFixture.make()
        let repositories = workspace.snapshot.repositories
        let commits = workspace.snapshot.commits
        let pullRequests = workspace.snapshot.pullRequests
        let discovered = workspace.discovered
        for group in workspace.configuration.groups.keys.sorted() + [""] {
            await workspace.selectGroup(group)
            #expect(workspace.activeGroup == group)
            #expect(workspace.snapshot.repositories == repositories)
            #expect(workspace.snapshot.commits == commits)
            #expect(workspace.snapshot.pullRequests == pullRequests)
            #expect(workspace.discovered == discovered)
        }
    }

    @Test func cachedStartupAndGroupsOnlyCollectSavedRepositories() async throws {
        let ids = ["owner/chosen", "owner/unchecked", "owner/new-private"]
        var config = SettingsRepositories.applying([ids[0]], available: Set(ids.prefix(2)), to: WorkspaceConfiguration())
        config.cachedOnly = true
        config.groups = ["Mixed": [ids[0], ids[2]], "Unchecked": [ids[2]]]
        let fixture = try CollectionFixture(config: config, granted: ids, cached: ids)
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [ids[0]])
        #expect(Set(fixture.workspace.snapshot.commits.map(\.repo)) == [ids[0]])
        #expect(fixture.workspace.snapshot.commits.count == 2)
        #expect(fixture.scanExists(ids[0]))
        #expect(!fixture.scanExists(ids[1]) && !fixture.scanExists(ids[2]))
        await fixture.workspace.selectGroup("Mixed")
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [ids[0]])
        await fixture.workspace.selectGroup("Unchecked")
        #expect(fixture.workspace.snapshot.repositories.isEmpty)
        #expect(fixture.workspace.snapshot.commits.isEmpty && fixture.workspace.snapshot.pullRequests.isEmpty)
        #expect(!fixture.scanExists(ids[2]))
        #expect(await fixture.network.calls == 0)
    }

    @Test func liveDiscoveryKeepsNewRepositoriesVisibleWithoutCollectingThem() async throws {
        let chosen = "owner/chosen", new = "owner/new-private"
        let config = SettingsRepositories.applying([chosen], available: [chosen], to: WorkspaceConfiguration())
        let fixture = try CollectionFixture(config: config, granted: [chosen], cached: [chosen], discovered: [chosen, new])
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(Set(fixture.workspace.discovered.map(\.id)) == [chosen, new])
        #expect(fixture.workspace.configuration.repositories == [chosen])
        #expect(!fixture.workspace.configuration.excludedRepositories.contains(new))
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [chosen])
        #expect(Set(fixture.workspace.snapshot.commits.map(\.repo)) == [chosen])
        #expect(await fixture.network.graphRepositories == [chosen])
        #expect(!fixture.scanExists(new) && !fixture.lockExists(new))
        let choices = SettingsRepositories.rows(available: fixture.workspace.discovered, saved: fixture.workspace.configuration.repositories, sort: .name, query: "")
        #expect(choices.map(\.id) == [chosen, new])
        #expect(!Set(fixture.workspace.configuration.repositories).contains(new))
    }

    @Test func settingsDiscoveryRequiresFullRefreshBeforeRemovingNewlyUnlistedCache() async throws {
        let ids = ["owner/chosen", "owner/removed"]
        let config = SettingsRepositories.applying(Set(ids), available: Set(ids), to: WorkspaceConfiguration())
        let fixture = try CollectionFixture(config: config, granted: ids, cached: ids, refreshHistory: { _, repo, _ in repo })
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        try #require(fixture.workspace.snapshot.coverage.phase == "ready")
        #expect(Set(fixture.workspace.snapshot.repositories.map(\.id)) == Set(ids))
        #expect(Set(fixture.workspace.snapshot.commits.map(\.repo)) == Set(ids))
        #expect(Set(fixture.workspace.cachedRepositoryRows.map(\.id)) == Set(ids))
        #expect(fixture.workspace.removableCacheIDs.isEmpty)
        try #require(ids.allSatisfy { fixture.cacheExists($0) && fixture.scanExists($0) })

        await fixture.network.setRepositories([ids[0]])
        await fixture.workspace.discover()
        try #require(fixture.workspace.error.isEmpty)
        #expect(fixture.workspace.discovered.map(\.id) == [ids[0]])
        #expect(fixture.workspace.removableCacheIDs.isEmpty)
        await fixture.workspace.removeCache(ids[1])
        #expect(fixture.workspace.error == "Refresh live access before removing an unlisted cache.")
        #expect(ids.allSatisfy { fixture.cacheExists($0) && fixture.scanExists($0) })
        #expect(Set(fixture.workspace.cachedRepositoryRows.map(\.id)) == Set(ids))

        await fixture.workspace.refresh()
        try #require(fixture.workspace.snapshot.coverage.phase == "ready")
        try #require(fixture.workspace.error.isEmpty)
        #expect(fixture.workspace.removableCacheIDs == [ids[1]])
        await fixture.workspace.removeCache(ids[1])
        #expect(fixture.workspace.error.isEmpty)
        #expect(!fixture.cacheExists(ids[1]) && !fixture.scanExists(ids[1]))
        #expect(fixture.cacheExists(ids[0]) && fixture.scanExists(ids[0]))
        #expect(fixture.workspace.cachedRepositoryRows.map(\.id) == [ids[0]])
        #expect(fixture.workspace.removableCacheIDs.isEmpty)
    }

    @Test func freshSetupDiscoversChoicesWithoutScanningGrantedCaches() async throws {
        let ids = ["owner/chosen", "owner/new-private"]
        let fixture = try CollectionFixture(config: WorkspaceConfiguration(), granted: [ids[0]], cached: [ids[0]], discovered: ids)
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(fixture.workspace.snapshot.coverage.phase == "setup")
        #expect(Set(fixture.workspace.discovered.map(\.id)) == Set(ids))
        #expect(fixture.workspace.configuration.repositories.isEmpty)
        #expect(fixture.workspace.snapshot.repositories.isEmpty && fixture.workspace.snapshot.commits.isEmpty)
        #expect(!fixture.scanExists(ids[0]) && !fixture.scanExists(ids[1]))
        #expect(await fixture.network.graphRepositories.isEmpty)
        #expect(!fixture.lockExists(ids[1]))
    }

    @Test(arguments: ["vendor-*", "owner/vendor-*"])
    func exclusionsNarrowSavedRepositoriesAndGroupMembership(pattern: String) async throws {
        let ids = ["owner/keep", "owner/vendor-lib", "owner/exact", "owner/unchecked"]
        var config = WorkspaceConfiguration()
        config.repositories = Array(ids.prefix(3)); config.excludedRepositories = [ids[2]]
        config.exclude = [pattern]; config.cachedOnly = true; config.groups = ["Mixed": ids]
        let fixture = try CollectionFixture(config: config, granted: ids, cached: ids)
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [ids[0]])
        await fixture.workspace.selectGroup("Mixed")
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [ids[0]])
        #expect(ids.dropFirst().allSatisfy { !fixture.scanExists($0) })
        #expect(fixture.workspace.configuration.repositories == Array(ids.prefix(3)))
        #expect(await fixture.network.calls == 0)
    }

    @Test func savingNarrowerCollectionRemovesRetainedEvidenceBeforeDiscoveryFailure() async throws {
        let ids = ["owner/chosen", "owner/removed"]
        var config = WorkspaceConfiguration(); config.repositories = ids; config.cachedOnly = true
        let fixture = try CollectionFixture(config: config, granted: ids, cached: ids)
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(Set(fixture.workspace.snapshot.repositories.map(\.id)) == Set(ids))
        await fixture.workspace.discover()
        fixture.workspace.snapshot.pullRequests = ids.map { PullRequest(repo: $0, number: 1, title: "Retained", author: "fixture", url: "", updatedAt: Date()) }
        await fixture.network.failDiscovery()
        var draft = SettingsRepositories.applying([ids[0]], available: Set(ids), to: fixture.workspace.configuration)
        draft.cachedOnly = false
        #expect(await fixture.workspace.saveConfiguration(draft))
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [ids[0]])
        #expect(fixture.workspace.snapshot.pullRequests.map(\.repo) == [ids[0]])
        let deadline = ContinuousClock.now.advanced(by: .seconds(10))
        while fixture.workspace.busy && ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        #expect(!fixture.workspace.busy)
        #expect(fixture.workspace.snapshot.coverage.phase == "partial")
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [ids[0]])
        #expect(Set(fixture.workspace.snapshot.commits.map(\.repo)) == [ids[0]])
        #expect(fixture.workspace.snapshot.pullRequests.map(\.repo) == [ids[0]])
        #expect(Set(fixture.workspace.discovered.map(\.id)) == Set(ids))
        #expect(await fixture.network.graphRepositories.isEmpty)
    }
}

@MainActor private struct CollectionFixture {
    let base: URL
    let repositoryRoot: URL
    let network: CollectionTransport
    let workspace: WorkspaceModel
    init(config: WorkspaceConfiguration, granted: [String], cached: [String], discovered: [String]? = nil, refreshHistory: @escaping @Sendable (GitHistory, Repository, String) async throws -> Repository = { try await $0.refresh($1, token: $2) }) throws {
        let history = try HistoryFixture()
        base = history.base
        let storage = WorkspaceStorage(supportRoot: base.appendingPathComponent("support"), cacheRoot: base.appendingPathComponent("workspace-cache"))
        repositoryRoot = try storage.repositoryRoot("123")
        for id in cached {
            let destination = repositoryRoot.appendingPathComponent("github.com/" + id + ".git")
            try FileManager.default.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true)
            try FileManager.default.copyItem(at: history.bare, to: destination)
            try Data((id + "\n").utf8).write(to: destination.appendingPathComponent("bigboard-cache"))
            _ = try history.git(["--git-dir=" + destination.path, "config", "remote.origin.url", "https://github.com/\(id).git"])
        }
        try storage.saveConfiguration(config, account: "123")
        try storage.saveGrant(granted.map { Repository(id: $0) }, account: "123")
        network = CollectionTransport(repositories: discovered ?? granted)
        let auth = AuthManager(configuration: AuthConfiguration(clientID: "fixture-client", clientSecret: "fixture-secret", appSlug: "fixture-app"), store: try CollectionCredentials(), transport: network)
        workspace = WorkspaceModel(storage: storage, authentication: auth, transport: network, refreshHistory: refreshHistory)
    }
    func cacheExists(_ id: String) -> Bool { FileManager.default.fileExists(atPath: repositoryRoot.appendingPathComponent("github.com/" + id + ".git/bigboard-cache").path) }
    func scanExists(_ id: String) -> Bool { FileManager.default.fileExists(atPath: repositoryRoot.appendingPathComponent("scans/" + hash(id) + ".json").path) }
    func lockExists(_ id: String) -> Bool { FileManager.default.fileExists(atPath: repositoryRoot.appendingPathComponent("locks/" + hash(id) + ".lock").path) }
    private func hash(_ id: String) -> String { SHA256.hash(data: Data(id.utf8)).map { String(format: "%02x", $0) }.joined() }
}

private actor CollectionTransport: GitHubTransport {
    private var repositories: [String]
    private var discoveryFails = false
    private(set) var calls = 0
    private(set) var graphRepositories: [String] = []
    init(repositories: [String]) { self.repositories = repositories }
    func setRepositories(_ repositories: [String]) { self.repositories = repositories }
    func failDiscovery() { discoveryFails = true }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        calls += 1
        let object: [String: Any]
        switch request.url?.path {
        case "/user": object = ["login": "fixture", "id": 123]
        case "/user/installations":
            if discoveryFails { throw GitHubFailure.unavailable }
            object = ["total_count": 1, "installations": [["id": 1, "account": ["login": "owner", "type": "Organization"]]]]
        case "/user/installations/1/repositories":
            object = ["total_count": repositories.count, "repositories": repositories.map { ["full_name": $0, "default_branch": "", "archived": false] as [String: Any] }]
        case "/graphql":
            guard let body = request.httpBody, let document = try JSONSerialization.jsonObject(with: body) as? [String: Any], let variables = document["variables"] as? [String: Any], let owner = variables["owner"] as? String, let name = variables["name"] as? String else { throw GitHubFailure.response }
            let id = owner + "/" + name
            graphRepositories.append(id)
            object = ["data": ["repository": ["nameWithOwner": id, "pullRequests": ["totalCount": 0, "nodes": [], "pageInfo": ["hasNextPage": false, "endCursor": NSNull()]]]]]
        default: throw GitHubFailure.unavailable
        }
        return GitHubHTTPResponse(status: 200, headers: [:], data: try JSONSerialization.data(withJSONObject: object))
    }
}

private final class CollectionCredentials: CredentialStore, @unchecked Sendable {
    private let lock = NSLock()
    private var data: Data?
    init() throws {
        let credentials = AuthCredentials(accessToken: "fixture-access", refreshToken: "fixture-refresh", expiresAt: Date().addingTimeInterval(3600), refreshExpiresAt: Date().addingTimeInterval(86400), login: "fixture", accountID: "123", clientID: "fixture-client")
        let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
        data = try encoder.encode(credentials)
    }
    func load() throws -> Data? { lock.withLock { data } }
    func save(_ data: Data) throws { lock.withLock { self.data = data } }
    func delete() throws { lock.withLock { data = nil } }
}
