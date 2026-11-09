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
        let attrs = try HistoryRules.attributes("vendor/keep.swift -linguist-generated\n**/generated.swift linguist-generated\n")
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), attributes: attrs)
        try parser.feed(payload(files: ["\n50\t0\tvendor/drop.swift", "2\t0\tvendor/keep.swift", "3\t1\tsrc/generated.swift", "1999\t1\tdata.json", "1\t0\tsource.swift", "12\t0\ttests/fixtures/user.json", "4\t0\tapp/__snapshots__/view.yaml", "6\t0\tfixtures/helper.swift", "7\t0\tyarn.lock", "5\t0\tconfig/fixtures.json"]))
        let commit = try #require(parser.finish().first)
        #expect(commit.added == 14 && commit.removed == 0)
        #expect(commit.files.map(\.excludedReason) == ["vendored", "", "generated", "bulk", "", "fixture", "fixture", "", "generated", ""])
        #expect(Analysis.excludedLines([commit]) == ["vendored": 50, "generated": 11, "bulk": 2000, "fixture": 16])
        #expect(Analysis.scoped([commit], scope: ActivityScope(repo: "o/r", path: "vendor", isFile: false)).first?.files.count == 1)
    }
    @Test func allFilesOverridesEveryExclusion() throws {
        var config = WorkspaceConfiguration(); config.allFiles = true
        let parser = GitLogParser(repo: "o/r", configuration: config, attributes: try HistoryRules.attributes("* linguist-generated"))
        try parser.feed(payload(files: ["\n2000\t2\tvendor/data.json"]))
        let commit = try #require(parser.finish().first)
        #expect(commit.added == 2000 && commit.removed == 2)
        #expect(commit.files[0].excludedReason.isEmpty)
    }
    @Test func attributeWildcardsPreservePathAndBasenameSemantics() throws {
        for path in ["generated.swift", "src/generated.swift", "src/deep/generated.swift"] {
            #expect(try HistoryRules.pathMatches("**/generated.swift", path))
            #expect(try HistoryRules.pathMatches("*.swift", path))
        }
        #expect(try HistoryRules.pathMatches("src/**/generated.swift", "src/generated.swift"))
        #expect(try HistoryRules.pathMatches("src/**/generated.swift", "src/a/b/generated.swift"))
        #expect(try HistoryRules.pathMatches("src/**", "src"))
        #expect(try HistoryRules.pathMatches("src/*/file?.[ch]", "src/a/file1.c"))
        #expect(try !HistoryRules.pathMatches("src/*/file?.[ch]", "src/a/b/file1.c"))
        #expect(try !HistoryRules.pathMatches("/generated.swift", "src/generated.swift"))
        #expect(try !HistoryRules.pathMatches("src/**/generated.swift", "other/src/generated.swift"))
        #expect(try !HistoryRules.pathMatches("src/**/generated.swift", "src/generated.swift/extra"))
    }
    @Test func repeatedRecursiveAttributeWildcardsFinishWithExactMatches() throws {
        let prefix = Array(repeating: "**", count: 32).joined(separator: "/")
        let directory = Array(repeating: "directory", count: 32).joined(separator: "/")
        let matching = directory + "/generated.swift"
        let nonmatching = directory + "/source.swift"
        #expect(try HistoryRules.pathMatches(prefix + "/generated.swift", matching))
        #expect(try !HistoryRules.pathMatches(prefix + "/generated.swift", nonmatching))
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), attributes: try HistoryRules.attributes(prefix + "/generated.swift linguist-generated"))
        try parser.feed(payload(files: ["\n3\t0\t" + matching, "2\t0\t" + nonmatching]))
        let commit = try #require(parser.finish().first)
        #expect(commit.added == 2)
        #expect(commit.files.map(\.excludedReason) == ["generated", ""])
    }
    @Test func oversizedAttributeMatchingFailsHistoryParsing() throws {
        let pattern = Array(repeating: "**", count: 1024).joined(separator: "/") + "/generated.swift"
        let path = Array(repeating: "directory", count: 1024).joined(separator: "/") + "/source.swift"
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), attributes: try HistoryRules.attributes(pattern + " linguist-generated"))
        do {
            try parser.feed(payload(files: ["\n3\t0\t" + path]))
            Issue.record("An oversized attribute match did not stop history parsing.")
        } catch { #expect(error.localizedDescription == "Repository attribute matching exceeded the safety limit.") }
        #expect(throws: (any Error).self) { try HistoryRules.pathMatches(String(repeating: "*", count: 64 * 1024 + 1), "source.swift") }
        #expect(throws: (any Error).self) { try HistoryRules.pathMatches("**/source.swift", String(repeating: "a", count: 64 * 1024 + 1)) }
        #expect(throws: (any Error).self) { try HistoryRules.pathMatches(String(repeating: "a", count: 4097), "source.swift") }
        #expect(throws: (any Error).self) { try HistoryRules.pathMatches("**/source.swift", String(repeating: "a", count: 4097)) }
    }
    @Test func attributeInputLimitsRejectOversizedBytesAndRules() throws {
        let contents = "é.swift linguist-generated\nsource.swift -linguist-generated\n"
        let rules = try HistoryRules.attributes(contents, byteLimit: contents.utf8.count, ruleLimit: 2)
        #expect(rules.map(\.pattern) == ["é.swift", "source.swift"])
        #expect(rules.map(\.generated) == [true, false])
        #expect(throws: (any Error).self) { try HistoryRules.attributes(contents, byteLimit: contents.utf8.count - 1, ruleLimit: 2) }
        #expect(throws: (any Error).self) { try HistoryRules.attributes(contents, byteLimit: contents.utf8.count, ruleLimit: 1) }
    }
    @Test func attributeMatchingBudgetAccumulatesAcrossFilesAndFeedChunks() throws {
        let attributes = try HistoryRules.attributes("*.gen linguist-generated")
        for path in ["a.swift", "b.swift"] {
            let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), attributes: attributes, matchingWorkLimit: 20)
            try parser.feed(payload(files: ["1\t0\t" + path]))
            #expect(try parser.finish().first?.added == 1)
        }
        for splitChunks in [false, true] {
            let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), attributes: attributes, matchingWorkLimit: 20)
            do {
                if splitChunks {
                    try parser.feed(payload(files: ["1\t0\ta.swift"]))
                    #expect(parser.records.first?.files.map(\.path) == ["a.swift"])
                    try parser.feed(Data("1\t0\tb.swift\0".utf8))
                } else { try parser.feed(payload(files: ["1\t0\ta.swift", "1\t0\tb.swift"])) }
                Issue.record("Attribute matching reset its work budget before the second file.")
            } catch { #expect(error.localizedDescription == "Repository attribute matching exceeded the scan work limit.") }
            #expect(throws: (any Error).self) { try parser.finish() }
            #expect(throws: (any Error).self) { try parser.feed(Data()) }
        }
    }
    @Test func attributeBudgetFailurePropagatesThroughGitStream() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let log = directory.appendingPathComponent("history")
        try payload(files: ["1\t0\ta.swift", "1\t0\tb.swift"]).write(to: log)
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), attributes: try HistoryRules.attributes("*.gen linguist-generated"), matchingWorkLimit: 20)
        do {
            _ = try await GitProcess.execute(URL(fileURLWithPath: "/bin/cat"), arguments: [log.path], environment: ["PATH": "/usr/bin:/bin"], directory: directory, stream: { data, checkpoint in try parser.feed(data, checkpoint: checkpoint) })
            Issue.record("Git streaming returned success after its parser exhausted the matching budget.")
        } catch { #expect(error.localizedDescription == "Repository attribute matching exceeded the scan work limit.") }
        #expect(throws: (any Error).self) { try parser.finish() }
    }
    @Test func cachedAttributeDecisionsRetainPerCommitBulkExclusions() throws {
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), attributes: try HistoryRules.attributes("*.gen linguist-generated"), matchingWorkLimit: 20)
        try parser.feed(payload(files: ["1\t0\tdata.json"]))
        try parser.feed(payload(id: String(repeating: "b", count: 40), files: ["2000\t0\tdata.json"]))
        let records = try parser.finish()
        #expect(records.map(\.added) == [1, 0])
        #expect(records.flatMap(\.files).map(\.excludedReason) == ["", "bulk"])
    }
    @Test func cachedAttributeDecisionsKeepLiteralUnicodePathBytes() throws {
        let composed = "\u{e9}.swift", decomposed = "e\u{301}.swift"
        let attributes = try HistoryRules.attributes(composed + " linguist-generated")
        for paths in [[composed, decomposed], [decomposed, composed]] {
            let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), attributes: attributes)
            try parser.feed(payload(files: paths.map { "1\t0\t" + $0 }))
            let record = try #require(parser.finish().first)
            #expect(record.added == 1)
            #expect(record.files.map { Data($0.path.utf8) } == paths.map { Data($0.utf8) })
            #expect(record.files.map(\.excludedReason) == paths.map { $0.utf8.elementsEqual(composed.utf8) ? "generated" : "" })
        }
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
    @Test func unreadableAuthorDatesSkipOnlyThatCommit() throws {
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration())
        let bad = ["", String(repeating: "b", count: 40), "B", "b@example.test", "", "", "Broken", "\n9\t9\tbroken.swift", "0\t0\t", "old.swift", "new.swift", ""]
        try parser.feed(Data(bad.joined(separator: "\0").utf8))
        try parser.feed(payload(files: ["\n1\t0\tkept.swift"]))
        try parser.feed(Data((["", String(repeating: "c", count: 40), "C", "c@example.test", "abc", "", "Garbage", "\n4\t4\tlate.swift", ""]).joined(separator: "\0").utf8))
        let records = try parser.finish()
        #expect(records.map(\.id) == [String(repeating: "a", count: 40)])
        #expect(records[0].files.map(\.path) == ["kept.swift"])
        #expect(records[0].added == 1)
        #expect(parser.skipped == 2)
        #expect(GitHistory.skippedCommitsMessage(1) == "Skipped 1 commit with an unreadable author date.")
        #expect(GitHistory.skippedCommitsMessage(2) == "Skipped 2 commits with unreadable author dates.")
    }
    @Test func scansRemoteTrackingHistoryAndReusesMatchingCache() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let history = GitHistory(root: fixture.root)
        let repo = try fixture.repository()
        let (scanned, records) = try await history.scan(repo, configuration: WorkspaceConfiguration())
        #expect(records.count == 2)
        #expect(scanned.revision == (try fixture.git(["-C", fixture.clone.path, "rev-parse", "refs/remotes/origin/main"])))
        #expect(scanned.error.isEmpty)
        #expect(Set(records.map(\.author)) == ["Mapped Author"])
        #expect(records.contains { $0.ai })
        #expect(records.flatMap(\.files).contains { $0.path == "new\tname\n.swift" && $0.previousPath == "source.swift" })
        #expect(records.flatMap(\.files).contains { $0.path == "vendor/generated.swift" && $0.excludedReason == "vendored" })
        let second = try await history.scan(repo, configuration: WorkspaceConfiguration())
        #expect(second.0.scannedAt == scanned.scannedAt)
        #expect(second.1 == records)
        var config = WorkspaceConfiguration(); config.allFiles = true
        let all = try await history.scan(scanned, configuration: config)
        #expect(Analysis.totals(all.1).added > Analysis.totals(records).added)
        let url = try await history.pathURL(repo: scanned, path: "new\tname\n.swift")
        #expect(url.absoluteString.contains(scanned.revision))
        #expect(url.absoluteString.contains("%09") && url.absoluteString.contains("%0A"))
        let present = try await history.presentPaths(
            repo: scanned, paths: ["new\tname\n.swift", "source.swift", "vendor", "vendor/generated.swift", "missing/dir", "*"])
        #expect(present == ["new\tname\n.swift", "vendor", "vendor/generated.swift"])
    }
    @Test func separateHistoryInstancesCoordinateSharedCache() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let repo = try fixture.repository()
        let first = GitHistory(root: fixture.root), second = GitHistory(root: fixture.root)
        async let a = first.scan(repo, configuration: WorkspaceConfiguration())
        async let b = second.scan(repo, configuration: WorkspaceConfiguration())
        let results = try await (a, b)
        #expect(results.0.1 == results.1.1)
        #expect(results.0.0.scannedAt == results.1.0.scannedAt)
    }
    @Test func scanIgnoresLocalBranchesWorkingTreeAndRepositoryMailmapSettings() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let clone = fixture.clone
        try fixture.write("local.swift", "local only\n", in: clone)
        _ = try fixture.git(["-C", clone.path, "add", "local.swift"])
        _ = try fixture.git(["-C", clone.path, "commit", "--quiet", "-m", "Unpushed local work"])
        try fixture.write("uncommitted.swift", "draft\n", in: clone)
        try fixture.write(".mailmap", "Working Tree <tree@example.test> Original Author <old@example.test>\n", in: clone)
        let override = fixture.base.appendingPathComponent("override-mailmap")
        try Data("Config Author <config@example.test> Original Author <old@example.test>\n".utf8).write(to: override)
        _ = try fixture.git(["-C", clone.path, "config", "mailmap.file", override.path])
        let local = try fixture.git(["-C", clone.path, "rev-parse", "HEAD"])
        let result = try await GitHistory(root: fixture.root).scan(try fixture.repository(), configuration: WorkspaceConfiguration())
        #expect(result.1.count == 2)
        #expect(result.0.revision != local)
        #expect(!result.1.contains { $0.subject == "Unpushed local work" })
        #expect(!result.1.flatMap(\.files).contains { $0.path == "uncommitted.swift" || $0.path == "local.swift" })
        #expect(Set(result.1.map(\.author)) == ["Mapped Author"])
        #expect(Set(result.1.map(\.email)) == ["mapped@example.test"])
    }
    @Test func scanCacheIsReplacedWhenTheRemoteTrackingRefMoves() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let history = GitHistory(root: fixture.root)
        let repo = try fixture.repository()
        let first = try await history.scan(repo, configuration: WorkspaceConfiguration())
        try fixture.advance()
        let second = try await history.scan(repo, configuration: WorkspaceConfiguration())
        #expect(first.1.count == 2 && second.1.count == 3)
        #expect(first.0.revision != second.0.revision)
        #expect(second.1.contains { $0.subject == "Later" })
    }
    @Test func unfetchedOrMissingClonesExplainWhatToDo() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let history = GitHistory(root: fixture.root)
        var repo = try fixture.repository()
        repo.branch = ""
        await #expect { try await history.scan(repo, configuration: WorkspaceConfiguration()) } throws: {
            $0.localizedDescription.contains("default branch is unknown")
        }
        repo.branch = "release"
        await #expect { try await history.scan(repo, configuration: WorkspaceConfiguration()) } throws: {
            $0.localizedDescription.contains("refs/remotes/origin/release has not been fetched yet")
        }
        repo.branch = "main"
        repo.path = fixture.base.appendingPathComponent("gone/.git").path
        await #expect { try await history.scan(repo, configuration: WorkspaceConfiguration()) } throws: {
            $0.localizedDescription.contains("is missing")
        }
        repo.path = ""
        await #expect(throws: (any Error).self) { try await history.scan(repo, configuration: WorkspaceConfiguration()) }
    }
    @Test func partialClonesHaveUnknownLinesAndPaths() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        _ = try fixture.git(["--git-dir=" + fixture.server.path, "config", "uploadpack.allowFilter", "true"])
        let clone = try fixture.addClone("owner/partial", arguments: ["--filter=blob:none"])
        let repo = try #require(await CloneDiscovery.inspect(clone))
        #expect(repo.partial && !repo.shallow)
        let result = try await GitHistory(root: fixture.root).scan(repo, configuration: WorkspaceConfiguration())
        #expect(result.1.count == 2)
        #expect(result.1.allSatisfy { $0.linesUnknown && $0.pathsUnknown && $0.files.isEmpty })
        #expect(result.0.error.contains("Partial clone"))
        #expect(Analysis.totals(result.1).unknown)
    }
    @Test func shallowClonesAreMarkedAsPartialHistory() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let clone = try fixture.addClone("owner/shallow", arguments: ["--depth", "1"])
        let repo = try #require(await CloneDiscovery.inspect(clone))
        #expect(repo.shallow && !repo.partial)
        let result = try await GitHistory(root: fixture.root).scan(repo, configuration: WorkspaceConfiguration())
        #expect(result.1.count == 1)
        #expect(result.0.error.contains("Shallow clone"))
    }
    @Test func fetchArgumentsOnlyUpdateTheRemoteTrackingDefaultBranch() {
        var repo = Repository(id: "owner/repo", branch: "trunk")
        repo.remote = "upstream"
        #expect(GitHistory.fetchArguments(repo, gitDirectory: "/clone/.git") == [
            "--git-dir=/clone/.git", "-c", "fetch.writeCommitGraph=false", "-c", "fetch.recurseSubmodules=false",
            "fetch", "--quiet", "--no-tags", "--no-prune", "--no-recurse-submodules", "--no-write-fetch-head",
            "--no-auto-maintenance", "--no-auto-gc", "--refmap=", "upstream",
            "+refs/heads/trunk:refs/remotes/upstream/trunk",
        ])
        #expect(GitHistory.scanRevision(repo) == "refs/remotes/upstream/trunk")
    }
    @Test func sshNeverPromptsUnlessTheUserChoseACommand() {
        #expect(GitHistory.batchSSH(configured: "", environment: [:]) == ["-c", "core.sshCommand=ssh -o BatchMode=yes"])
        #expect(GitHistory.batchSSH(configured: "ssh -i ~/.ssh/work", environment: [:]).isEmpty)
        #expect(GitHistory.batchSSH(configured: "", environment: ["GIT_SSH_COMMAND": "ssh -v"]).isEmpty)
        #expect(GitHistory.batchSSH(configured: "", environment: ["GIT_SSH": "/usr/bin/ssh"]).isEmpty)
    }
    @Test func refreshFetchesThroughTheClonesOwnGitSetupWithoutChangingIt() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let clone = fixture.clone
        _ = try fixture.git(["-C", clone.path, "config", "url.file://\(fixture.server.path).insteadOf", "https://github.com/\(fixture.id).git"])
        func state() throws -> String {
            try fixture.git(["-C", clone.path, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads"]) + "\n"
                + fixture.git(["-C", clone.path, "status", "--porcelain=v1"]) + "\n"
                + fixture.git(["-C", clone.path, "rev-parse", "HEAD"]) + "\n"
                + fixture.git(["-C", clone.path, "config", "--local", "--list"])
        }
        let before = try state()
        try fixture.advance(clones: [])
        let expected = try fixture.git(["--git-dir=" + fixture.server.path, "rev-parse", "refs/heads/main"])
        let history = GitHistory(root: fixture.root, environment: ["HOME": fixture.base.path])
        var repo = try fixture.repository()
        repo.remote = "origin"
        #expect(await history.fetchedAt(repo.id) == nil)
        let started = Date()
        let refreshed = try await history.refresh(repo)
        let fetched = try #require(await history.fetchedAt(repo.id))
        #expect(fetched >= started.addingTimeInterval(-1) && fetched <= Date().addingTimeInterval(1))
        #expect(refreshed.revision == expected)
        #expect(try fixture.git(["-C", clone.path, "rev-parse", "refs/remotes/origin/main"]) == expected)
        #expect(try state() == before)
        #expect(!FileManager.default.fileExists(atPath: clone.appendingPathComponent(".git/FETCH_HEAD").path))
        var unknown = repo
        unknown.branch = ""
        #expect(try await history.refresh(unknown).branch == "main")
        _ = try fixture.git(["-C", clone.path, "config", "--unset", "url.file://\(fixture.server.path).insteadOf"])
        _ = try fixture.git(["-C", clone.path, "config", "url.file://\(fixture.base.path)/missing.git.insteadOf", "https://github.com/\(fixture.id).git"])
        let configured = try state()
        let lastFetch = await history.fetchedAt(repo.id)
        await #expect { try await history.refresh(repo) } throws: { $0 is GitCommandFailure && !$0.localizedDescription.isEmpty }
        #expect(await history.fetchedAt(repo.id) == lastFetch)
        #expect(try state() == configured)
    }
    private var processFamilyFixture: String {
        """
        (
            trap '' TERM
            printf ready > "$1"
            /bin/sleep 20
            printf completed > "$2"
        ) &
        wait
        """
    }
    private func awaitProcessFamilyReady(_ ready: URL, completion: ProcessFamilyCompletion) async throws {
        while !FileManager.default.fileExists(atPath: ready.path) && !completion.isFinished { try await Task.sleep(for: .milliseconds(10)) }
        try #require(FileManager.default.fileExists(atPath: ready.path))
    }
    @Test func cancellationStopsDescendantsHoldingOutputPipes() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let ready = directory.appendingPathComponent("ready")
        let completed = directory.appendingPathComponent("completed")
        let completion = ProcessFamilyCompletion()
        let task = Task {
            defer { completion.finish() }
            return try await GitProcess.execute(URL(fileURLWithPath: "/bin/sh"), arguments: ["-c", processFamilyFixture, "bigboard-process-family", ready.path, completed.path], environment: ["PATH": "/usr/bin:/bin"], directory: directory, timeout: 5)
        }
        defer { task.cancel() }
        try await awaitProcessFamilyReady(ready, completion: completion)
        let start = ContinuousClock.now
        task.cancel()
        await #expect(throws: CancellationError.self) { try await task.value }
        #expect(start.duration(to: .now) < .seconds(10))
        #expect(!FileManager.default.fileExists(atPath: completed.path))
    }
    @Test func timeoutStopsDescendantsHoldingOutputPipes() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let ready = directory.appendingPathComponent("ready")
        let completed = directory.appendingPathComponent("completed")
        let completion = ProcessFamilyCompletion()
        let task = Task {
            defer { completion.finish() }
            return try await GitProcess.execute(URL(fileURLWithPath: "/bin/sh"), arguments: ["-c", processFamilyFixture, "bigboard-process-family", ready.path, completed.path], environment: ["PATH": "/usr/bin:/bin"], directory: directory, timeout: 0.5)
        }
        defer { task.cancel() }
        try await awaitProcessFamilyReady(ready, completion: completion)
        let start = ContinuousClock.now
        do {
            _ = try await task.value
            Issue.record("A hung Git process family did not time out.")
        } catch { #expect(error.localizedDescription.contains("timeout")) }
        #expect(start.duration(to: .now) < .seconds(10))
        #expect(!FileManager.default.fileExists(atPath: completed.path))
    }
    @Test func cancellationInterruptsAttributeMatchingInsideStreamCallback() async throws {
        try await interruptAttributeMatching(timedOut: false)
    }
    @Test func timeoutInterruptsAttributeMatchingInsideStreamCallback() async throws {
        try await interruptAttributeMatching(timedOut: true)
    }
    @Test func stderrOutputLimitSurvivesParserCancellation() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let log = directory.appendingPathComponent("history"), trigger = directory.appendingPathComponent("emit-diagnostic"), diagnostic = directory.appendingPathComponent("diagnostic")
        try Data("1\t0\tsource.swift\0".utf8).write(to: log)
        try Data(repeating: 120, count: 64 * 1024 + 1).write(to: diagnostic)
        let rules = (0..<64).map { "generated-\($0).swift linguist-generated" }.joined(separator: "\n")
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), attributes: try HistoryRules.attributes(rules))
        try parser.feed(payload())
        let gate = ParserCheckpointGate(pauseAt: 12), completion = ProcessFamilyCompletion()
        let script = """
        /bin/cat "$1"
        exec 1>/dev/null
        while [ ! -f "$2" ]; do /bin/sleep 0.01; done
        /bin/cat "$3" >&2
        """
        let task = Task {
            defer { completion.finish() }
            return try await GitProcess.execute(URL(fileURLWithPath: "/bin/sh"), arguments: ["-c", script, "bigboard-stderr-limit", log.path, trigger.path, diagnostic.path], environment: ["PATH": "/usr/bin:/bin"], directory: directory, timeout: 10, stream: { data, checkpoint in
                try parser.feed(data, checkpoint: { try gate.checkpoint(checkpoint) })
            })
        }
        defer { task.cancel(); gate.release() }
        let deadline = ContinuousClock.now.advanced(by: .seconds(10))
        while !gate.isPaused && !completion.isFinished && ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        try #require(gate.isPaused)
        try Data().write(to: trigger)
        do {
            _ = try await task.value
            Issue.record("A Git diagnostic output limit returned success.")
        } catch { #expect(error.localizedDescription == "Git output exceeded the safety limit.") }
        #expect(gate.interruption is CancellationError)
        #expect(throws: CancellationError.self) { try parser.finish() }
    }
    private func interruptAttributeMatching(timedOut: Bool) async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let log = directory.appendingPathComponent("history")
        try Data("1\t0\tsource.swift\0".utf8).write(to: log)
        let rules = (0..<64).map { "generated-\($0).swift linguist-generated" }.joined(separator: "\n")
        let parser = GitLogParser(repo: "o/r", configuration: WorkspaceConfiguration(), attributes: try HistoryRules.attributes(rules))
        try parser.feed(payload())
        let gate = ParserCheckpointGate(pauseAt: 12), completion = ProcessFamilyCompletion()
        let task = Task {
            defer { completion.finish() }
            return try await GitProcess.execute(URL(fileURLWithPath: "/bin/cat"), arguments: [log.path], environment: ["PATH": "/usr/bin:/bin"], directory: directory, timeout: timedOut ? 2 : 10, stream: { data, checkpoint in
                try parser.feed(data, checkpoint: { try gate.checkpoint(checkpoint) })
            })
        }
        defer { task.cancel(); gate.release() }
        let deadline = ContinuousClock.now.advanced(by: .seconds(10))
        while !gate.isPaused && !completion.isFinished && ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        try #require(gate.isPaused)
        #expect(!completion.isFinished)
        #expect(parser.records.first?.files.isEmpty == true)
        if timedOut {
            while gate.interruption == nil && ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
            let error = try #require(gate.interruption)
            #expect(error.localizedDescription == "Git did not finish within the operation timeout.")
        } else { task.cancel() }
        gate.release()
        do {
            _ = try await task.value
            Issue.record("An interrupted attribute matcher returned a successful Git stream.")
        } catch {
            if timedOut { #expect(error.localizedDescription == "Git did not finish within the operation timeout.") }
            else { #expect(error is CancellationError) }
        }
        #expect(parser.records.first?.files.isEmpty == true)
        #expect(throws: (any Error).self) { try parser.finish() }
    }
    @Test func canceledScanDoesNotPublishParsedCache() async throws {
        let fixture = try HistoryFixture()
        defer { try? FileManager.default.removeItem(at: fixture.base) }
        let history = GitHistory(root: fixture.root)
        let repo = try fixture.repository()
        let task = Task { try await history.scan(repo, configuration: WorkspaceConfiguration()) }
        task.cancel()
        await #expect(throws: CancellationError.self) { try await task.value }
        #expect(!FileManager.default.fileExists(atPath: fixture.root.appendingPathComponent("scans").path))
    }
    @Test func boundedGitOutput() async throws {
        await #expect(throws: (any Error).self) { try await GitProcess.run(["--version"], limit: 1) }
    }
}

private final class ProcessFamilyCompletion: @unchecked Sendable {
    private let lock = NSLock()
    private var finished = false
    func finish() { lock.lock(); finished = true; lock.unlock() }
    var isFinished: Bool { lock.lock(); defer { lock.unlock() }; return finished }
}

private final class ParserCheckpointGate: @unchecked Sendable {
    private let lock = NSLock()
    private let resume = DispatchSemaphore(value: 0)
    private let pauseAt: Int
    private var checks = 0
    private var paused = false
    private var error: (any Error)?
    init(pauseAt: Int) { self.pauseAt = pauseAt }
    func checkpoint(_ probe: @Sendable () throws -> Void) throws {
        lock.lock()
        checks += 1
        let shouldPause = checks == pauseAt
        if shouldPause { paused = true }
        lock.unlock()
        do {
            if shouldPause { while resume.wait(timeout: .now() + .milliseconds(10)) == .timedOut { try probe() } }
            try probe()
        } catch { lock.lock(); self.error = error; lock.unlock(); throw error }
    }
    func release() { resume.signal() }
    var isPaused: Bool { lock.lock(); defer { lock.unlock() }; return paused }
    var interruption: (any Error)? { lock.lock(); defer { lock.unlock() }; return error }
}

struct HistoryFixture {
    let base: URL
    let root: URL
    let checkout: URL
    let server: URL
    let sources: URL
    let id: String
    var clone: URL { cloneURL(id) }
    init(id: String = "owner/repo") throws {
        self.id = id
        base = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        root = base.appendingPathComponent("cache")
        checkout = base.appendingPathComponent("author")
        server = base.appendingPathComponent("server/repo.git")
        sources = base.appendingPathComponent("src")
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
        try FileManager.default.createDirectory(at: server.deletingLastPathComponent(), withIntermediateDirectories: true)
        _ = try git(["clone", "--quiet", "--bare", "--single-branch", "--branch", "main", checkout.path, server.path])
        try addClone(id)
    }
    func cloneURL(_ id: String) -> URL { sources.appendingPathComponent(id) }
    @discardableResult func addClone(_ id: String, at location: URL? = nil, arguments: [String] = []) throws -> URL {
        let destination = location ?? cloneURL(id)
        try FileManager.default.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true)
        _ = try git(["clone", "--quiet"] + arguments + ["file://" + server.path, destination.path])
        _ = try git(["-C", destination.path, "config", "remote.origin.url", "https://github.com/\(id).git"])
        _ = try git(["-C", destination.path, "config", "user.name", "Local Author"])
        _ = try git(["-C", destination.path, "config", "user.email", "local@example.test"])
        return destination
    }
    func gitDirectory(_ clone: URL? = nil) throws -> String {
        try git(["-C", (clone ?? self.clone).path, "rev-parse", "--path-format=absolute", "--git-common-dir"])
    }
    func repository(_ id: String? = nil, clone: URL? = nil) throws -> Repository {
        let id = id ?? self.id
        var repo = Repository(id: id, branch: "main")
        repo.path = try gitDirectory(clone ?? cloneURL(id))
        return repo
    }
    func advance(_ path: String = "later.swift", _ contents: String = "later\n", message: String = "Later", clones: [URL]? = nil) throws {
        try write(path, contents)
        _ = try git(["-C", checkout.path, "add", "."])
        _ = try git(["-C", checkout.path, "commit", "--quiet", "-m", message])
        _ = try git(["-C", checkout.path, "push", "--quiet", "file://" + server.path, "main:main"])
        for clone in clones ?? [self.clone] {
            _ = try git(["-C", clone.path, "fetch", "--quiet", "file://" + server.path, "+refs/heads/main:refs/remotes/origin/main"])
        }
    }
    func write(_ path: String, _ contents: String, in directory: URL? = nil) throws {
        let url = (directory ?? checkout).appendingPathComponent(path)
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data(contents.utf8).write(to: url)
    }
    func git(_ arguments: [String]) throws -> String {
        let process = Process(), pipe = Pipe()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        process.arguments = ["-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"] + arguments
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
