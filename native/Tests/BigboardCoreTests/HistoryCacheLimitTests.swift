import Foundation
import Darwin
import Testing
@testable import Bigboard

struct HistoryCacheLimitTests {
    private func payload(id: Character = "a", author: String = "A", subject: String = "S", files: [String] = []) -> Data {
        Data((["", String(repeating: String(id), count: 40), author, "a@x", "1700000000", "", subject] + files + [""]).joined(separator: "\0").utf8)
    }
    private func terminalFailure(_ parser: GitLogParser, _ data: Data, message: String) {
        do {
            try parser.feed(data)
            Issue.record("History parsing accepted evidence beyond its memory limit.")
        } catch { #expect(error.localizedDescription == message) }
        #expect(throws: (any Error).self) { try parser.finish() }
        #expect(throws: (any Error).self) { try parser.feed(Data()) }
    }
    @Test func commitLimitAccumulatesAcrossChunks() throws {
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), limits: HistoryLimits(commits: 2))
        try parser.feed(payload())
        for byte in payload(id: "b") { try parser.feed(Data([byte])) }
        #expect(parser.records.count == 2)
        terminalFailure(parser, payload(id: "c"), message: "Git history exceeded the commit safety limit.")
        #expect(parser.records.count == 2)
    }
    @Test func fileChangeLimitIncludesExcludedFilesAndPendingRenames() throws {
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), limits: HistoryLimits(fileChanges: 2))
        try parser.feed(payload(files: ["1\t0\tvendor/generated.swift"]))
        try parser.feed(payload(id: "b", files: ["0\t0\t", "old.swift", "new.swift"]))
        #expect(parser.records.flatMap(\.files).count == 2)
        #expect(parser.records.first?.files.first?.excludedReason == "path")
        terminalFailure(parser, Data("0\t0\t\0".utf8), message: "Git history exceeded the file-change safety limit.")
        #expect(parser.records.flatMap(\.files).count == 2)
    }
    @Test func retainedPathBudgetCountsUTF8BytesAcrossChunks() throws {
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), limits: HistoryLimits(retainedStringBytes: 112))
        let path = String(repeating: "é", count: 32)
        for byte in payload(files: ["1\t0\t" + path]) { try parser.feed(Data([byte])) }
        #expect(parser.records.first?.files.first?.path == path)
        terminalFailure(parser, Data("1\t0\tx\0".utf8), message: "Git history exceeded the retained-text safety limit.")
        #expect(parser.records.first?.files.count == 1)
    }
    @Test func retainedMetadataBudgetAccumulatesAcrossCommits() throws {
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), limits: HistoryLimits(retainedStringBytes: 167))
        let subject = String(repeating: "é", count: 16)
        try parser.feed(payload(author: "α", subject: subject))
        for byte in payload(id: "b", author: "α", subject: subject) { try parser.feed(Data([byte])) }
        #expect(parser.records.count == 2)
        terminalFailure(parser, payload(id: "c", author: "α", subject: subject), message: "Git history exceeded the retained-text safety limit.")
        #expect(parser.records.count == 2)
    }
    @Test func retainedBudgetIncludesBothRenamePathsAndPreviousCommits() throws {
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), limits: HistoryLimits(retainedStringBytes: 175))
        try parser.feed(payload(files: ["1\t0\t" + String(repeating: "x", count: 16)]))
        try parser.feed(payload(id: "b", files: ["0\t0\t", String(repeating: "é", count: 16)]))
        #expect(parser.records.count == 2)
        #expect(parser.records.last?.files.isEmpty == true)
        terminalFailure(parser, Data((String(repeating: "y", count: 32) + "\0").utf8), message: "Git history exceeded the retained-text safety limit.")
        #expect(parser.records.last?.files.isEmpty == true)
    }
    @Test func oversizedTerminatedFieldFailsBeforeRetention() throws {
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration())
        try parser.feed(payload())
        terminalFailure(parser, Data(("1\t0\t" + String(repeating: "x", count: 4 * 1024 * 1024) + "\0").utf8), message: "Git history contains an oversized field.")
        #expect(parser.records.first?.files.isEmpty == true)
    }
    private func cache() -> HistoryScanCache {
        let record = Commit(id: String(repeating: "a", count: 40), repo: "o/r", author: "A", email: "a@x", date: Date(timeIntervalSince1970: 1_700_000_000), subject: "A subject\nwith \"quotes\" and é", files: [FileChange(path: "new\tname\n.swift", previousPath: "old.swift", added: 2, removed: 1)])
        return HistoryScanCache(schema: 2, fingerprint: "fingerprint", scannedAt: Date(timeIntervalSince1970: 1_700_000_001), records: [record])
    }
    private func directory() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }
    @Test func boundedCacheRoundtripPreservesCompleteEvidence() throws {
        let directory = try directory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("scan.json"), original = cache()
        #expect(try original.write(to: url))
        let decoded = try HistoryScanCache.read(url)
        #expect(decoded.schema == original.schema)
        #expect(decoded.fingerprint == original.fingerprint)
        #expect(decoded.scannedAt == original.scannedAt)
        #expect(decoded.records == original.records)
    }
    @Test func oversizedCacheIsRejectedBeforeJSONDecoding() throws {
        let directory = try directory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("scan.json")
        try Data(repeating: 120, count: 1025).write(to: url)
        do {
            _ = try HistoryScanCache.read(url, limits: HistoryLimits(cacheBytes: 1024))
            Issue.record("An oversized cache reached JSON decoding.")
        } catch { #expect(error.localizedDescription == "Git history cache exceeded the file safety limit.") }
    }
    @Test func nonregularCacheIsRejectedWithoutWaitingForAWriter() throws {
        let directory = try directory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("scan.json")
        try #require(mkfifo(url.path, mode_t(0o600)) == 0)
        #expect(throws: (any Error).self) { try HistoryScanCache.read(url) }
    }
    @Test func decodedCacheMustFitAllRetentionLimits() throws {
        let directory = try directory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("scan.json")
        var original = cache()
        original.records.append(original.records[0])
        try JSONEncoder().encode(original).write(to: url)
        for limits in [HistoryLimits(commits: 1), HistoryLimits(fileChanges: 1), HistoryLimits(retainedStringBytes: 1)] {
            #expect(throws: (any Error).self) { try HistoryScanCache.read(url, limits: limits) }
        }
    }
    @Test func oversizedEstimateSkipsJSONEncoding() throws {
        var original = cache()
        original.scannedAt = Date(timeIntervalSince1970: .infinity)
        #expect(throws: (any Error).self) { try JSONEncoder().encode(original) }
        #expect(try original.encoded(limits: HistoryLimits(cacheBytes: 512)) == nil)
    }
    @Test func rejectedOrCanceledCacheWritePreservesPreviousCache() throws {
        let directory = try directory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("scan.json"), original = cache()
        try original.write(to: url)
        let before = try Data(contentsOf: url)
        var replacement = original
        replacement.records[0].subject = String(repeating: "new", count: 1000)
        #expect(try !replacement.write(to: url, limits: HistoryLimits(cacheBytes: 1024)))
        #expect(try Data(contentsOf: url) == before)
        #expect(throws: CancellationError.self) { try replacement.write(to: url, checkpoint: { throw CancellationError() }) }
        #expect(try Data(contentsOf: url) == before)
    }
    @Test func scanLimitFailurePreservesPreviousCache() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let repo = Repository(id: "owner/repo")
        _ = try await GitHistory(root: fixture.root).scan(repo, configuration: WorkspaceConfiguration())
        let files = try FileManager.default.contentsOfDirectory(at: fixture.root.appendingPathComponent("scans"), includingPropertiesForKeys: nil)
        let url = try #require(files.first)
        let before = try Data(contentsOf: url)
        do {
            _ = try await GitHistory(root: fixture.root, limits: HistoryLimits(commits: 1)).scan(repo, configuration: WorkspaceConfiguration())
            Issue.record("A scan published incomplete history after exceeding its commit limit.")
        } catch { #expect(error.localizedDescription == "Git history exceeded the commit safety limit.") }
        #expect(try Data(contentsOf: url) == before)
    }
    @Test func scanCanReturnCompleteHistoryWithoutReplacingAnOversizedCache() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let repo = Repository(id: "owner/repo")
        let original = try await GitHistory(root: fixture.root).scan(repo, configuration: WorkspaceConfiguration())
        let files = try FileManager.default.contentsOfDirectory(at: fixture.root.appendingPathComponent("scans"), includingPropertiesForKeys: nil)
        let url = try #require(files.first)
        let before = try Data(contentsOf: url)
        let result = try await GitHistory(root: fixture.root, limits: HistoryLimits(cacheBytes: 1)).scan(repo, configuration: WorkspaceConfiguration())
        #expect(result.1 == original.1)
        #expect(result.0.revision == original.0.revision)
        #expect(try Data(contentsOf: url) == before)
    }
}
