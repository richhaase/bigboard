import Foundation
import Testing
@testable import Bigboard

struct CloneDiscoveryTests {
    @Test(arguments: [
        ("https://github.com/Owner/Repo.git", "owner/repo"), ("https://github.com/owner/repo", "owner/repo"),
        ("https://github.com/owner/repo/", "owner/repo"), ("http://github.com/owner/repo.git", "owner/repo"),
        ("ssh://git@github.com/owner/repo.git", "owner/repo"), ("git@github.com:owner/repo.git", "owner/repo"),
        ("git@github.com:owner/repo", "owner/repo"), ("git://github.com/owner/-repo.git", "owner/-repo"),
    ])
    func gitHubRemoteURLsMapToRepositories(url: String, id: String) {
        #expect(CloneDiscovery.githubRepository(url) == id)
    }
    @Test(arguments: [
        "https://gitlab.com/owner/repo.git", "git@bitbucket.org:owner/repo.git", "https://github.com/owner",
        "https://github.com/owner/repo/extra", "https://github.com/../repo", "git@github.com:-owner/repo.git",
        "/Users/me/src/repo", "",
    ])
    func otherRemotesAreIgnored(url: String) {
        #expect(CloneDiscovery.githubRepository(url) == nil)
    }
    @Test func upstreamIsPreferredOverOriginAndOtherRemotes() {
        let both = CloneDiscovery.remote([
            ("origin", "git@github.com:me/fork.git"), ("upstream", "https://github.com/team/project.git"),
        ])
        #expect(both?.name == "upstream" && both?.id == "team/project")
        let origin = CloneDiscovery.remote([
            ("backup", "https://github.com/me/backup.git"), ("origin", "https://github.com/me/main.git"),
        ])
        #expect(origin?.name == "origin" && origin?.id == "me/main")
        let other = CloneDiscovery.remote([
            ("zeta", "https://github.com/z/z.git"), ("alpha", "https://github.com/a/a.git"),
            ("origin", "https://gitlab.com/x/y.git"),
        ])
        #expect(other?.name == "alpha" && other?.id == "a/a")
        #expect(CloneDiscovery.remote([("origin", "https://gitlab.com/x/y.git")]) == nil)
    }
    @Test func inspectionReadsRemoteDefaultBranchAndLatestCommit() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let repo = try #require(await CloneDiscovery.inspect(fixture.clone))
        #expect(repo.id == "owner/repo" && repo.remote == "origin" && repo.branch == "main")
        #expect(repo.path == (try fixture.gitDirectory()))
        #expect(!repo.shallow && !repo.partial)
        #expect(repo.pushedAt == ISO8601DateFormatter().date(from: "2026-10-01T12:00:00Z"))
        _ = try fixture.git(["-C", fixture.clone.path, "remote", "add", "upstream", "git@github.com:team/project.git"])
        let upstream = try #require(await CloneDiscovery.inspect(fixture.clone))
        #expect(upstream.id == "team/project" && upstream.remote == "upstream" && upstream.branch == "")
        #expect(upstream.pushedAt == nil)
        _ = try fixture.git(["-C", fixture.clone.path, "remote", "remove", "upstream"])
        _ = try fixture.git(["-C", fixture.clone.path, "config", "remote.origin.url", "https://gitlab.com/owner/repo.git"])
        #expect(await CloneDiscovery.inspect(fixture.clone) == nil)
        #expect(await CloneDiscovery.inspect(fixture.base) == nil)
    }
    @Test func discoveryDeduplicatesWorktreesAndClonesAndSkipsIgnoredFolders() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        _ = try fixture.git(["-C", fixture.clone.path, "worktree", "add", "--quiet", fixture.sources.appendingPathComponent("worktree").path, "-b", "side"])
        try fixture.addClone("owner/repo", at: fixture.sources.appendingPathComponent("zcopy/repo"))
        try fixture.addClone("other/second")
        try fixture.addClone("hidden/repo", at: fixture.sources.appendingPathComponent(".hidden/repo"))
        try fixture.addClone("modules/repo", at: fixture.sources.appendingPathComponent("app/node_modules/repo"))
        try fixture.addClone("deep/repo", at: fixture.sources.appendingPathComponent("a/b/c/d/e/repo"))
        let found = await CloneDiscovery.find(in: [fixture.sources])
        #expect(found.map(\.id) == ["other/second", "owner/repo"])
        #expect(found.last?.path == (try fixture.gitDirectory()))
        let deeper = await CloneDiscovery.find(in: [fixture.sources], maximumDepth: 6)
        #expect(deeper.map(\.id) == ["deep/repo", "other/second", "owner/repo"])
    }
    @Test func rootsAreOnlyTheConfiguredFolders() throws {
        let home = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: home) }
        try FileManager.default.createDirectory(at: home.appendingPathComponent("src"), withIntermediateDirectories: true)
        #expect(CloneDiscovery.roots([]).isEmpty)
        let src = home.resolvingSymlinksInPath().appendingPathComponent("src").path
        let roots = CloneDiscovery.roots([src, src + "/", home.appendingPathComponent("missing").path])
        #expect(roots.map(\.path) == [src])
    }
}
