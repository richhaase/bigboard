import Foundation
import Testing
@testable import Bigboard

private final class Captured: @unchecked Sendable {
    private let lock = NSLock()
    private var out = ""
    private var err = ""
    private var opened = 0
    func write(_ text: String) { lock.withLock { out += text } }
    func writeError(_ text: String) { lock.withLock { err += text } }
    func open() { lock.withLock { opened += 1 } }
    var output: String { lock.withLock { out } }
    var error: String { lock.withLock { err } }
    var opens: Int { lock.withLock { opened } }
}

private struct PullRequestFixture: GitHubTransport {
    static func node(_ number: Int, author: String, draft: Bool, review: String?, requested: [String], check: String, failing: [String]) -> [String: Any] {
        [
            "number": number, "title": number == 7 ? "Add search" : "Draft cleanup", "author": ["login": author],
            "isDraft": draft, "reviewDecision": review as Any? ?? NSNull(), "mergeable": "MERGEABLE",
            "createdAt": "2026-10-02T12:00:00Z", "updatedAt": "2026-10-04T12:0\(number):00Z", "additions": 12, "deletions": 3,
            "headRefOid": "head\(number)", "baseRefOid": "base", "labels": ["nodes": [["name": "bug"]]],
            "reviewRequests": ["totalCount": requested.count, "pageInfo": ["hasNextPage": false], "nodes": requested.map { ["requestedReviewer": ["login": $0]] }],
            "latestReviews": ["totalCount": 0, "pageInfo": ["hasNextPage": false], "nodes": [Any]()],
            "files": ["totalCount": 2, "pageInfo": ["hasNextPage": false], "nodes": [["path": "src/a.swift"], ["path": "src/b.swift"]]],
            "commits": ["nodes": [["commit": ["statusCheckRollup": [
                "state": check,
                "contexts": ["pageInfo": ["hasNextPage": false], "nodes": failing.map { ["__typename": "CheckRun", "name": $0, "conclusion": "FAILURE"] }],
            ]]]]],
        ]
    }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        guard request.url?.path == "/graphql" else { throw GitHubFailure.unavailable }
        let nodes = [
            Self.node(7, author: "someone", draft: false, review: "REVIEW_REQUIRED", requested: ["fixture"], check: "SUCCESS", failing: []),
            Self.node(8, author: "fixture", draft: true, review: nil, requested: [], check: "FAILURE", failing: ["test"]),
        ]
        let document: [String: Any] = ["data": ["repository": [
            "nameWithOwner": "owner/repo",
            "pullRequests": ["totalCount": nodes.count, "pageInfo": ["hasNextPage": false], "nodes": nodes],
        ]]]
        return GitHubHTTPResponse(status: 200, headers: [:], data: try JSONSerialization.data(withJSONObject: document))
    }
}

private struct UnavailableGitHub: GitHubTransport {
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse { throw GitHubFailure.unavailable }
}

private struct CLIFixture {
    let history: HistoryFixture
    let root: URL
    let storage: WorkspaceStorage
    let captured = Captured()
    init(cache: Bool = true) throws {
        history = try HistoryFixture()
        root = history.base.appendingPathComponent("workspace")
        storage = WorkspaceStorage(
            configRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
        let empty = history.base.appendingPathComponent("empty-src")
        try FileManager.default.createDirectory(at: empty, withIntermediateDirectories: true)
        var configuration = WorkspaceConfiguration()
        configuration.repositories = ["owner/repo"]
        configuration.groups = ["Core": ["owner/repo"]]
        configuration.sourceRoots = [cache ? history.sources.path : empty.path]
        try storage.saveConfiguration(configuration)
    }
    func tool(token: CLITokenSource = CLITokenSource(), openFails: Bool = false, github: any GitHubTransport = PullRequestFixture()) -> CommandLineTool {
        let captured = captured
        let environment = CommandLineEnvironment(
            storage: storage, auth: cliAuth(storage: storage, token: token, user: CLIUserTransport()),
            transport: github, now: { Date(timeIntervalSince1970: 1791201600) },
            openApplication: {
                if openFails { throw BigboardError.message("Bigboard.app was not found next to this command.") }
                captured.open()
            },
            refreshHistory: { _, repo in
                var result = repo
                result.revision = String(repeating: "a", count: 40)
                return result
            },
            write: { captured.write($0) }, writeError: { captured.writeError($0) })
        return CommandLineTool(environment: environment)
    }
    func remove() { try? FileManager.default.removeItem(at: history.base) }
}

struct CommandLineTests {
    @Test func appExecutableStartsTheInterfaceUnlessACommandIsGiven() {
        let app = "/Applications/Bigboard.app/Contents/MacOS/Bigboard"
        #expect(BigboardCommandLine.commandArguments([app]) == nil)
        #expect(BigboardCommandLine.commandArguments([app, "-NSDocumentRevisionsDebugMode", "YES"]) == nil)
        #expect(BigboardCommandLine.commandArguments([app, "status"]) == ["status"])
        #expect(BigboardCommandLine.commandArguments(["bigboard"]) == [])
        #expect(BigboardCommandLine.commandArguments(["/opt/homebrew/bin/bigboard", "activity", "--json"]) == ["activity", "--json"])
        #expect(BigboardCommandLine.commandArguments(["bigboard-dev", "-NSDocumentRevisionsDebugMode"]) == ["-NSDocumentRevisionsDebugMode"])
    }
    @Test func noCommandPrintsHelpAndOnlyOpenLaunchesTheApplication() async throws {
        let fixture = try CLIFixture(cache: false)
        defer { fixture.remove() }
        #expect(await fixture.tool().run([]) == 0)
        #expect(fixture.captured.opens == 0)
        #expect(fixture.captured.output.contains("Usage: bigboard <command> [options]"))
        #expect(fixture.captured.output.contains("For scripts and agents:"))
        #expect(await fixture.tool().run(["open"]) == 0)
        #expect(fixture.captured.opens == 1)
        #expect(await fixture.tool(openFails: true).run(["open"]) == 1)
        #expect(fixture.captured.error.contains("Bigboard.app was not found"))
    }
    @Test func eachCommandHasItsOwnHelp() async throws {
        let fixture = try CLIFixture(cache: false)
        defer { fixture.remove() }
        #expect(await fixture.tool(token: CLITokenSource(nil)).run(["activity", "--help"]) == 0)
        #expect(fixture.captured.output.hasPrefix("Usage: bigboard activity [options]"))
        #expect(fixture.captured.output.contains("--people-sort ORDER"))
        let topic = try CLIFixture(cache: false)
        defer { topic.remove() }
        #expect(await topic.tool().run(["help", "refresh"]) == 0)
        #expect(topic.captured.output.hasPrefix("Usage: bigboard refresh [options]"))
        #expect(await topic.tool().run(["status", "-h"]) == 0)
        #expect(topic.captured.output.contains("Usage: bigboard status [--json]"))
        #expect(await topic.tool().run(["config", "--help"]) == 0)
        #expect(topic.captured.output.contains("bigboard config set|add|remove|unset SETTING"))
        #expect(await topic.tool().run(["help", "bogus"]) == 64)
        #expect(topic.captured.error.contains("No help for: bogus"))
        let usage = try CLIFixture(cache: false)
        defer { usage.remove() }
        #expect(await usage.tool().run(["activity", "--days", "soon"]) == 64)
        #expect(usage.captured.error.contains("Usage: bigboard activity [options]"))
    }
    @Test func configShowsAndChangesSettings() async throws {
        let fixture = try CLIFixture()
        defer { fixture.remove() }
        try fixture.history.addClone("owner/other", at: fixture.history.sources.appendingPathComponent("owner/other"))
        let tool = fixture.tool()
        #expect(await tool.run(["config"]) == 0)
        #expect(fixture.captured.output.contains("Settings: \(fixture.storage.configurationFile.path)"))
        #expect(fixture.captured.output.contains("repositories:\n  owner/repo\n"))
        #expect(fixture.captured.output.contains("group:\n  Core: owner/repo\n"))
        #expect(fixture.captured.output.contains("since-days: 14\n"))
        #expect(await tool.run(["config", "set", "since-days", "30"]) == 0)
        #expect(fixture.captured.output.hasSuffix("30\n"))
        #expect(await tool.run(["config", "add", "repositories", "Owner/Other"]) == 0)
        #expect(await tool.run(["config", "set", "group", "Team", "owner/repo", "Owner/Other"]) == 0)
        #expect(await tool.run(["config", "remove", "group", "Team", "owner/other"]) == 0)
        #expect(await tool.run(["config", "unset", "group", "Core"]) == 0)
        #expect(await tool.run(["config", "set", "work-area", "owner/repo", "App", "Sources/, Tests"]) == 0)
        #expect(await tool.run(["config", "set", "identity", "A@Example.com", "A Person", "a@old.example"]) == 0)
        #expect(await tool.run(["config", "add", "exclude", "*-archive"]) == 0)
        #expect(await tool.run(["config", "set", "contributor-sort", "commits"]) == 0)
        #expect(await tool.run(["config", "set", "cached-only", "yes"]) == 0)
        let saved = try fixture.storage.loadConfiguration()
        #expect(saved.sinceDays == 30)
        #expect(saved.repositories == ["owner/other", "owner/repo"])
        #expect(saved.groups == ["Team": ["owner/repo"]])
        #expect(saved.workAreas == ["owner/repo": [WorkArea(name: "App", paths: ["Sources/", "Tests"])]])
        #expect(saved.identities == ["a@example.com": IdentityRule(aliases: ["a@old.example"], name: "A Person")])
        #expect(saved.exclude == ["*-archive"])
        #expect(saved.contributorSort == "commits" && saved.cachedOnly)
        let json = try CLIFixture()
        defer { json.remove() }
        #expect(await json.tool().run(["config", "--json"]) == 0)
        #expect(json.captured.output.contains("\"since_days\" : 14"))
        #expect(json.captured.output.contains("\"name\" : \"Core\""))
        let one = try CLIFixture()
        defer { one.remove() }
        #expect(await one.tool().run(["config", "get", "group", "Core"]) == 0)
        #expect(one.captured.output == "owner/repo\n")
    }
    @Test func configRejectsChangesSettingsWouldRefuse() async throws {
        let fixture = try CLIFixture()
        defer { fixture.remove() }
        let before = try fixture.storage.loadConfiguration()
        let tool = fixture.tool()
        #expect(await tool.run(["config", "set", "refresh-interval", "7"]) == 1)
        #expect(fixture.captured.error.contains("supported automatic refresh interval"))
        #expect(await tool.run(["config", "add", "repositories", "owner/missing"]) == 1)
        #expect(fixture.captured.error.contains("No local clone was found for: owner/missing"))
        #expect(await tool.run(["config", "remove", "repositories", "owner/repo"]) == 1)
        #expect(fixture.captured.error.contains("Select at least one repository"))
        #expect(await tool.run(["config", "set", "sources", "/nonexistent-bigboard-folder"]) == 1)
        #expect(fixture.captured.error.contains("Not a folder"))
        #expect(await tool.run(["config", "remove", "group", "Nope", "owner/repo"]) == 1)
        #expect(await tool.run(["config", "set", "contributor-sort", "size"]) == 64)
        #expect(await tool.run(["config", "set", "colour", "blue"]) == 64)
        #expect(await tool.run(["config", "add", "since-days", "3"]) == 64)
        #expect(await tool.run(["config", "set", "since-days"]) == 64)
        #expect(await tool.run(["config", "rename", "group"]) == 64)
        #expect(fixture.captured.error.contains("Usage: bigboard config"))
        #expect(try fixture.storage.loadConfiguration() == before)
    }
    @Test func statusListsAccountAndCollectedRepositories() async throws {
        let fixture = try CLIFixture()
        defer { fixture.remove() }
        #expect(await fixture.tool().run(["status"]) == 0)
        #expect(fixture.captured.output.contains("GitHub CLI: fixture (123)"))
        #expect(fixture.captured.output.contains("owner/repo  main  "))
        #expect(fixture.captured.output.contains("src/owner/repo\n"))
        #expect(fixture.captured.output.contains("Sources:    /") && fixture.captured.output.contains("/src\n"))
        let json = try CLIFixture()
        defer { json.remove() }
        #expect(await json.tool().run(["status", "--json"]) == 0)
        let decoded = try JSONSerialization.jsonObject(with: Data(json.captured.output.utf8)) as? [String: Any]
        #expect(decoded?["account_id"] as? String == "123")
        let rows = decoded?["repositories"] as? [[String: Any]]
        #expect(rows?.first?["id"] as? String == "owner/repo")
        #expect(rows?.first?["clone"] as? String == (try json.history.gitDirectory()))
        #expect(rows?.first?["shallow"] as? Bool == false && rows?.first?["partial"] as? Bool == false)
        #expect((decoded?["source_folders"] as? [String])?.count == 1)
        let missing = try CLIFixture(cache: false)
        defer { missing.remove() }
        #expect(await missing.tool().run(["status"]) == 0)
        #expect(missing.captured.output.contains("owner/repo    no local clone found in source folders"))
    }
    @Test func activitySummarizesCachedHistory() async throws {
        let fixture = try CLIFixture()
        defer { fixture.remove() }
        #expect(await fixture.tool().run(["activity", "--days", "7", "--group", "Core"]) == 0)
        let text = fixture.captured.output
        #expect(text.contains("fixture · last 7 days · 1 active repository"))
        #expect(text.contains("2 commits"))
        #expect(text.contains("People (1)\n  Mapped Author  owner/repo\n"))
        #expect(text.contains("Excluded: 1 vendored lines (not counted above)"))
        #expect(text.contains("not productivity"))
        let ranked = try CLIFixture()
        defer { ranked.remove() }
        #expect(await ranked.tool().run(["activity", "--days", "7", "--people-sort", "lines"]) == 0)
        #expect(ranked.captured.output.contains("People by lines\n  Name           Commits"))
        #expect(!ranked.captured.output.contains("People (1)"))
        let json = try CLIFixture()
        defer { json.remove() }
        #expect(await json.tool().run(["activity", "--days=0", "--json", "--top", "1"]) == 0)
        let decoded = try #require(try JSONSerialization.jsonObject(with: Data(json.captured.output.utf8)) as? [String: Any])
        #expect((decoded["totals"] as? [String: Any])?["commits"] as? Int == 2)
        #expect((decoded["directories"] as? [Any])?.count == 1)
        #expect((decoded["people"] as? [[String: Any]])?.first?["name"] as? String == "Mapped Author")
        #expect((decoded["people"] as? [[String: Any]])?.first?["email"] == nil)
        #expect(decoded["people_sort"] as? String == "name")
        #expect((decoded["totals"] as? [String: Any])?["unknown_line_commits"] as? Int == 0)
        let evidence = try #require((decoded["evidence"] as? [[String: Any]])?.first)
        #expect(evidence["repository"] as? String == "owner/repo" && evidence["branch"] as? String == "main")
        #expect((evidence["revision"] as? String)?.count == 40 && evidence["scanned_at"] is String)
        #expect(evidence["fetched_at"] is NSNull && evidence["history"] as? String == "complete")
        #expect(evidence["line_counts"] as? String == "complete" && evidence["paths"] as? String == "complete")
        #expect((evidence["problems"] as? [Any])?.isEmpty == true && (decoded["problems"] as? [Any])?.isEmpty == true)
        #expect((decoded["excluded"] as? [String: Any])?["vendored"] as? Int == 1)
        let empty = try CLIFixture()
        defer { empty.remove() }
        #expect(await empty.tool().run(["activity", "--days", "1"]) == 0)
        #expect(empty.captured.output.contains("No recorded change in this range."))
        let quiet = try CLIFixture()
        defer { quiet.remove() }
        #expect(await quiet.tool().run(["activity", "--days", "1", "--json"]) == 0)
        let quietReport = try #require(try JSONSerialization.jsonObject(with: Data(quiet.captured.output.utf8)) as? [String: Any])
        #expect((quietReport["repositories"] as? [Any])?.isEmpty == true)
        #expect((quietReport["evidence"] as? [[String: Any]])?.map { $0["repository"] as? String } == ["owner/repo"])
    }
    @Test func compareReportsThePreviousWindowWithAnInclusiveEnd() async throws {
        let fixture = try CLIFixture()
        defer { fixture.remove() }
        #expect(await fixture.tool().run(["activity", "--days", "4", "--compare"]) == 0)
        let text = fixture.captured.output
        #expect(text.contains("fixture · last 4 days · 0 active repositories"))
        #expect(text.contains("Previous 4 days: 2 commits"))
        #expect(text.contains("Previous  Change\n  owner/repo") && text.contains("quiet\n"))
        let json = try CLIFixture()
        defer { json.remove() }
        #expect(await json.tool().run(["activity", "--days", "4", "--compare", "--json"]) == 0)
        let decoded = try #require(try JSONSerialization.jsonObject(with: Data(json.captured.output.utf8)) as? [String: Any])
        #expect(decoded["depth"] as? Int == 1)
        let comparison = try #require(decoded["comparison"] as? [String: Any])
        #expect((comparison["totals"] as? [String: Any])?["commits"] as? Int == 2)
        #expect(comparison["start"] is String && comparison["end"] is String)
        let row = try #require((decoded["repositories"] as? [[String: Any]])?.first)
        #expect((row["totals"] as? [String: Any])?["commits"] as? Int == 0)
        #expect((row["previous"] as? [String: Any])?["commits"] as? Int == 2)
        let plain = try CLIFixture()
        defer { plain.remove() }
        #expect(await plain.tool().run(["activity", "--days", "7", "--json"]) == 0)
        let current = try #require(try JSONSerialization.jsonObject(with: Data(plain.captured.output.utf8)) as? [String: Any])
        #expect(current["comparison"] == nil)
        #expect((current["repositories"] as? [[String: Any]])?.first?["previous"] == nil)
    }
    @Test func pathScopeNarrowsTotalsAndListsSupportingCommits() async throws {
        let fixture = try CLIFixture()
        defer { fixture.remove() }
        #expect(await fixture.tool().run(["activity", "--days", "0", "--repo", "owner/repo", "--path", ".mailmap", "--commits", "--json"]) == 0)
        let report = try #require(try JSONSerialization.jsonObject(with: Data(fixture.captured.output.utf8)) as? [String: Any])
        let scope = try #require(report["scope"] as? [String: Any])
        #expect(scope["path"] as? String == ".mailmap" && scope["kind"] as? String == "file")
        #expect(scope["since"] is NSNull && scope["until"] is NSNull && scope["include_bots"] as? Bool == false)
        #expect((report["totals"] as? [String: Any])?["commits"] as? Int == 1)
        let commit = try #require((report["commits"] as? [[String: Any]])?.first)
        #expect((commit["sha"] as? String)?.count == 40 && (commit["url"] as? String)?.contains("github.com/owner/repo/commit/") == true)
        #expect(commit["author"] as? String == "Mapped Author" && commit["added"] as? Int == 1 && commit["email"] == nil)
        let folder = try CLIFixture()
        defer { folder.remove() }
        #expect(await folder.tool().run(["activity", "--days", "0", "--repo", "owner/repo", "--path", "vendor/"]) == 0)
        #expect(folder.captured.output.contains("fixture · owner/repo/vendor/ · all history · 0 active repositories"))
        #expect(folder.captured.output.contains("Excluded: 1 vendored lines"))
        let text = try CLIFixture()
        defer { text.remove() }
        #expect(await text.tool().run(["activity", "--days", "0", "--commits"]) == 0)
        #expect(text.captured.output.contains("Commits (newest first)\n  2026-10-01T12:00:00Z  "))
    }
    @Test func explicitIntervalExcludesItsStartAndIncludesItsEnd() async throws {
        let instant = "2026-10-01T12:00:00Z"
        let after = try CLIFixture()
        defer { after.remove() }
        #expect(await after.tool().run(["activity", "--since", instant, "--json"]) == 0)
        let excluded = try #require(try JSONSerialization.jsonObject(with: Data(after.captured.output.utf8)) as? [String: Any])
        #expect((excluded["totals"] as? [String: Any])?["commits"] as? Int == 0)
        #expect((excluded["scope"] as? [String: Any])?["since"] as? String == instant)
        let through = try CLIFixture()
        defer { through.remove() }
        #expect(await through.tool().run(["activity", "--until", instant, "--json"]) == 0)
        let included = try #require(try JSONSerialization.jsonObject(with: Data(through.captured.output.utf8)) as? [String: Any])
        #expect((included["totals"] as? [String: Any])?["commits"] as? Int == 2)
        #expect((included["scope"] as? [String: Any])?["since"] is NSNull)
        let window = try CLIFixture()
        defer { window.remove() }
        #expect(await window.tool().run(["activity", "--since", "2026-10-01T00:00:00Z", "--until", "2026-10-02T00:00:00Z", "--compare"]) == 0)
        #expect(window.captured.output.contains("after 2026-10-01T00:00:00Z through 2026-10-02T00:00:00Z"))
        #expect(window.captured.output.contains("Previous period (after 2026-09-30T00:00:00Z): 0 commits"))
    }
    @Test func pathsMustStayInsideTheRepository() throws {
        #expect(try CommandLineTool.repositoryPath("src/ui/") == "src/ui")
        for invalid in ["", "/src", "src/../ui", "./src", "src//ui"] {
            #expect(throws: UsageError.self) { try CommandLineTool.repositoryPath(invalid) }
        }
    }
    @Test func depthGroupsPathsDeeperAndKeepsShallowFiles() {
        let files = [FileChange(path: "packages/a/x.swift", added: 5), FileChange(path: "packages/b/y.swift", added: 3), FileChange(path: "README.md", added: 1)]
        let commits = [Commit(id: String(repeating: "a", count: 40), repo: "o/r", author: "A", email: "a@test", date: Date(timeIntervalSince1970: 1_791_200_000), subject: "Change", added: 9, files: files)]
        let repos = [Repository(id: "o/r")]
        let shallow = ActivityReport(account: "me", generatedAt: Date(), days: 0, commits: commits, repositories: repos, top: 10, evidence: [])
        #expect(shallow.directories.map(\.path) == ["packages", "README.md"])
        let deep = ActivityReport(account: "me", generatedAt: Date(), days: 0, commits: commits, repositories: repos, top: 10, depth: 2, evidence: [])
        #expect(deep.directories.map(\.path) == ["packages/a", "packages/b", "README.md"])
        #expect(deep.directories.map(\.kind) == ["directory", "directory", "file"])
        #expect(deep.text().contains("Top paths by lines changed (2 levels deep)"))
        #expect(ActivityReport.change(5, 0) == "new" && ActivityReport.change(0, 3) == "quiet")
        #expect(ActivityReport.change(3, 2) == "+50%" && ActivityReport.change(1, 2) == "−50%" && ActivityReport.change(2, 2) == "0%")
    }
    @Test func pullRequestsShowWhatNeedsAttention() async throws {
        let fixture = try CLIFixture()
        defer { fixture.remove() }
        #expect(await fixture.tool().run(["prs"]) == 0)
        let text = fixture.captured.output
        #expect(text.hasPrefix("fixture · 2 open pull requests in 1 repository · 2 need your attention\n"))
        #expect(text.contains("! owner/repo#7  Add search\n    someone · ready for review · needs review · checks passing · +12 −3 in 2 files · updated 2026-10-04\n    Needs your attention: your review is requested\n"))
        #expect(text.contains("! owner/repo#8  Draft cleanup\n    fixture · draft · review unknown · checks failing"))
        #expect(text.contains("    Needs your attention: checks are failing\n    Failing: test\n"))
        let json = try CLIFixture()
        defer { json.remove() }
        #expect(await json.tool().run(["prs", "--mine", "requested", "--json"]) == 0)
        let report = try #require(try JSONSerialization.jsonObject(with: Data(json.captured.output.utf8)) as? [String: Any])
        #expect(report["viewer"] as? String == "fixture")
        let counts = try #require(report["counts"] as? [String: Any])
        #expect(counts["open"] as? Int == 2 && counts["matching"] as? Int == 1 && counts["needs_attention"] as? Int == 2)
        #expect((report["filters"] as? [String: Any])?["mine"] as? [String] == ["requested"])
        let row = try #require((report["pull_requests"] as? [[String: Any]])?.first)
        #expect(row["number"] as? Int == 7 && row["review_requested"] as? Bool == true && row["review"] as? String == "review_required")
        #expect(row["labels"] as? [String] == ["bug"] && row["changed_files"] as? Int == 2 && row["files"] == nil)
        #expect((row["complete"] as? [String: Any])?["inventory"] as? Bool == true)
        let evidence = try #require((report["evidence"] as? [[String: Any]])?.first)
        #expect(evidence["repository"] as? String == "owner/repo" && evidence["open"] as? Int == 2 && evidence["inventory_complete"] as? Bool == true)
        #expect((report["problems"] as? [Any])?.isEmpty == true)
        let drafts = try CLIFixture()
        defer { drafts.remove() }
        #expect(await drafts.tool().run(["prs", "--state", "draft", "--checks", "FAILURE", "--files", "--json"]) == 0)
        let draft = try #require(try JSONSerialization.jsonObject(with: Data(drafts.captured.output.utf8)) as? [String: Any])
        let only = try #require((draft["pull_requests"] as? [[String: Any]]))
        #expect(only.map { $0["number"] as? Int } == [8])
        #expect(only.first?["files"] as? [String] == ["src/a.swift", "src/b.swift"] && only.first?["failing_checks"] as? [String] == ["test"])
        #expect(only.first?["attention_reason"] as? String == "checks are failing")
    }
    @Test func pullRequestFailuresAreReportedAsIncomplete() async throws {
        let fixture = try CLIFixture()
        defer { fixture.remove() }
        #expect(await fixture.tool(github: UnavailableGitHub()).run(["prs", "--json"]) == 2)
        let report = try #require(try JSONSerialization.jsonObject(with: Data(fixture.captured.output.utf8)) as? [String: Any])
        #expect((report["evidence"] as? [[String: Any]])?.first?["inventory_complete"] as? Bool == false)
        #expect((report["problems"] as? [[String: Any]])?.first?["repository"] as? String == "owner/repo")
    }
    @Test func missingHistoryIsReportedAsIncomplete() async throws {
        let fixture = try CLIFixture(cache: false)
        defer { fixture.remove() }
        #expect(await fixture.tool().run(["activity"]) == 2)
        #expect(fixture.captured.output.contains("Incomplete evidence:\n  owner/repo: No local clone found in source folders.\n"))
        let json = try CLIFixture(cache: false)
        defer { json.remove() }
        #expect(await json.tool().run(["activity", "--json"]) == 2)
        let report = try #require(try JSONSerialization.jsonObject(with: Data(json.captured.output.utf8)) as? [String: Any])
        let entry = try #require((report["evidence"] as? [[String: Any]])?.first)
        #expect(entry["history"] as? String == "missing" && entry["line_counts"] as? String == "unknown")
        #expect(entry["revision"] is NSNull && entry["remote"] is NSNull && entry["branch"] is NSNull)
        #expect((entry["problems"] as? [[String: Any]])?.first?["kind"] as? String == "no_clone")
        let problem = try #require((report["problems"] as? [[String: Any]])?.first)
        #expect(problem["repository"] as? String == "owner/repo" && problem["kind"] as? String == "no_clone")
    }
    @Test(arguments: [
        ["bogus"], ["activity", "--days", "soon"], ["activity", "--days"], ["activity", "--repo", "other/repo"],
        ["activity", "--group", "Missing"], ["status", "--days", "3"], ["activity", "--days", "4000"],
        ["activity", "--people-sort", "size"], ["activity", "--compare", "--days", "0"], ["activity", "--depth", "0"],
        ["activity", "--depth", "11"], ["activity", "--path", "src"], ["activity", "--repo", "owner/repo", "--path", "../x"],
        ["activity", "--days", "3", "--since", "2026-10-01"], ["activity", "--since", "soon"],
        ["activity", "--since", "2026-10-02", "--until", "2026-10-01"], ["prs", "--mine", "everything"],
        ["prs", "--sort", "size"], ["prs", "--review", "maybe"], ["prs", "--top", "0"],
    ])
    func invalidUsageExplainsAndExits64(arguments: [String]) async throws {
        let fixture = try CLIFixture(cache: false)
        defer { fixture.remove() }
        #expect(await fixture.tool().run(arguments) == 64)
        #expect(fixture.captured.error.contains("Usage: bigboard"))
    }
    @Test func missingSourceFoldersAreExplained() async throws {
        let fixture = try CLIFixture()
        defer { fixture.remove() }
        var configuration = try fixture.storage.loadConfiguration()
        configuration.sourceRoots = []
        try fixture.storage.saveConfiguration(configuration)
        #expect(await fixture.tool().run(["activity"]) == 1)
        #expect(fixture.captured.error.contains("No source folders are set"))
        #expect(await fixture.tool().run(["refresh"]) == 1)
        #expect(await fixture.tool().run(["status"]) == 0)
        #expect(fixture.captured.output.contains("Sources:    none. Choose folders in Bigboard Settings."))
        #expect(fixture.captured.output.contains("no local clone found in source folders"))
    }
    @Test func signedOutGitHubCLIExplainsWhatToRun() async throws {
        let fixture = try CLIFixture(cache: false)
        defer { fixture.remove() }
        #expect(await fixture.tool(token: CLITokenSource(nil)).run(["status"]) == 1)
        #expect(fixture.captured.error.contains("gh auth login"))
    }
    @Test func refreshFetchesLocalClonesAndReportsMissingOnes() async throws {
        let fixture = try CLIFixture()
        defer { fixture.remove() }
        #expect(await fixture.tool().run(["refresh"]) == 0)
        #expect(fixture.captured.output.contains("owner/repo: 2 commits at aaaaaaaaaaaa"))
        let missing = try CLIFixture(cache: false)
        defer { missing.remove() }
        #expect(await missing.tool().run(["refresh", "--repo", "owner/repo"]) == 2)
        #expect(missing.captured.error.contains("owner/repo: no local clone found in source folders"))
    }
}
