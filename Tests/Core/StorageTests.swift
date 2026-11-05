import Foundation
import Testing
@testable import Bigboard

struct StorageTests {
    @Test func existingWorkspaceDefaultsToFifteenMinuteRefreshWithoutRewriting() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        var config = WorkspaceConfiguration(repositories: ["owner/repo"])
        config.sinceDays = 90
        try storage.saveConfiguration(config)
        let file = storage.configurationFile
        var document = try #require(JSONSerialization.jsonObject(with: Data(contentsOf: file)) as? [String: Any])
        document.removeValue(forKey: "refreshIntervalMinutes")
        let original = try JSONSerialization.data(withJSONObject: document)
        try original.write(to: file)
        #expect(try storage.loadConfiguration() == config)
        #expect(try Data(contentsOf: file) == original)
        config.refreshIntervalMinutes = 30
        try storage.saveConfiguration(config)
        #expect(try storage.loadConfiguration().refreshIntervalMinutes == 30)
    }

    @Test(arguments: [0, 5, 15, 30, 60, -1, 1, 1440])
    func refreshIntervalsAreValidatedAndPersisted(minutes: Int) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        var config = WorkspaceConfiguration()
        config.refreshIntervalMinutes = minutes
        if [0, 5, 15, 30, 60].contains(minutes) {
            try storage.saveConfiguration(config)
            #expect(try storage.loadConfiguration().refreshIntervalMinutes == minutes)
        } else {
            #expect(throws: (any Error).self) { try storage.saveConfiguration(config) }
        }
    }

    func temporary() throws -> WorkspaceStorage {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        return WorkspaceStorage(configRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    }
    @Test func freshConfigurationRoundTripsInOneFile() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        var config = try storage.loadConfiguration()
        #expect(config.repositories.isEmpty)
        #expect(!FileManager.default.fileExists(atPath: storage.configurationFile.path))
        config.repositories = ["owner/repo"]
        try storage.saveConfiguration(config)
        #expect(try storage.loadConfiguration().repositories == ["owner/repo"])
        #expect(storage.configurationFile == storage.configRoot.appendingPathComponent("config.json"))
        #expect(storage.historyRoot == storage.cacheRoot.appendingPathComponent("history"))
        #expect(try FileManager.default.contentsOfDirectory(atPath: storage.configRoot.path) == ["config.json"])
    }
    @Test func externalEditIsNotOverwritten() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        let config = try storage.loadConfiguration()
        try storage.saveConfiguration(config)
        let path = storage.configurationFile
        try Data("{}".utf8).write(to: path)
        #expect(throws: (any Error).self) { try storage.saveConfiguration(config) }
        #expect(try Data(contentsOf: path) == Data("{}".utf8))
    }
    @Test func symbolicLinksAreRejectedAndStateIsPrivate() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        let config = try storage.loadConfiguration()
        try storage.saveConfiguration(config)
        let file = storage.configurationFile
        let attributes = try FileManager.default.attributesOfItem(atPath: file.path)
        #expect((attributes[.posixPermissions] as? NSNumber)?.intValue == 0o600)
        let directory = try FileManager.default.attributesOfItem(atPath: storage.configRoot.path)
        #expect((directory[.posixPermissions] as? NSNumber)?.intValue == 0o700)
        let target = storage.configRoot.deletingLastPathComponent().appendingPathComponent("outside.json")
        try Data("private".utf8).write(to: target)
        try FileManager.default.removeItem(at: file)
        try FileManager.default.createSymbolicLink(at: file, withDestinationURL: target)
        #expect(throws: (any Error).self) { try storage.loadConfiguration() }
        #expect(throws: (any Error).self) { try storage.saveConfiguration(config) }
        #expect(try Data(contentsOf: target) == Data("private".utf8))
        let identity = storage.cacheRoot.appendingPathComponent("gh-identity.json")
        try FileManager.default.createDirectory(at: storage.cacheRoot, withIntermediateDirectories: true)
        try FileManager.default.createSymbolicLink(at: identity, withDestinationURL: target)
        #expect(throws: (any Error).self) { try storage.loadIdentity() }
        #expect(throws: (any Error).self) { try storage.saveIdentity(GitHubIdentity(login: "octo", accountID: "1")) }
        #expect(try Data(contentsOf: target) == Data("private".utf8))
    }
    @Test func malformedIdentityAndPathsRejected() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        var config = WorkspaceConfiguration()
        config.identities = ["a@b":["A@B"].identityRule]
        #expect(throws: (any Error).self) { try storage.validate(config) }
        #expect(!WorkspaceStorage.validRepository("../repo"))
        #expect(!WorkspaceStorage.validRepository("owner/repo/extra"))
    }
    @Test(arguments: ["repositories", "excludedRepositories", "groups", "workAreas"])
    func mixedCaseRepositoryReferencesRequireRepairBeforeSaving(field: String) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        let area = WorkArea(name: "Sources", paths: ["src/"])
        var original = try storage.loadConfiguration()
        original.repositories = ["owner/repo"]
        original.excludedRepositories = ["other/repo"]
        original.groups = ["Focus": ["owner/repo"]]
        original.workAreas = ["owner/repo": [area]]
        try storage.saveConfiguration(original)
        let file = storage.configurationFile
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
        #expect(throws: (any Error).self) { try storage.saveConfiguration(mixed) }
        #expect(try Data(contentsOf: file) == saved)
        let existing = try JSONEncoder().encode(mixed)
        try existing.write(to: file)
        var repaired = try storage.loadConfiguration()
        #expect(repaired == mixed)
        #expect(try Data(contentsOf: file) == existing)
        #expect(throws: (any Error).self) { try storage.saveConfiguration(repaired) }
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
        try storage.saveConfiguration(repaired)
        let reopened = WorkspaceStorage(configRoot: storage.configRoot, cacheRoot: storage.cacheRoot)
        #expect(try reopened.loadConfiguration() == original)
    }
    @Test func equivalentWorkAreaPrefixesCannotReplaceSavedSettings() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        var original = try storage.loadConfiguration()
        original.workAreas = ["owner/repo": [WorkArea(name: "Sources", paths: ["src/"])]]
        try storage.saveConfiguration(original)
        let file = storage.configurationFile
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
                #expect(throws: (any Error).self) { try storage.saveConfiguration(candidate) }
                #expect(try Data(contentsOf: file) == saved)
            }
        }
        #expect(try storage.loadConfiguration() == original)
    }
    @Test func trailingSlashAndNestedWorkAreaPrefixesRoundTripUnchanged() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        var config = try storage.loadConfiguration()
        config.workAreas = [
            "owner/repo": [WorkArea(name: "Sources", paths: ["src/"]), WorkArea(name: "Core", paths: ["src/core"])],
            "other/repo": [WorkArea(name: "Sources", paths: ["src"])]
        ]
        try storage.saveConfiguration(config)
        let loaded = try storage.loadConfiguration()
        #expect(loaded == config)
        let rules = try #require(loaded.workAreas["owner/repo"])
        #expect(Analysis.classifyWorkArea("src/main.swift", rules: rules) == "Sources")
        #expect(Analysis.classifyWorkArea("src/core/parser.swift", rules: rules) == "Core")
    }
    @Test(arguments: [".", "./", "./src", "src/.", "src/./", "src/./ui", "src//ui", "src//", "src///", "src//\u{301}ui", "src/./\u{301}ui", "./\u{301}src"])
    func noncanonicalWorkAreaPathsRequireRepairBeforeSaving(path: String) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        var original = try storage.loadConfiguration()
        original.workAreas = ["owner/repo": [WorkArea(name: "UI", paths: ["src/ui/"])]]
        try storage.saveConfiguration(original)
        let file = storage.configurationFile
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
        #expect(throws: (any Error).self) { try storage.saveConfiguration(malformed) }
        #expect(try Data(contentsOf: file) == saved)
        #expect(try storage.loadConfiguration() == original)
        let existing = try JSONEncoder().encode(malformed)
        try existing.write(to: file)
        let loaded = try storage.loadConfiguration()
        #expect(loaded == malformed)
        #expect(try Data(contentsOf: file) == existing)
        #expect(throws: (any Error).self) { try storage.saveConfiguration(loaded) }
        #expect(try Data(contentsOf: file) == existing)
        var repaired = loaded
        repaired.workAreas["owner/repo"]?[0].paths = ["src/ui/"]
        try storage.saveConfiguration(repaired)
        #expect(try storage.loadConfiguration() == original)
        #expect(Analysis.classifyWorkArea("src/ui/view.swift", rules: repaired.workAreas["owner/repo"] ?? []) == "UI")
    }
    @Test(arguments: [".github/workflows/", "src/.../ui", "src/ spaced /ui ", "src/line\nbreak", "src/back\\slash", "src/\u{301}ui", "src/\u{301}ui/", "\u{301}src/ui", "src/.\u{301}/ui", "src/..\u{301}/ui"])
    func literalWorkAreaPathComponentsRoundTripUnchanged(path: String) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        var config = try storage.loadConfiguration()
        config.workAreas = ["owner/repo": [WorkArea(name: "Literal", paths: [path])]]
        try storage.saveConfiguration(config)
        let loaded = try storage.loadConfiguration()
        #expect(loaded == config)
        let file = WorkArea.canonicalPrefix(path) + "/file.swift"
        #expect(Analysis.classifyWorkArea(file, rules: loaded.workAreas["owner/repo"] ?? []) == "Literal")
    }
    @Test(arguments: ["", "/", "/src", "..", "../src", "src/../ui", "src/..", "src/../", "src/\0ui", "/\u{301}src", "src/../\u{301}ui", "../\u{301}src", "src/\0\u{301}ui"])
    func unsafeWorkAreaPathsRemainRejectedOnLoadAndSave(path: String) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        var config = try storage.loadConfiguration()
        config.workAreas = ["owner/repo": [WorkArea(name: "Unsafe", paths: [path])]]
        #expect(throws: (any Error).self) { try storage.validate(config) }
        #expect(throws: (any Error).self) { try storage.saveConfiguration(config) }
        let file = storage.configurationFile
        try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        let existing = try JSONEncoder().encode(config)
        try existing.write(to: file)
        #expect(throws: (any Error).self) { try storage.loadConfiguration() }
        #expect(try Data(contentsOf: file) == existing)
    }
    @Test(arguments: [false, true]) func previouslySavedAmbiguousWorkAreasCanBeOpenedAndRepaired(externalEdit: Bool) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        var original = WorkspaceConfiguration()
        original.workAreas = ["owner/repo": [WorkArea(name: "First", paths: ["src"]), WorkArea(name: "Second", paths: ["src/"])]]
        let file = storage.configurationFile
        try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        let encoder = JSONEncoder(); encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        let saved = try encoder.encode(original)
        try saved.write(to: file)
        let loaded = try storage.loadConfiguration()
        #expect(loaded == original)
        #expect(try Data(contentsOf: file) == saved)
        #expect(Analysis.classifyWorkArea("src/main.swift", rules: loaded.workAreas["owner/repo"] ?? []) == "First")
        #expect(throws: (any Error).self) { try storage.saveConfiguration(loaded) }
        #expect(try Data(contentsOf: file) == saved)
        var repaired = loaded
        repaired.workAreas["owner/repo"]?[1].paths = ["tests/"]
        if externalEdit {
            var changed = original; changed.sinceDays = 90
            let external = try encoder.encode(changed)
            try external.write(to: file)
            do {
                try storage.saveConfiguration(repaired)
                Issue.record("Overwrote externally changed settings while repairing work areas")
            } catch { #expect(error.localizedDescription.contains("changed outside")) }
            #expect(try Data(contentsOf: file) == external)
            repaired = try storage.loadConfiguration()
            repaired.workAreas["owner/repo"]?[1].paths = ["tests/"]
        }
        try storage.saveConfiguration(repaired)
        let reloaded = try storage.loadConfiguration()
        #expect(reloaded == repaired)
        #expect(reloaded.sinceDays == (externalEdit ? 90 : original.sinceDays))
        #expect(reloaded.workAreas["owner/repo"]?.map(\.name) == ["First", "Second"])
        let rules = try #require(reloaded.workAreas["owner/repo"])
        #expect(Analysis.classifyWorkArea("src/main.swift", rules: rules) == "First")
        #expect(Analysis.classifyWorkArea("tests/test.swift", rules: rules) == "Second")
    }
}
private extension Array where Element == String { var identityRule: IdentityRule { IdentityRule(aliases:self) } }
