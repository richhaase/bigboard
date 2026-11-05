import Foundation
import Testing
@testable import Bigboard

@Test(arguments: ApplicationIdentity.allCases)
func bundleIdentitySelectsStorage(identity: ApplicationIdentity) throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let contents = root.appendingPathComponent("Identity.bundle/Contents")
    try FileManager.default.createDirectory(at: contents, withIntermediateDirectories: true)
    let plist = try PropertyListSerialization.data(fromPropertyList: ["CFBundleIdentifier": identity.rawValue, "CFBundlePackageType": "BNDL"], format: .xml, options: 0)
    try plist.write(to: contents.appendingPathComponent("Info.plist"))
    let bundle = try #require(Bundle(url: contents.deletingLastPathComponent()))
    #expect(ApplicationIdentity.fromBundle(bundle) == identity)
    let storage = WorkspaceStorage.system(identity: .fromBundle(bundle), environment: [:])
    #expect(storage.supportRoot == FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("\(identity.rawValue).native").resolvingSymlinksInPath())
    #expect(storage.cacheRoot == FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("\(identity.rawValue).native").resolvingSymlinksInPath())
}

@Test func developmentStorageDoesNotLoadOrChangeProductionState() throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    func storage(_ identity: ApplicationIdentity) -> WorkspaceStorage {
        WorkspaceStorage(supportRoot: root.appendingPathComponent("support/\(identity.storageDirectory)"), cacheRoot: root.appendingPathComponent("cache/\(identity.storageDirectory)"))
    }
    let production = storage(.production)
    var original = try production.loadConfiguration("123")
    original.repositories = ["owner/production"]
    try production.saveConfiguration(original, account: "123")
    try production.saveGrant([Repository(id: "owner/production")], account: "123")
    let development = storage(.development)
    var config = try development.loadConfiguration("123")
    #expect(config.repositories.isEmpty)
    #expect(try development.loadGrant("123").isEmpty)
    config.repositories = ["owner/development"]
    try development.saveConfiguration(config, account: "123")
    #expect(try storage(.development).loadConfiguration("123") == config)
    #expect(try storage(.production).loadConfiguration("123") == original)
    #expect(try production.loadGrant("123").map(\.id) == ["owner/production"])
    #expect(try development.repositoryRoot("123") != production.repositoryRoot("123"))
}

@Test(arguments: ApplicationIdentity.allCases)
func explicitStorageOverridesRemainAvailable(identity: ApplicationIdentity) {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    let storage = WorkspaceStorage.system(identity: identity, environment: ["BIGBOARD_DATA_ROOT": root.appendingPathComponent("support").path, "BIGBOARD_CACHE_ROOT": root.appendingPathComponent("cache").path])
    #expect(storage.supportRoot == root.appendingPathComponent("support").resolvingSymlinksInPath())
    #expect(storage.cacheRoot == root.appendingPathComponent("cache").resolvingSymlinksInPath())
}
