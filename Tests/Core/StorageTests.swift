import Foundation
import Testing
@testable import Bigboard

struct StorageTests {
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
    @Test func unreadableSettingsNameTheFile() throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        try FileManager.default.createDirectory(at: storage.configRoot, withIntermediateDirectories: true)
        try Data(#"{"mode":"personal","repositories":[]}"#.utf8).write(to: storage.configurationFile)
        #expect { try storage.loadConfiguration() } throws: {
            $0.localizedDescription.contains(storage.configurationFile.path) && $0.localizedDescription.contains("delete it")
        }
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
    @Test(arguments: ["repositories", "groups", "workAreas"])
    func mixedCaseRepositoryReferencesAreRejected(field: String) throws {
        let storage = try temporary()
        defer { try? FileManager.default.removeItem(at: storage.configRoot.deletingLastPathComponent()) }
        let area = WorkArea(name: "Sources", paths: ["src/"])
        var original = WorkspaceConfiguration(repositories: ["owner/repo"])
        original.groups = ["Focus": ["owner/repo"]]
        original.workAreas = ["owner/repo": [area]]
        try storage.saveConfiguration(original)
        let file = storage.configurationFile
        let saved = try Data(contentsOf: file)
        var mixed = original
        switch field {
        case "repositories": mixed.repositories = ["Owner/Repo"]
        case "groups": mixed.groups["Focus"] = ["Owner/Repo", "owner/repo"]
        default: mixed.workAreas = ["Owner/Repo": [area]]
        }
        #expect { try storage.saveConfiguration(mixed) } throws: { $0.localizedDescription.contains("lowercase") }
        #expect(try Data(contentsOf: file) == saved)
        try JSONEncoder().encode(mixed).write(to: file)
        #expect { try storage.loadConfiguration() } throws: { $0.localizedDescription.contains("lowercase") }
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
    func noncanonicalWorkAreaPathsAreRejected(path: String) throws {
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
        try JSONEncoder().encode(malformed).write(to: file)
        #expect(throws: (any Error).self) { try storage.loadConfiguration() }
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
}
private extension Array where Element == String { var identityRule: IdentityRule { IdentityRule(aliases:self) } }
