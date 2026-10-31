import Foundation
import Testing
@testable import Bigboard

struct HistoryTests {
    func payload(id: String = String(repeating: "a", count: 40), author: String = "A", email: String = "a@example.test", trailers: String = "", files: [String] = []) -> Data {
        Data((["", id, author, email, "1700000000", trailers, "A commit"] + files + [""]).joined(separator: "\0").utf8)
    }
    @Test func chunkedParsingRetainsLiteralPathsAndRenames() throws {
        let parser = GitLogParser(repo: "owner/repo", configuration: WorkspaceConfiguration())
        let bytes = payload(files: ["\n3\t2\tsrc/a\tb\nc.swift", "0\t0\t", "old\nname.swift", "new\tname.swift", "-\t-\timage.bin"])
        for byte in bytes { try parser.feed(Data([byte])) }
        let records = try parser.finish()
        #expect(records.count == 1)
        #expect(records[0].added == 3 && records[0].removed == 2)
        #expect(records[0].files.map(\.path) == ["src/a\tb\nc.swift", "new\tname.swift", "image.bin"])
        #expect(records[0].files[1].previousPath == "old\nname.swift")
        #expect(records[0].files[2].binary)
    }
    @Test func excludesGeneratedBulkAndRetainsCommitEvidence() throws {
        let attrs = HistoryRules.attributes("vendor/keep.swift -linguist-generated\n**/generated.swift linguist-generated\n")
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), attributes: attrs)
        try parser.feed(payload(files: ["\n50\t0\tvendor/drop.swift", "2\t0\tvendor/keep.swift", "3\t1\tsrc/generated.swift", "1999\t1\tdata.json", "1\t0\tsource.swift"]))
        let commit = try #require(parser.finish().first)
        #expect(commit.added == 3 && commit.removed == 0)
        #expect(commit.files.map(\.excludedReason) == ["path", "", "attribute", "bulk", ""])
        #expect(Analysis.excludedLines([commit]) == ["path": 50, "attribute": 4, "bulk": 2000])
        #expect(Analysis.scoped([commit], scope: ActivityScope(repo: "o/r", path: "vendor", isFile: false)).first?.files.count == 1)
    }
    @Test func allFilesOverridesEveryExclusion() throws {
        var config = WorkspaceConfiguration(); config.allFiles = true
        let parser = GitLogParser(repo: "o/r", configuration: config, attributes: HistoryRules.attributes("* linguist-generated"))
        try parser.feed(payload(files: ["\n2000\t2\tvendor/data.json"]))
        let commit = try #require(parser.finish().first)
        #expect(commit.added == 2000 && commit.removed == 2)
        #expect(commit.files[0].excludedReason.isEmpty)
    }
    @Test func identityAliasesAndCoauthorAI() throws {
        var config = WorkspaceConfiguration()
        config.identities = ["canonical@example.test": IdentityRule(aliases: ["alias@example.test"], name: "Canonical Name")]
        let parser = GitLogParser(repo: "o/r", configuration: config)
        try parser.feed(payload(email: "ALIAS@example.test", trailers: "Human <other@example.test>\u{1f}Claude <noreply@anthropic.com>", files: ["\n1\t0\tfile"]))
        let commit = try #require(parser.finish().first)
        #expect(commit.email == "canonical@example.test")
        #expect(commit.author == "Canonical Name")
        #expect(commit.ai && !commit.bot)
        #expect(HistoryRules.isBot(name: "Pre-Commit-CI", email: "", extra: []))
        #expect(!HistoryRules.isBot(name: "AI researcher", email: "human@openai.com", extra: []))
        #expect(!HistoryRules.isAI("human@anthropic.com", extra: []))
        #expect(HistoryRules.isAI("123+copilot-swe-agent[bot]@users.noreply.github.com", extra: []))
    }
    @Test func malformedLogFailsWithoutPartialSuccess() throws {
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration())
        #expect(throws: (any Error).self) { try parser.feed(Data("\0abcdef\0".utf8)) }
    }
    @Test func rejectsTruncatedRename() throws {
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration())
        try parser.feed(payload(files: ["\n1\t1\t", "old"]))
        #expect(throws: (any Error).self) { try parser.finish() }
    }
    @Test func authFailuresAreAuthoritativeOnlyWhenEvidenced() {
        #expect(GitProcess.authenticatedFailure("fatal: Authentication failed for https://github.com/o/r")?.isAccessDenied == true)
        #expect(GitProcess.authenticatedFailure("remote: Repository not found.")?.isAccessDenied == true)
        #expect(GitProcess.authenticatedFailure("fatal: unable to access: The requested URL returned error: 403")?.isAccessDenied == true)
        #expect(GitProcess.authenticatedFailure("rate limit exceeded; HTTP 403")?.isAccessDenied == false)
        #expect(GitProcess.authenticatedFailure("Could not resolve host: github.com") == nil)
    }
    @Test func scansOwnedBareCacheAndReusesOnlyMatchingInputs() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let history = GitHistory(root: fixture.root)
        let repo = Repository(id: "owner/repo")
        let (scanned, records) = try await history.scan(repo, configuration: WorkspaceConfiguration())
        #expect(records.count == 2)
        #expect(Set(records.map(\.author)) == ["Mapped Author"])
        #expect(records.contains { $0.ai })
        #expect(records.flatMap(\.files).contains { $0.path == "new\tname\n.swift" && $0.previousPath == "source.swift" })
        #expect(records.flatMap(\.files).contains { $0.path == "vendor/generated.swift" && $0.excludedReason == "path" })
        let second = try await history.scan(repo, configuration: WorkspaceConfiguration())
        #expect(second.0.scannedAt == scanned.scannedAt)
        #expect(second.1 == records)
        var config = WorkspaceConfiguration(); config.allFiles = true
        let all = try await history.scan(scanned, configuration: config)
        #expect(Analysis.totals(all.1).added > Analysis.totals(records).added)
        let url = try await history.pathURL(repo: scanned, path: "new\tname\n.swift")
        #expect(url.absoluteString.contains(scanned.revision))
        #expect(url.absoluteString.contains("%09") && url.absoluteString.contains("%0A"))
        let inventory = try await history.cachedRepositories()
        #expect(inventory.map(\.id) == [repo.id])
        try await history.remove(repo.id)
        #expect(try await history.cachedRepositories().isEmpty)
    }
    @Test func separateHistoryInstancesCoordinateSharedCache() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let first = GitHistory(root: fixture.root), second = GitHistory(root: fixture.root)
        async let a = first.scan(Repository(id: "owner/repo"), configuration: WorkspaceConfiguration())
        async let b = second.scan(Repository(id: "owner/repo"), configuration: WorkspaceConfiguration())
        let results = try await (a, b)
        #expect(results.0.1 == results.1.1)
        #expect(results.0.0.scannedAt == results.1.0.scannedAt)
    }
    @Test func externalMailmapChangeInvalidatesCachedScan() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let map = fixture.base.appendingPathComponent("override-mailmap")
        try Data("First <first@test> Original Author <old@example.test>\n".utf8).write(to: map)
        _ = try fixture.git(["--git-dir=" + fixture.bare.path, "config", "mailmap.file", "../../../../override-mailmap"])
        let history = GitHistory(root: fixture.root)
        let first = try await history.scan(Repository(id: "owner/repo"), configuration: WorkspaceConfiguration())
        #expect(first.1.first?.email == "first@test")
        try Data("Second <second@test> Original Author <old@example.test>\n".utf8).write(to: map)
        let second = try await history.scan(first.0, configuration: WorkspaceConfiguration())
        #expect(second.1.first?.email == "second@test")
    }
    @Test func unavailableFingerprintDoesNotPreventValidHistoryScan() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let attributes = fixture.base.appendingPathComponent("large-attributes")
        try Data(("#" + String(repeating: " ", count: 17 * 1024 * 1024) + "\n").utf8).write(to: attributes)
        _ = try fixture.git(["--git-dir=" + fixture.bare.path, "config", "core.attributesFile", attributes.path])
        let history = GitHistory(root: fixture.root)
        let result = try await history.scan(Repository(id: "owner/repo"), configuration: WorkspaceConfiguration())
        #expect(result.1.count == 2)
        #expect(!FileManager.default.fileExists(atPath: fixture.root.appendingPathComponent("scans").path))
    }
    @Test func ownershipAndSymlinksProtectDeveloperFiles() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let history = GitHistory(root: fixture.root)
        try FileManager.default.removeItem(at: fixture.bare.appendingPathComponent("bigboard-cache"))
        await #expect(throws: (any Error).self) { try await history.remove("owner/repo") }
        #expect(FileManager.default.fileExists(atPath: fixture.bare.path))
        try Data("owner/repo\n".utf8).write(to: fixture.bare.appendingPathComponent("bigboard-cache"))
        try FileManager.default.createSymbolicLink(at: fixture.bare.appendingPathComponent("outside"), withDestinationURL: fixture.checkout)
        await #expect(throws: (any Error).self) { try await history.scan(Repository(id: "owner/repo"), configuration: WorkspaceConfiguration()) }
        #expect(FileManager.default.fileExists(atPath: fixture.checkout.appendingPathComponent(".git").path))
    }
    @Test func unexpectedOriginIsNeverScannedOrDeleted() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        _ = try fixture.git(["--git-dir=" + fixture.bare.path, "config", "remote.origin.url", "https://example.test/unowned.git"])
        let history = GitHistory(root: fixture.root)
        await #expect(throws: (any Error).self) { try await history.scan(Repository(id: "owner/repo"), configuration: WorkspaceConfiguration()) }
        await #expect(throws: (any Error).self) { try await history.remove("owner/repo") }
        #expect(FileManager.default.fileExists(atPath: fixture.bare.path))
    }
    private var processFamilyFixture: String {
        """
        import os, signal, sys, time
        child = os.fork()
        if child == 0:
            signal.signal(signal.SIGTERM, signal.SIG_IGN)
            with open(sys.argv[1], "w") as ready:
                ready.write("ready")
        time.sleep(20)
        """
    }
    @Test func cancellationStopsDescendantsHoldingOutputPipes() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let ready = directory.appendingPathComponent("ready")
        let task = Task { try await GitProcess.execute(URL(fileURLWithPath: "/usr/bin/python3"), arguments: ["-c", processFamilyFixture, ready.path], environment: ["PATH": "/usr/bin:/bin"], directory: directory, timeout: 5) }
        defer { task.cancel() }
        let deadline = Date().addingTimeInterval(3)
        while !FileManager.default.fileExists(atPath: ready.path) && Date() < deadline { try await Task.sleep(for: .milliseconds(10)) }
        #expect(FileManager.default.fileExists(atPath: ready.path))
        let start = ContinuousClock.now
        task.cancel()
        await #expect(throws: CancellationError.self) { try await task.value }
        #expect(start.duration(to: .now) < .seconds(3))
    }
    @Test func timeoutStopsDescendantsHoldingOutputPipes() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let start = ContinuousClock.now
        do {
            _ = try await GitProcess.execute(URL(fileURLWithPath: "/usr/bin/python3"), arguments: ["-c", processFamilyFixture, directory.appendingPathComponent("ready").path], environment: ["PATH": "/usr/bin:/bin"], directory: directory, timeout: 0.5)
            Issue.record("A hung Git process family did not time out.")
        } catch { #expect(error.localizedDescription.contains("timeout")) }
        #expect(start.duration(to: .now) < .seconds(3))
    }
    @Test func canceledScanDoesNotPublishParsedCache() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let history = GitHistory(root: fixture.root)
        let task = Task { try await history.scan(Repository(id: "owner/repo"), configuration: WorkspaceConfiguration()) }
        task.cancel()
        await #expect(throws: CancellationError.self) { try await task.value }
        #expect(!FileManager.default.fileExists(atPath: fixture.root.appendingPathComponent("scans").path))
    }
    @Test func boundedGitOutput() async throws {
        await #expect(throws: (any Error).self) { try await GitProcess.run(["--version"], limit: 1) }
    }
}

struct HistoryFixture {
    let base: URL
    let root: URL
    let checkout: URL
    let bare: URL
    init() throws {
        base = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        root = base.appendingPathComponent("cache")
        checkout = base.appendingPathComponent("checkout")
        bare = root.appendingPathComponent("github.com/owner/repo.git")
        try FileManager.default.createDirectory(at: checkout, withIntermediateDirectories: true)
        _ = try git(["init", "--quiet", "--initial-branch=main", checkout.path])
        _ = try git(["-C", checkout.path, "config", "user.name", "Original Author"])
        _ = try git(["-C", checkout.path, "config", "user.email", "old@example.test"])
        try write("source.swift", "one\ntwo\nthree\n")
        try write("vendor/generated.swift", "generated\n")
        try write(".mailmap", "Mapped Author <mapped@example.test> Original Author <old@example.test>\n")
        _ = try git(["-C", checkout.path, "add", "."])
        _ = try git(["-C", checkout.path, "commit", "--quiet", "-m", "Initial\n\nCo-authored-by: Claude <noreply@anthropic.com>"])
        _ = try git(["-C", checkout.path, "mv", "source.swift", "new\tname\n.swift"])
        _ = try git(["-C", checkout.path, "commit", "--quiet", "-m", "Rename"])
        _ = try git(["-C", checkout.path, "checkout", "--quiet", "-b", "feature"])
        try write("feature.swift", "feature only\n")
        _ = try git(["-C", checkout.path, "add", "."])
        _ = try git(["-C", checkout.path, "commit", "--quiet", "-m", "Feature"])
        _ = try git(["-C", checkout.path, "checkout", "--quiet", "main"])
        try FileManager.default.createDirectory(at: bare.deletingLastPathComponent(), withIntermediateDirectories: true)
        _ = try git(["clone", "--quiet", "--bare", "--single-branch", "--branch", "main", checkout.path, bare.path])
        _ = try git(["--git-dir=" + bare.path, "config", "remote.origin.url", "https://github.com/owner/repo.git"])
        try Data("owner/repo\n".utf8).write(to: bare.appendingPathComponent("bigboard-cache"))
    }
    func write(_ path: String, _ contents: String) throws {
        let url = checkout.appendingPathComponent(path)
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data(contents.utf8).write(to: url)
    }
    func git(_ arguments: [String]) throws -> String {
        let process = Process(), pipe = Pipe()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        process.arguments = ["-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false"] + arguments
        process.environment = ["PATH": "/usr/bin:/bin", "HOME": base.path, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_AUTHOR_DATE": "2026-10-01T12:00:00Z", "GIT_COMMITTER_DATE": "2026-10-01T12:00:00Z"]
        process.standardOutput = pipe; process.standardError = pipe; process.standardInput = FileHandle.nullDevice
        try process.run()
        let bytes = pipe.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        let text = String(decoding: bytes, as: UTF8.self)
        guard process.terminationStatus == 0 else { throw BigboardError.message(text) }
        return text.trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
