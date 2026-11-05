import Foundation
import Testing
@testable import Bigboard

private actor FilterTestTransport: GitHubTransport {
    private(set) var count = 0
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        count += 1
        throw GitHubFailure.unavailable
    }
}
@MainActor struct RepositoryFilterTests {
    let now = Date(timeIntervalSince1970: 2_000_000_000)
    func snapshot() -> Snapshot {
        Snapshot(generatedAt: now, accountID: "123", viewer: "alex", repositories: [Repository(id: "acme/app"), Repository(id: "acme/library")], commits: [
            Commit(id: "app", repo: "acme/app", author: "Alex", email: "alex@example.test", date: now.addingTimeInterval(-3600), subject: "Application change", added: 10),
            Commit(id: "library", repo: "acme/library", author: "Sam", email: "sam@example.test", date: now.addingTimeInterval(-3600), subject: "Library change", added: 20)
        ], pullRequests: [
            PullRequest(repo: "acme/app", number: 1, title: "Application PR", author: "alex", url: "https://github.com/acme/app/pull/1", updatedAt: now),
            PullRequest(repo: "acme/library", number: 2, title: "Library PR", author: "sam", url: "https://github.com/acme/library/pull/2", updatedAt: now)
        ])
    }
    @Test func repositoryFilterOnlyChangesViewingScope() async throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: root) }
        let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
        var configuration = try storage.loadConfiguration("123")
        configuration.repositories = ["acme/app", "acme/library"]
        try storage.saveConfiguration(configuration, account: "123")
        let settingsURL = try storage.accountRoot("123").appendingPathComponent("workspace.json")
        let settingsBefore = try Data(contentsOf: settingsURL)
        let token = CLITokenSource()
        let network = FilterTestTransport()
        let auth = cliAuth(storage: storage, token: token)
        let workspace = WorkspaceModel(storage: storage, authentication: auth, transport: network)
        workspace.configuration = configuration
        workspace.snapshot = snapshot()
        let state = BoardState()
        state.dateRange = .days(30)
        #expect(state.setRepositoryFilter(["acme/library"], available: workspace.snapshot.repositories))
        #expect(state.visibleRepositories(workspace.snapshot).map(\.id) == ["acme/library"])
        #expect(state.filteredCommits(workspace.snapshot).map(\.id) == ["library"])
        state.switchBoard(.contributors)
        #expect(state.peopleRows(workspace.snapshot).map(\.name) == ["Sam"])
        state.switchBoard(.pullRequests)
        #expect(state.pullRequests(workspace.snapshot).map(\.id) == ["acme/library#2"])
        #expect(state.repositories == ["acme/library"])
        #expect(state.dateRange == .days(30))
        #expect(workspace.snapshot.repositories.count == 2)
        #expect(workspace.snapshot.commits.count == 2)
        #expect(workspace.snapshot.pullRequests.count == 2)
        #expect(workspace.snapshot.generatedAt == now)
        #expect(workspace.configuration == configuration)
        #expect(try Data(contentsOf: settingsURL) == settingsBefore)
        #expect(token.count == 0)
        #expect(await network.count == 0)
        #expect(!workspace.busy)
        #expect(state.setRepositoryFilter([], available: workspace.snapshot.repositories))
        #expect(state.repositoryFilterLabel == "All")
        #expect(state.visibleRepositories(workspace.snapshot).count == 2)
        #expect(state.filteredCommits(workspace.snapshot).count == 2)
        #expect(state.peopleRows(workspace.snapshot).count == 2)
        #expect(state.pullRequests(workspace.snapshot).count == 2)
        #expect(workspace.configuration == configuration)
        #expect(try Data(contentsOf: settingsURL) == settingsBefore)
        #expect(await network.count == 0)
    }
    @Test func repositoryFilterAcceptsOnlyLoadedSelectionsAndPreservesOtherFilters() {
        let value = snapshot()
        let state = BoardState()
        state.dateRange = .days(90)
        state.activity.query = "source"
        state.contributors.hideBots = true
        state.prs.authors = ["alex"]
        state.scope = ActivityScope(repo: "acme/app", path: "Sources")
        #expect(state.setRepositoryFilter(["acme/app", "unloaded/repo"], available: value.repositories))
        #expect(state.repositories == ["acme/app"])
        #expect(state.scope.path == "Sources")
        #expect(!state.setRepositoryFilter(["unloaded/repo"], available: value.repositories))
        #expect(state.repositories == ["acme/app"])
        state.showPRs(repo: "acme/app")
        #expect(state.setRepositoryFilter(["acme/library"], available: value.repositories))
        #expect(state.scope.repo.isEmpty)
        #expect(state.contextualRepository == nil)
        #expect(state.prs.authors == ["alex"])
        #expect(state.activity.query == "source")
        #expect(state.contributors.hideBots)
        #expect(state.dateRange == .days(90))
    }
}
