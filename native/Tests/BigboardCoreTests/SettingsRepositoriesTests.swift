import Foundation
import Testing
@testable import Bigboard

@Test func settingsRepositoriesSortByLatestPushWithoutDroppingUnavailableChoices() {
    let rows = [Repository(id: "owner/alpha", pushedAt: Date(timeIntervalSince1970: 100)), Repository(id: "owner/zeta", pushedAt: Date(timeIntervalSince1970: 200)), Repository(id: "owner/unknown")]
    let recent = SettingsRepositories.rows(available: rows, saved: ["other/saved"], sort: .recent, query: "")
    #expect(recent.map(\.id) == ["owner/zeta", "owner/alpha", "other/saved", "owner/unknown"])
    #expect(recent.first?.pushedAt == Date(timeIntervalSince1970: 200))
    #expect(SettingsRepositories.rows(available: rows, saved: ["other/saved"], sort: .name, query: "").map(\.id) == ["other/saved", "owner/alpha", "owner/unknown", "owner/zeta"])
    #expect(SettingsRepositories.rows(available: rows, saved: ["other/saved"], sort: .recent, query: "ALPHA").map(\.id) == ["owner/alpha"])
}
@Test func settingsSelectionKeepsExclusionsOutsideCurrentDiscovery() {
    var original = WorkspaceConfiguration()
    original.repositories = ["owner/old"]
    original.excludedRepositories = ["other/hidden", "owner/selected"]
    let next = SettingsRepositories.applying(["owner/selected"], available: ["owner/selected", "owner/unselected"], to: original)
    #expect(next.repositories == ["owner/selected"])
    #expect(next.excludedRepositories == ["other/hidden", "owner/unselected"])
    #expect(original.repositories == ["owner/old"])
}
@Test func settingsSelectionRepairsStoredCaseWithoutDroppingHiddenExclusions() throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    var original = WorkspaceConfiguration()
    original.repositories = ["Owner/Selected", "owner/selected"]
    original.excludedRepositories = ["Other/Hidden", "other/hidden", "OWNER/Selected"]
    original.groups = ["Focus": ["owner/selected"]]
    original.workAreas = ["owner/selected": [WorkArea(name: "Sources", paths: ["src/"])]]
    original.sinceDays = 90
    let file = try storage.accountRoot("42").appendingPathComponent("workspace.json")
    try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
    try JSONEncoder().encode(original).write(to: file)
    let loaded = try storage.loadConfiguration("42")
    #expect(loaded == original)

    let next = SettingsRepositories.applying(Set(loaded.repositories), available: ["OWNER/Selected", "Owner/Unselected", "owner/unselected"], to: loaded)
    var expected = original
    expected.repositories = ["owner/selected"]
    expected.excludedRepositories = ["other/hidden", "owner/unselected"]
    #expect(next == expected)
    try storage.saveConfiguration(next, account: "42")
    let reopened = WorkspaceStorage(supportRoot: storage.supportRoot, cacheRoot: storage.cacheRoot)
    #expect(try reopened.loadConfiguration("42") == expected)
}
