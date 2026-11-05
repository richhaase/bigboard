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

private struct RoutedTransport: GitHubTransport {
    var repositories: String
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        let path = request.url?.path ?? ""
        let body = path == "/user" ? #"{"login":"fixture","id":123}"# : repositories
        return GitHubHTTPResponse(status: 200, headers: [:], data: Data(body.utf8))
    }
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
            supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
        let empty = history.base.appendingPathComponent("empty-src")
        try FileManager.default.createDirectory(at: empty, withIntermediateDirectories: true)
        var configuration = WorkspaceConfiguration()
        configuration.repositories = ["owner/repo"]
        configuration.groups = ["Core": ["owner/repo"]]
        configuration.sourceRoots = [cache ? history.sources.path : empty.path]
        try storage.saveConfiguration(configuration, account: "123")
    }
    func tool(
        token: CLITokenSource = CLITokenSource(), openFails: Bool = false,
        repositories: String = #"[{"full_name":"owner/repo","default_branch":"main","archived":false}]"#
    ) -> CommandLineTool {
        let captured = captured
        let transport = RoutedTransport(repositories: repositories)
        let environment = CommandLineEnvironment(
            storage: storage, auth: cliAuth(storage: storage, token: token, user: CLIUserTransport()),
            transport: transport, now: { Date(timeIntervalSince1970: 1791201600) },
            openApplication: {
                if openFails { throw BigboardError.message("Bigboard.app was not found next to this command.") }
                captured.open()
            },
            refreshHistory: { _, repo, _ in
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
    @Test func noCommandOpensTheApplication() async throws {
        let fixture = try CLIFixture(cache: false)
        defer { fixture.remove() }
        #expect(await fixture.tool().run([]) == 0)
        #expect(await fixture.tool().run(["open"]) == 0)
        #expect(fixture.captured.opens == 2)
        #expect(await fixture.tool(openFails: true).run([]) == 1)
        #expect(fixture.captured.error.contains("Bigboard.app was not found"))
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
        #expect(text.contains("Mapped Author"))
        #expect(text.contains("not productivity"))
        let json = try CLIFixture()
        defer { json.remove() }
        #expect(await json.tool().run(["activity", "--days=0", "--json", "--top", "1"]) == 0)
        let decoded = try #require(try JSONSerialization.jsonObject(with: Data(json.captured.output.utf8)) as? [String: Any])
        #expect((decoded["totals"] as? [String: Any])?["commits"] as? Int == 2)
        #expect((decoded["directories"] as? [Any])?.count == 1)
        #expect((decoded["people"] as? [[String: Any]])?.first?["name"] as? String == "Mapped Author")
        #expect((decoded["people"] as? [[String: Any]])?.first?["email"] == nil)
        let empty = try CLIFixture()
        defer { empty.remove() }
        #expect(await empty.tool().run(["activity", "--days", "1"]) == 0)
        #expect(empty.captured.output.contains("No recorded change in this range."))
    }
    @Test func missingHistoryIsReportedAsIncomplete() async throws {
        let fixture = try CLIFixture(cache: false)
        defer { fixture.remove() }
        #expect(await fixture.tool().run(["activity"]) == 2)
        #expect(fixture.captured.output.contains("Incomplete evidence:"))
        #expect(fixture.captured.output.contains("owner/repo:"))
    }
    @Test(arguments: [
        ["bogus"], ["activity", "--days", "soon"], ["activity", "--days"], ["activity", "--repo", "other/repo"],
        ["activity", "--group", "Missing"], ["status", "--days", "3"], ["activity", "--days", "4000"],
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
        var configuration = try fixture.storage.loadConfiguration("123")
        configuration.sourceRoots = []
        try fixture.storage.saveConfiguration(configuration, account: "123")
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
