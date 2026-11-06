import Foundation
import CryptoKit
import Testing
@testable import Bigboard

@MainActor struct CollectionScopeTests {
    @Test func cachedStartupOnlyCollectsSavedRepositories() async throws {
        let ids = ["owner/chosen", "owner/unchecked", "owner/new-private"]
        var config = WorkspaceConfiguration(repositories: [ids[0]])
        config.cachedOnly = true
        config.groups = ["Mixed": [ids[0], ids[2]], "Unchecked": [ids[2]]]
        let fixture = try CollectionFixture(config: config, cloned: ids)
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [ids[0]])
        #expect(Set(fixture.workspace.snapshot.commits.map(\.repo)) == [ids[0]])
        #expect(fixture.workspace.snapshot.commits.count == 2)
        #expect(fixture.scanExists(ids[0]))
        #expect(!fixture.scanExists(ids[1]) && !fixture.scanExists(ids[2]))
        #expect(await fixture.network.calls == 0)
    }

    @Test func liveDiscoveryKeepsNewRepositoriesVisibleWithoutCollectingThem() async throws {
        let chosen = "owner/chosen", new = "owner/new-private"
        let config = WorkspaceConfiguration(repositories: [chosen])
        let fixture = try CollectionFixture(config: config, cloned: [chosen, new])
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(Set(fixture.workspace.discovered.map(\.id)) == [chosen, new])
        #expect(fixture.workspace.configuration.repositories == [chosen])
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [chosen])
        #expect(Set(fixture.workspace.snapshot.commits.map(\.repo)) == [chosen])
        #expect(await fixture.network.graphRepositories == [chosen])
        #expect(!fixture.scanExists(new) && !fixture.lockExists(new))
        let choices = SettingsRepositories.rows(available: fixture.workspace.discovered, saved: fixture.workspace.configuration.repositories, sort: .name, query: "")
        #expect(choices.map(\.id) == [chosen, new])
        #expect(!Set(fixture.workspace.configuration.repositories).contains(new))
    }

    @Test func gitFetchFailureKeepsLocalHistoryAndAccess() async throws {
        let denied = "owner/denied", transient = "owner/transient"
        let ids = [denied, transient]
        let config = WorkspaceConfiguration(repositories: ids)
        let fixture = try CollectionFixture(config: config, cloned: ids, refreshHistory: { _, repo in
            if repo.id == denied {
                throw GitCommandFailure(status: 128, detail: "fatal: Could not read from remote repository.")
            }
            throw BigboardError.message("Access denied: local Git process could not start.")
        })
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(fixture.workspace.authStatus.signedIn)
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == ids)
        #expect(fixture.workspace.snapshot.repositories.allSatisfy { $0.stale })
        #expect(Set(fixture.workspace.snapshot.commits.map(\.repo)) == Set(ids))
        let deniedRow = fixture.workspace.snapshot.repositories.first { $0.id == denied }
        #expect(deniedRow?.error == "fatal: Could not read from remote repository.")
        let transientRow = fixture.workspace.snapshot.repositories.first { $0.id == transient }
        #expect(transientRow?.error == "Access denied: local Git process could not start.")
        #expect(Set(fixture.workspace.discovered.map(\.id)) == Set(ids))
    }

    @Test func discoveryFollowsClonesOnDisk() async throws {
        let ids = ["owner/chosen", "owner/removed"]
        let config = WorkspaceConfiguration(repositories: ids.sorted())
        let fixture = try CollectionFixture(config: config, cloned: ids)
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        try #require(fixture.workspace.snapshot.coverage.phase == "ready")
        #expect(Set(fixture.workspace.snapshot.repositories.map(\.id)) == Set(ids))
        #expect(Set(fixture.workspace.snapshot.commits.map(\.repo)) == Set(ids))
        #expect(fixture.workspace.discovered.allSatisfy { !$0.path.isEmpty })

        try FileManager.default.removeItem(at: fixture.clones.appendingPathComponent(ids[1]))
        await fixture.workspace.discover()
        try #require(fixture.workspace.error.isEmpty)
        #expect(fixture.workspace.discovered.map(\.id) == [ids[0]])
        await fixture.workspace.refresh()
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [ids[0]])
        #expect(Set(fixture.workspace.snapshot.commits.map(\.repo)) == [ids[0]])
        #expect(fixture.workspace.discovered.map(\.id) == [ids[0]])
    }

    @Test func changingSourceFoldersRediscoversAndRequiresLocalClones() async throws {
        let chosen = "owner/chosen"
        let empty = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: empty, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: empty) }
        var config = WorkspaceConfiguration(); config.sourceRoots = [empty.path]
        let fixture = try CollectionFixture(config: config, cloned: [chosen])
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(fixture.workspace.discovered.isEmpty)
        var draft = fixture.workspace.configuration
        draft.sourceRoots = [fixture.clones.path]
        draft.repositories = [chosen]
        #expect(await fixture.workspace.saveConfiguration(draft))
        #expect(fixture.workspace.discovered.map(\.id) == [chosen])
        let deadline = ContinuousClock.now.advanced(by: .seconds(10))
        while fixture.workspace.busy && ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [chosen])
        var missing = fixture.workspace.configuration
        missing.repositories = [chosen, "owner/absent"]
        #expect(await fixture.workspace.saveConfiguration(missing) == false)
        #expect(fixture.workspace.error.contains("No local clone was found for: owner/absent"))
        #expect(fixture.workspace.configuration.repositories == [chosen])
    }

    @Test func noSourceFoldersAsksForThemBeforeFindingClones() async throws {
        let id = "owner/chosen"
        let fixture = try CollectionFixture(
            config: WorkspaceConfiguration(repositories: [id]), cloned: [id], chooseSources: false)
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(fixture.workspace.configuration.sourceRoots.isEmpty)
        #expect(fixture.workspace.snapshot.coverage.phase == "setup")
        #expect(fixture.workspace.discovered.isEmpty && fixture.workspace.snapshot.repositories.isEmpty)
        #expect(await fixture.workspace.addSourceFolders([fixture.clones.path, fixture.clones.path]))
        #expect(fixture.workspace.configuration.sourceRoots == [fixture.clones.path])
        #expect(try fixture.storage.loadConfiguration().sourceRoots == [fixture.clones.path])
        #expect(fixture.workspace.discovered.map(\.id) == [id])
        let deadline = ContinuousClock.now.advanced(by: .seconds(10))
        while fixture.workspace.busy && ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [id])
        #expect(await fixture.workspace.addSourceFolders(["relative/path"]) == false)
        #expect(fixture.workspace.configuration.sourceRoots == [fixture.clones.path])
    }

    @Test func freshSetupDiscoversChoicesWithoutScanningClones() async throws {
        let ids = ["owner/chosen", "owner/new-private"]
        let fixture = try CollectionFixture(config: WorkspaceConfiguration(), cloned: ids)
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
        config.repositories = Array(ids.prefix(2))
        config.exclude = [pattern]; config.cachedOnly = true; config.groups = ["Mixed": ids]
        let fixture = try CollectionFixture(config: config, cloned: ids)
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(fixture.workspace.snapshot.repositories.map(\.id) == [ids[0]])
        #expect(ids.dropFirst().allSatisfy { !fixture.scanExists($0) })
        #expect(fixture.workspace.configuration.repositories == Array(ids.prefix(2)))
        #expect(await fixture.network.calls == 0)
    }

    @Test func savingNarrowerCollectionRemovesRetainedEvidenceBeforeDiscoveryFailure() async throws {
        let ids = ["owner/chosen", "owner/removed"]
        var config = WorkspaceConfiguration(); config.repositories = ids; config.cachedOnly = true
        let fixture = try CollectionFixture(config: config, cloned: ids)
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        await fixture.workspace.start()
        #expect(Set(fixture.workspace.snapshot.repositories.map(\.id)) == Set(ids))
        await fixture.workspace.discover()
        fixture.workspace.snapshot.pullRequests = ids.map { PullRequest(repo: $0, number: 1, title: "Retained", author: "fixture", url: "", updatedAt: Date()) }
        await fixture.network.failDiscovery()
        var draft = fixture.workspace.configuration
        draft.repositories = [ids[0]]
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
    let clones: URL
    let storage: WorkspaceStorage
    let repositoryRoot: URL
    let network: CollectionTransport
    let workspace: WorkspaceModel
    init(config original: WorkspaceConfiguration, cloned: [String], chooseSources: Bool = true, refreshHistory: @escaping @Sendable (GitHistory, Repository) async throws -> Repository = { _, repo in repo }) throws {
        let history = try HistoryFixture()
        base = history.base
        clones = base.appendingPathComponent("collection-src")
        try FileManager.default.createDirectory(at: clones, withIntermediateDirectories: true)
        storage = WorkspaceStorage(configRoot: base.appendingPathComponent("support"), cacheRoot: base.appendingPathComponent("workspace-cache"))
        repositoryRoot = storage.historyRoot
        for id in cloned { try history.addClone(id, at: clones.appendingPathComponent(id)) }
        var config = original
        if chooseSources, config.sourceRoots.isEmpty { config.sourceRoots = [clones.path] }
        try storage.saveConfiguration(config)
        network = CollectionTransport()
        let auth = cliAuth(storage: storage)
        workspace = WorkspaceModel(storage: storage, authentication: auth, transport: network, refreshHistory: refreshHistory)
    }
    func scanExists(_ id: String) -> Bool { FileManager.default.fileExists(atPath: repositoryRoot.appendingPathComponent("scans/" + hash(id) + ".json").path) }
    func lockExists(_ id: String) -> Bool { FileManager.default.fileExists(atPath: repositoryRoot.appendingPathComponent("locks/" + hash(id) + ".lock").path) }
    private func hash(_ id: String) -> String { SHA256.hash(data: Data(id.utf8)).map { String(format: "%02x", $0) }.joined() }
}

private actor CollectionTransport: GitHubTransport {
    private var discoveryFails = false
    private(set) var calls = 0
    private(set) var graphRepositories: [String] = []
    func failDiscovery() { discoveryFails = true }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        calls += 1
        let object: Any
        switch request.url?.path {
        case "/user":
            if discoveryFails { throw GitHubFailure.unavailable }
            object = ["login": "fixture", "id": 123]
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
