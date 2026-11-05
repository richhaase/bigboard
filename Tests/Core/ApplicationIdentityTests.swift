import Foundation
import Testing
@testable import Bigboard

private func temporaryRoot() throws -> URL {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    return root.resolvingSymlinksInPath()
}

@Test(arguments: ApplicationIdentity.allCases)
func bundleIdentitySelectsStorageNames(identity: ApplicationIdentity) throws {
    let root = try temporaryRoot()
    defer { try? FileManager.default.removeItem(at: root) }
    let contents = root.appendingPathComponent("Identity.bundle/Contents")
    try FileManager.default.createDirectory(at: contents, withIntermediateDirectories: true)
    let plist = try PropertyListSerialization.data(fromPropertyList: ["CFBundleIdentifier": identity.rawValue, "CFBundlePackageType": "BNDL"], format: .xml, options: 0)
    try plist.write(to: contents.appendingPathComponent("Info.plist"))
    let bundle = try #require(Bundle(url: contents.deletingLastPathComponent()))
    #expect(ApplicationIdentity.fromBundle(bundle) == identity)
    #expect(identity.directoryName == (identity == .production ? "bigboard" : "bigboard-dev"))
}

@Test(arguments: ApplicationIdentity.allCases)
func defaultStorageUsesHomeConfigAndCacheFolders(identity: ApplicationIdentity) throws {
    let home = try temporaryRoot()
    defer { try? FileManager.default.removeItem(at: home) }
    let storage = WorkspaceStorage.system(identity: identity, environment: [:], home: home)
    #expect(storage.configRoot == home.appendingPathComponent(".config/\(identity.directoryName)"))
    #expect(storage.cacheRoot == home.appendingPathComponent(".cache/\(identity.directoryName)"))
    #expect(storage.configurationFile == storage.configRoot.appendingPathComponent("config.json"))
    #expect(storage.historyRoot == storage.cacheRoot.appendingPathComponent("history"))
}

@Test(arguments: ApplicationIdentity.allCases)
func xdgVariablesChooseBaseFoldersWhenAbsolute(identity: ApplicationIdentity) throws {
    let root = try temporaryRoot()
    defer { try? FileManager.default.removeItem(at: root) }
    let home = root.appendingPathComponent("home")
    let config = root.appendingPathComponent("xdg-config"), cache = root.appendingPathComponent("xdg-cache")
    let chosen = WorkspaceStorage.system(identity: identity, environment: ["XDG_CONFIG_HOME": config.path, "XDG_CACHE_HOME": cache.path], home: home)
    #expect(chosen.configRoot == config.appendingPathComponent(identity.directoryName))
    #expect(chosen.cacheRoot == cache.appendingPathComponent(identity.directoryName))
    let relative = WorkspaceStorage.system(identity: identity, environment: ["XDG_CONFIG_HOME": "relative/config", "XDG_CACHE_HOME": ""], home: home)
    #expect(relative.configRoot == home.appendingPathComponent(".config/\(identity.directoryName)"))
    #expect(relative.cacheRoot == home.appendingPathComponent(".cache/\(identity.directoryName)"))
}

@Test(arguments: ApplicationIdentity.allCases)
func explicitStorageOverridesWinOverXDG(identity: ApplicationIdentity) throws {
    let root = try temporaryRoot()
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage.system(identity: identity, environment: [
        "BIGBOARD_CONFIG_ROOT": root.appendingPathComponent("config").path,
        "BIGBOARD_CACHE_ROOT": root.appendingPathComponent("cache").path,
        "XDG_CONFIG_HOME": root.appendingPathComponent("ignored-config").path,
        "XDG_CACHE_HOME": root.appendingPathComponent("ignored-cache").path,
    ], home: root.appendingPathComponent("home"))
    #expect(storage.configRoot == root.appendingPathComponent("config"))
    #expect(storage.cacheRoot == root.appendingPathComponent("cache"))
}

@Test func developmentStorageDoesNotLoadOrChangeProductionState() throws {
    let home = try temporaryRoot()
    defer { try? FileManager.default.removeItem(at: home) }
    let production = WorkspaceStorage.system(identity: .production, environment: [:], home: home)
    var original = try production.loadConfiguration()
    original.repositories = ["owner/production"]
    try production.saveConfiguration(original)
    let development = WorkspaceStorage.system(identity: .development, environment: [:], home: home)
    var config = try development.loadConfiguration()
    #expect(config.repositories.isEmpty)
    config.repositories = ["owner/development"]
    try development.saveConfiguration(config)
    #expect(try WorkspaceStorage.system(identity: .development, environment: [:], home: home).loadConfiguration() == config)
    #expect(try WorkspaceStorage.system(identity: .production, environment: [:], home: home).loadConfiguration() == original)
    #expect(development.historyRoot != production.historyRoot)
}
