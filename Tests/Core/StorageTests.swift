import Foundation
import Testing
@testable import Bigboard

struct StorageTests {
    @Test func existingWorkspaceDefaultsToFifteenMinuteRefreshWithoutRewriting() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        var config = WorkspaceConfiguration(repositories: ["owner/repo"])
        config.sinceDays = 90
        try storage.saveConfiguration(config, account: "42")
        let file = try storage.accountRoot("42").appendingPathComponent("workspace.json")
        var document = try #require(JSONSerialization.jsonObject(with: Data(contentsOf: file)) as? [String: Any])
        document.removeValue(forKey: "refreshIntervalMinutes")
        let original = try JSONSerialization.data(withJSONObject: document)
        try original.write(to: file)
        #expect(try storage.loadConfiguration("42") == config)
        #expect(try Data(contentsOf: file) == original)
        config.refreshIntervalMinutes = 30
        try storage.saveConfiguration(config, account: "42")
        #expect(try storage.loadConfiguration("42").refreshIntervalMinutes == 30)
        #expect(try storage.loadConfiguration("43").refreshIntervalMinutes == 15)
    }

    @Test(arguments: [0, 5, 15, 30, 60, -1, 1, 1440])
    func refreshIntervalsAreValidatedAndPersisted(minutes: Int) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        var config = WorkspaceConfiguration()
        config.refreshIntervalMinutes = minutes
        if [0, 5, 15, 30, 60].contains(minutes) {
            try storage.saveConfiguration(config, account: "42")
            #expect(try storage.loadConfiguration("42").refreshIntervalMinutes == minutes)
        } else {
            #expect(throws: (any Error).self) { try storage.saveConfiguration(config, account: "42") }
        }
    }

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
    @Test(arguments: ["repositories", "excludedRepositories", "groups", "workAreas"])
    func mixedCaseRepositoryReferencesRequireRepairBeforeSaving(field: String) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        let area = WorkArea(name: "Sources", paths: ["src/"])
        var original = try storage.loadConfiguration("42")
        original.repositories = ["owner/repo"]
        original.excludedRepositories = ["other/repo"]
        original.groups = ["Focus": ["owner/repo"]]
        original.workAreas = ["owner/repo": [area]]
        try storage.saveConfiguration(original, account: "42")
        try storage.saveConfiguration(original, account: "43")
        let file = try storage.accountRoot("42").appendingPathComponent("workspace.json")
        let saved = try Data(contentsOf: file)
        var mixed = original
        switch field {
        case "repositories": mixed.repositories = ["Owner/Repo"]
        case "excludedRepositories": mixed.excludedRepositories = ["Other/Repo"]
        case "groups": mixed.groups["Focus"] = ["Owner/Repo", "owner/repo"]
        default: mixed.workAreas = ["Owner/Repo": [area]]
        }
        do {
            try storage.validate(mixed)
            Issue.record("Accepted a mixed-case repository reference")
        } catch {
            #expect(error.localizedDescription.contains("lowercase"))
        }
        #expect(throws: (any Error).self) { try storage.saveConfiguration(mixed, account: "42") }
        #expect(try Data(contentsOf: file) == saved)
        let existing = try JSONEncoder().encode(mixed)
        try existing.write(to: file)
        var repaired = try storage.loadConfiguration("42")
        #expect(repaired == mixed)
        #expect(try Data(contentsOf: file) == existing)
        #expect(throws: (any Error).self) { try storage.saveConfiguration(repaired, account: "42") }
        #expect(try Data(contentsOf: file) == existing)
        switch field {
        case "repositories", "excludedRepositories":
            repaired = SettingsRepositories.applying(Set(repaired.repositories), available: [], to: repaired)
        case "groups":
            var editor = SettingsGroupEditor()
            editor.edit(name: "Focus", repositories: repaired.groups["Focus"]!)
            let didSave = editor.save(to: &repaired.groups)
            #expect(didSave)
        default:
            var editor = SettingsWorkAreaEditor()
            editor.edit(repository: "Owner/Repo", area: area)
            let didSave = editor.save(to: &repaired.workAreas)
            #expect(didSave)
        }
        try storage.saveConfiguration(repaired, account: "42")
        let reopened = WorkspaceStorage(supportRoot: storage.supportRoot, cacheRoot: storage.cacheRoot)
        #expect(try reopened.loadConfiguration("42") == original)
        #expect(try reopened.loadConfiguration("43") == original)
    }
    @Test func equivalentWorkAreaPrefixesCannotReplaceSavedSettings() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        var original = try storage.loadConfiguration("42")
        original.workAreas = ["owner/repo": [WorkArea(name: "Sources", paths: ["src/"])]]
        try storage.saveConfiguration(original, account: "42")
        let file = try storage.accountRoot("42").appendingPathComponent("workspace.json")
        let saved = try Data(contentsOf: file)
        for paths in [["src", "src/"], ["src/", "src"]] {
            for sameArea in [false, true] {
                var candidate = original
                candidate.workAreas["owner/repo"] = sameArea ? [WorkArea(name: "Sources", paths: paths)] : [WorkArea(name: "First", paths: [paths[0]]), WorkArea(name: "Second", paths: [paths[1]])]
                do {
                    try storage.validate(candidate)
                    Issue.record("Accepted equivalent work-area prefixes")
                } catch {
                    #expect(error.localizedDescription.contains("owner/repo"))
                    #expect(error.localizedDescription.contains("src"))
                    #expect(error.localizedDescription.contains("trailing slash"))
                }
                #expect(throws: (any Error).self) { try storage.saveConfiguration(candidate, account: "42") }
                #expect(try Data(contentsOf: file) == saved)
            }
        }
        #expect(try storage.loadConfiguration("42") == original)
    }
    @Test func trailingSlashAndNestedWorkAreaPrefixesRoundTripUnchanged() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        var config = try storage.loadConfiguration("42")
        config.workAreas = [
            "owner/repo": [WorkArea(name: "Sources", paths: ["src/"]), WorkArea(name: "Core", paths: ["src/core"])],
            "other/repo": [WorkArea(name: "Sources", paths: ["src"])]
        ]
        try storage.saveConfiguration(config, account: "42")
        let loaded = try storage.loadConfiguration("42")
        #expect(loaded == config)
        let rules = try #require(loaded.workAreas["owner/repo"])
        #expect(Analysis.classifyWorkArea("src/main.swift", rules: rules) == "Sources")
        #expect(Analysis.classifyWorkArea("src/core/parser.swift", rules: rules) == "Core")
    }
    @Test(arguments: [".", "./", "./src", "src/.", "src/./", "src/./ui", "src//ui", "src//", "src///", "src//\u{301}ui", "src/./\u{301}ui", "./\u{301}src"])
    func noncanonicalWorkAreaPathsRequireRepairBeforeSaving(path: String) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        var original = try storage.loadConfiguration("42")
        original.workAreas = ["owner/repo": [WorkArea(name: "UI", paths: ["src/ui/"])]]
        try storage.saveConfiguration(original, account: "42")
        let file = try storage.accountRoot("42").appendingPathComponent("workspace.json")
        let saved = try Data(contentsOf: file)
        var malformed = original
        malformed.workAreas["owner/repo"]?[0].paths = [path]
        do {
            try storage.validate(malformed)
            Issue.record("Accepted noncanonical work-area path")
        } catch {
            #expect(error.localizedDescription.contains("owner/repo"))
            #expect(error.localizedDescription.contains(path))
            #expect(error.localizedDescription.contains("components"))
        }
        #expect(throws: (any Error).self) { try storage.saveConfiguration(malformed, account: "42") }
        #expect(try Data(contentsOf: file) == saved)
        #expect(try storage.loadConfiguration("42") == original)
        let existing = try JSONEncoder().encode(malformed)
        try existing.write(to: file)
        let loaded = try storage.loadConfiguration("42")
        #expect(loaded == malformed)
        #expect(try Data(contentsOf: file) == existing)
        #expect(throws: (any Error).self) { try storage.saveConfiguration(loaded, account: "42") }
        #expect(try Data(contentsOf: file) == existing)
        var repaired = loaded
        repaired.workAreas["owner/repo"]?[0].paths = ["src/ui/"]
        try storage.saveConfiguration(repaired, account: "42")
        #expect(try storage.loadConfiguration("42") == original)
        #expect(Analysis.classifyWorkArea("src/ui/view.swift", rules: repaired.workAreas["owner/repo"] ?? []) == "UI")
    }
    @Test(arguments: [".github/workflows/", "src/.../ui", "src/ spaced /ui ", "src/line\nbreak", "src/back\\slash", "src/\u{301}ui", "src/\u{301}ui/", "\u{301}src/ui", "src/.\u{301}/ui", "src/..\u{301}/ui"])
    func literalWorkAreaPathComponentsRoundTripUnchanged(path: String) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        var config = try storage.loadConfiguration("42")
        config.workAreas = ["owner/repo": [WorkArea(name: "Literal", paths: [path])]]
        try storage.saveConfiguration(config, account: "42")
        let loaded = try storage.loadConfiguration("42")
        #expect(loaded == config)
        let file = WorkArea.canonicalPrefix(path) + "/file.swift"
        #expect(Analysis.classifyWorkArea(file, rules: loaded.workAreas["owner/repo"] ?? []) == "Literal")
    }
    @Test(arguments: ["", "/", "/src", "..", "../src", "src/../ui", "src/..", "src/../", "src/\0ui", "/\u{301}src", "src/../\u{301}ui", "../\u{301}src", "src/\0\u{301}ui"])
    func unsafeWorkAreaPathsRemainRejectedOnLoadAndSave(path: String) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        var config = try storage.loadConfiguration("42")
        config.workAreas = ["owner/repo": [WorkArea(name: "Unsafe", paths: [path])]]
        #expect(throws: (any Error).self) { try storage.validate(config) }
        #expect(throws: (any Error).self) { try storage.saveConfiguration(config, account: "42") }
        let file = try storage.accountRoot("42").appendingPathComponent("workspace.json")
        try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        let existing = try JSONEncoder().encode(config)
        try existing.write(to: file)
        #expect(throws: (any Error).self) { try storage.loadConfiguration("42") }
        #expect(try Data(contentsOf: file) == existing)
    }
    @Test(arguments: [false, true]) func previouslySavedAmbiguousWorkAreasCanBeOpenedAndRepaired(externalEdit: Bool) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.supportRoot.deletingLastPathComponent()) }
        var original = WorkspaceConfiguration()
        original.workAreas = ["owner/repo": [WorkArea(name: "First", paths: ["src"]), WorkArea(name: "Second", paths: ["src/"])]]
        let file = try storage.accountRoot("42").appendingPathComponent("workspace.json")
        try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        let encoder = JSONEncoder(); encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        let saved = try encoder.encode(original)
        try saved.write(to: file)
        let loaded = try storage.loadConfiguration("42")
        #expect(loaded == original)
        #expect(try Data(contentsOf: file) == saved)
        #expect(Analysis.classifyWorkArea("src/main.swift", rules: loaded.workAreas["owner/repo"] ?? []) == "First")
        #expect(throws: (any Error).self) { try storage.saveConfiguration(loaded, account: "42") }
        #expect(try Data(contentsOf: file) == saved)
        var repaired = loaded
        repaired.workAreas["owner/repo"]?[1].paths = ["tests/"]
        if externalEdit {
            var changed = original; changed.sinceDays = 90
            let external = try encoder.encode(changed)
            try external.write(to: file)
            do {
                try storage.saveConfiguration(repaired, account: "42")
                Issue.record("Overwrote externally changed settings while repairing work areas")
            } catch { #expect(error.localizedDescription.contains("changed outside")) }
            #expect(try Data(contentsOf: file) == external)
            repaired = try storage.loadConfiguration("42")
            repaired.workAreas["owner/repo"]?[1].paths = ["tests/"]
        }
        try storage.saveConfiguration(repaired, account: "42")
        let reloaded = try storage.loadConfiguration("42")
        #expect(reloaded == repaired)
        #expect(reloaded.sinceDays == (externalEdit ? 90 : original.sinceDays))
        #expect(reloaded.workAreas["owner/repo"]?.map(\.name) == ["First", "Second"])
        let rules = try #require(reloaded.workAreas["owner/repo"])
        #expect(Analysis.classifyWorkArea("src/main.swift", rules: rules) == "First")
        #expect(Analysis.classifyWorkArea("tests/test.swift", rules: rules) == "Second")
    }
}
private extension Array where Element == String { var identityRule: IdentityRule { IdentityRule(aliases:self) } }
