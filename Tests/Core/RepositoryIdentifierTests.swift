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
        #expect(CloneDiscovery.githubRepository("git@github.com:Owner/-Repo.git") == "owner/-repo")
        let repo = Repository(id: "owner/-repo")
        let storage = WorkspaceStorage(configRoot: fixture.base.appendingPathComponent("support"), cacheRoot: fixture.root)
        var configuration = try storage.loadConfiguration()
        configuration.repositories = [repo.id]
        try storage.saveConfiguration(configuration)
        #expect(try storage.loadConfiguration().repositories == [repo.id])

        let clone = try fixture.addClone(repo.id)
        let discovered = try #require(await CloneDiscovery.find(in: [fixture.sources]).first { $0.id == repo.id })
        #expect(discovered.path == (try fixture.gitDirectory(clone)) && discovered.branch == "main")
        let history = GitHistory(root: fixture.root)
        let scanned = try await history.scan(discovered, configuration: configuration)
        #expect(scanned.0.id == repo.id && !scanned.0.revision.isEmpty)
        #expect(!scanned.1.isEmpty && scanned.1.allSatisfy { $0.repo == repo.id })
    }
}
