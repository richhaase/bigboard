import Foundation
import Testing
@testable import Bigboard

struct RepositoryIdentifierTests {
    @Test(arguments: ["owner/-repo", "owner/--repo", "owner/-.repo", "owner/repo"])
    func repositoryNamesAgreeAcrossDiscoveryStorageAndHistory(_ id: String) {
        #expect(canonicalGitHubRepository(id) == id)
        #expect(WorkspaceStorage.validRepository(id))
        #expect(GitHistory.validID(id))
    }

    @Test(arguments: ["-owner/repo", "owner/..", "../repo", "owner/./repo", "owner/repo/extra", "owner/", "/repo", "owner/repo\n", "owner/repo\0"])
    func invalidCacheIdentifiersRemainRejected(_ id: String) {
        #expect(!GitHistory.validID(id))
    }

    @Test func discoveredLeadingHyphenRepositoryCanBeSavedAndScanned() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let github = GitHubService(token: { "fixture-token" }, transport: IdentifierTransport())
        let repositories = try await github.discover()
        let repo = try #require(repositories.first)
        #expect(repositories.count == 1 && repo.id == "owner/-repo")
        let storage = WorkspaceStorage(supportRoot: fixture.base.appendingPathComponent("support"), cacheRoot: fixture.root)
        var configuration = try storage.loadConfiguration("123")
        configuration.repositories = [repo.id]
        try storage.saveConfiguration(configuration, account: "123")
        #expect(try storage.loadConfiguration("123").repositories == [repo.id])

        let destination = fixture.root.appendingPathComponent("github.com/" + repo.id + ".git")
        try FileManager.default.copyItem(at: fixture.bare, to: destination)
        try Data((repo.id + "\n").utf8).write(to: destination.appendingPathComponent("bigboard-cache"))
        _ = try fixture.git(["--git-dir=" + destination.path, "config", "remote.origin.url", "https://github.com/" + repo.id + ".git"])
        let history = GitHistory(root: fixture.root)
        let scanned = try await history.scan(repo, configuration: configuration)
        #expect(scanned.0.id == repo.id && !scanned.0.revision.isEmpty)
        #expect(!scanned.1.isEmpty && scanned.1.allSatisfy { $0.repo == repo.id })
        let cached = try await history.cachedRepositories()
        #expect(cached.contains { $0.id == repo.id })
    }
}

private struct IdentifierTransport: GitHubTransport {
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        let body: String
        switch request.url?.path {
        case "/user/installations":
            body = #"{"total_count":1,"installations":[{"id":1,"account":{"login":"Owner","type":"Organization"}}]}"#
        case "/user/installations/1/repositories":
            body = #"{"total_count":1,"repositories":[{"full_name":"Owner/-Repo","default_branch":"main","archived":false}]}"#
        default: throw GitHubFailure.unavailable
        }
        return GitHubHTTPResponse(status: 200, headers: [:], data: Data(body.utf8))
    }
}
