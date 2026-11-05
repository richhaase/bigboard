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
    @Test func completeHistoryExceedsFormerRetentionCeilings() throws {
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration())
        let subject = String(repeating: "é", count: 180)
        for index in 0..<50_001 {
            let fields = ["", String(format: "%040x", index + 1), "A", "a@x", "1700000000", "", subject, "1\t0\ta.swift", "2\t1\tb.swift", "0\t0\t", "old.swift", "new.swift", ""]
            try parser.feed(Data(fields.joined(separator: "\0").utf8))
        }
        let records = try parser.finish()
        #expect(records.count == 50_001)
        #expect(records.reduce(0) { $0 + $1.files.count } == 150_003)
        #expect(records.last?.subject == subject)
        #expect(records.last?.files.last?.previousPath == "old.swift")
        let cache = HistoryScanCache(schema: 2, fingerprint: "large", scannedAt: Date(), records: records)
        #expect(try cache.encoded() != nil)
        #expect(try cache.encoded(limits: HistoryCacheLimits(cacheBytes: 16 * 1024 * 1024)) == nil)
    }
    @Test func streamingHistoryHasNoBufferedOutputCeiling() async throws {
        let directory = try directory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let log = directory.appendingPathComponent("history")
        try payload(files: ["1\t0\ta.swift"]).write(to: log)
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration())
        _ = try await GitProcess.execute(URL(fileURLWithPath: "/bin/cat"), arguments: [log.path], environment: [:], directory: directory, limit: 1, stream: { data, checkpoint in try parser.feed(data, checkpoint: checkpoint) })
        #expect(try parser.finish().count == 1)
        await #expect(throws: (any Error).self) {
            _ = try await GitProcess.execute(URL(fileURLWithPath: "/bin/cat"), arguments: [log.path], environment: [:], directory: directory, limit: 1)
        }
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
            _ = try HistoryScanCache.read(url, limits: HistoryCacheLimits(cacheBytes: 1024))
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
    @Test func oversizedEstimateSkipsJSONEncoding() throws {
        var original = cache()
        original.scannedAt = Date(timeIntervalSince1970: .infinity)
        #expect(throws: (any Error).self) { try JSONEncoder().encode(original) }
        #expect(try original.encoded(limits: HistoryCacheLimits(cacheBytes: 512)) == nil)
    }
    @Test func rejectedOrCanceledCacheWritePreservesPreviousCache() throws {
        let directory = try directory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("scan.json"), original = cache()
        try original.write(to: url)
        let before = try Data(contentsOf: url)
        var replacement = original
        replacement.records[0].subject = String(repeating: "new", count: 1000)
        #expect(try !replacement.write(to: url, limits: HistoryCacheLimits(cacheBytes: 1024)))
        #expect(try Data(contentsOf: url) == before)
        #expect(throws: CancellationError.self) { try replacement.write(to: url, checkpoint: { throw CancellationError() }) }
        #expect(try Data(contentsOf: url) == before)
    }
    @Test func scanCanReturnCompleteHistoryWithoutReplacingAnOversizedCache() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let repo = try fixture.repository()
        let original = try await GitHistory(root: fixture.root).scan(repo, configuration: WorkspaceConfiguration())
        let files = try FileManager.default.contentsOfDirectory(at: fixture.root.appendingPathComponent("scans"), includingPropertiesForKeys: nil)
        let url = try #require(files.first)
        let before = try Data(contentsOf: url)
        let result = try await GitHistory(root: fixture.root, limits: HistoryCacheLimits(cacheBytes: 1)).scan(repo, configuration: WorkspaceConfiguration())
        #expect(result.1 == original.1)
        #expect(result.0.revision == original.0.revision)
        #expect(try Data(contentsOf: url) == before)
    }
}
