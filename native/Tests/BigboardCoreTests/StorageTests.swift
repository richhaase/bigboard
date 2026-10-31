import Foundation
import Testing
@testable import Bigboard

struct StorageTests {
    func temporary() throws -> WorkspaceStorage {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        return WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    }
    @Test func freshAccountAndIsolation() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        var config = try storage.loadConfiguration("42")
        #expect(config.repositories.isEmpty)
        config.repositories = ["owner/repo"]
        try storage.saveConfiguration(config, account: "42")
        #expect(try storage.loadConfiguration("42").repositories == ["owner/repo"])
        #expect(try storage.loadConfiguration("43").repositories.isEmpty)
        #expect(try storage.repositoryRoot("42") != storage.repositoryRoot("43"))
    }
    @Test func externalEditIsNotOverwritten() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        let config = try storage.loadConfiguration("42")
        try storage.saveConfiguration(config, account: "42")
        let path = try storage.accountRoot("42").appendingPathComponent("workspace.json")
        try Data("{}".utf8).write(to: path)
        #expect(throws: (any Error).self) { try storage.saveConfiguration(config, account: "42") }
        #expect(try Data(contentsOf: path) == Data("{}".utf8))
    }
    @Test func revokedAccessCannotReappear() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        try storage.saveGrant([Repository(id: "owner/private")], account: "42")
        #expect(try storage.loadGrant("42").count == 1)
        try storage.invalidateGrant("42")
        #expect(try storage.loadGrant("42").isEmpty)
        #expect(try storage.loadGrant("43").isEmpty)
    }
    @Test func bookmarksRemainMonotonicAndScoped() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        try storage.saveBookmarks(["all":Date(timeIntervalSince1970:200),"group:backend":Date(timeIntervalSince1970:100)], account:"42")
        try storage.saveBookmarks(["all":Date(timeIntervalSince1970:50)], account:"42")
        #expect(try storage.bookmarks("42")["all"] == Date(timeIntervalSince1970:200))
        #expect(try storage.bookmarks("42")["group:backend"] == Date(timeIntervalSince1970:100))
    }
    @Test func symbolicLinksAreRejectedAndStateIsPrivate() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        try storage.saveGrant([], account: "42")
        let account = try storage.accountRoot("42")
        let file = account.appendingPathComponent("access.json")
        let attributes = try FileManager.default.attributesOfItem(atPath: file.path)
        #expect((attributes[.posixPermissions] as? NSNumber)?.intValue == 0o600)
        let target = storage.supportRoot.appendingPathComponent("outside.json")
        try Data("private".utf8).write(to: target)
        try FileManager.default.removeItem(at: file)
        try FileManager.default.createSymbolicLink(at: file, withDestinationURL: target)
        #expect(throws: (any Error).self) { try storage.loadGrant("42") }
        #expect(throws: (any Error).self) { try storage.invalidateGrant("42") }
        #expect(try Data(contentsOf: target) == Data("private".utf8))
        let other = try storage.accountRoot("43")
        try FileManager.default.createSymbolicLink(at: other, withDestinationURL: account)
        #expect(throws: (any Error).self) { try storage.bookmarks("43") }
    }
    @Test func malformedIdentityAndPathsRejected() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        var config = WorkspaceConfiguration()
        config.identities = ["a@b":["A@B"].identityRule]
        #expect(throws: (any Error).self) { try storage.validate(config) }
        #expect(!WorkspaceStorage.validRepository("../repo"))
        #expect(!WorkspaceStorage.validRepository("owner/repo/extra"))
        #expect(throws: (any Error).self) { try storage.accountRoot("../../outside") }
    }
}
private extension Array where Element == String { var identityRule: IdentityRule { IdentityRule(aliases:self) } }
