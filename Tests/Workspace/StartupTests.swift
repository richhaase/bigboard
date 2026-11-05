import Foundation
import Testing
@testable import Bigboard

@Suite @MainActor
struct StartupTests {
    @Test func restoresTheSavedWorkspaceFromLocalClonesForWhicheverAccountIsActive() async throws {
        let history = try HistoryFixture(id: StartupFixture.repository)
        let fixture = StartupFixture(sources: history.sources)
        defer { fixture.remove(); try? FileManager.default.removeItem(at: history.base) }
        try fixture.saveWorkspace()

        for account in ["123", "456"] {
            let workspace = fixture.workspace(account: account)
            await workspace.start()

            #expect(!workspace.checkingSignIn && !workspace.busy)
            #expect(workspace.authStatus.signedIn && workspace.authStatus.accountID == account)
            #expect(workspace.configuration == fixture.configuration())
            #expect(workspace.snapshot.accountID == account)
            #expect(workspace.snapshot.viewer == "user" + account)
            #expect(workspace.snapshot.repositories.map(\.id) == [StartupFixture.repository])
            #expect(workspace.snapshot.commits.count == 2)
            #expect(Set(workspace.snapshot.commits.map(\.repo)) == [StartupFixture.repository])
            #expect(workspace.snapshot.repositories.first?.path == (try history.gitDirectory()))
            #expect(workspace.snapshot.cachedOnly && workspace.snapshot.coverage.phase == "ready")
            #expect(workspace.error.isEmpty)
            await workspace.shutdown()
        }
        #expect(await fixture.network.requests == 0)
    }

    @Test(arguments: [GitHubCLIFailure.signedOut, .missing, .rejected])
    func unavailableCredentialsLeaveSavedWorkspaceUnopened(condition: GitHubCLIFailure) async throws {
        let fixture = StartupFixture()
        defer { fixture.remove() }
        try fixture.saveWorkspace()
        let before = try fixture.savedSettings()
        let token = CLITokenSource()
        if condition == .signedOut { token.set(nil) }
        if condition == .missing { token.set(nil, failure: GitHubCLIFailure.missing) }
        let workspace = fixture.workspace(token: token, status: condition == .rejected ? 401 : 200)
        await workspace.start()

        #expect(!workspace.checkingSignIn && !workspace.busy && !workspace.authStatus.signedIn)
        #expect(workspace.snapshot.coverage.phase == "signin")
        #expect(workspace.snapshot.accountID.isEmpty && workspace.snapshot.repositories.isEmpty)
        #expect(workspace.snapshot.commits.isEmpty && workspace.discovered.isEmpty)
        #expect(workspace.configuration == WorkspaceConfiguration())
        #expect(workspace.error == condition.localizedDescription)
        #expect(workspace.authStatus.configured == (condition != .missing))
        #expect(await fixture.network.requests == 0)
        #expect(try fixture.savedSettings() == before)
    }

    @Test func damagedSettingsStopStartupWithoutReplacingThem() async throws {
        let fixture = StartupFixture()
        defer { fixture.remove() }
        try fixture.saveWorkspace()
        try Data("{broken".utf8).write(to: fixture.storage.configurationFile)
        let before = try fixture.savedSettings()
        let workspace = fixture.workspace()
        await workspace.start()

        #expect(workspace.authStatus.signedIn)
        #expect(!workspace.checkingSignIn && !workspace.busy && !workspace.error.isEmpty)
        #expect(workspace.snapshot.repositories.isEmpty && workspace.snapshot.commits.isEmpty)
        #expect(workspace.discovered.isEmpty)
        #expect(await fixture.network.requests == 0)
        #expect(try fixture.savedSettings() == before)
    }

    @Test func savedRepositoryWithoutALocalCloneIsNotCollected() async throws {
        let fixture = StartupFixture()
        defer { fixture.remove() }
        try fixture.saveWorkspace()
        let workspace = fixture.workspace()
        await workspace.start()

        #expect(workspace.authStatus.signedIn && !workspace.checkingSignIn && !workspace.busy)
        #expect(workspace.configuration == fixture.configuration())
        #expect(workspace.snapshot.repositories.isEmpty && workspace.snapshot.commits.isEmpty)
        #expect(workspace.discovered.isEmpty && workspace.error.isEmpty)
        #expect(await fixture.network.requests == 0)
        #expect(try FileManager.default.contentsOfDirectory(atPath: fixture.storage.configRoot.path) == ["config.json"])
    }
}

@MainActor private struct StartupFixture {
    static let repository = "user/repo"
    static let now = Date(timeIntervalSince1970: 1_800_000_000)
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    let network = StartupTransport()
    let sources: URL?
    init(sources: URL? = nil) { self.sources = sources }
    var storage: WorkspaceStorage {
        WorkspaceStorage(configRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
    }

    func configuration() -> WorkspaceConfiguration {
        var config = WorkspaceConfiguration()
        config.repositories = [Self.repository]
        config.groups = ["Focus": config.repositories]
        config.workAreas = [Self.repository: [WorkArea(name: "Sources", paths: ["src/"])]]
        config.cachedOnly = true
        config.sinceDays = 30
        config.sourceRoots = [(sources ?? root.appendingPathComponent("src")).path]
        return config
    }
    func saveWorkspace() throws {
        try FileManager.default.createDirectory(at: root.appendingPathComponent("src"), withIntermediateDirectories: true)
        try storage.saveConfiguration(configuration())
    }
    func savedSettings() throws -> Data { try Data(contentsOf: storage.configurationFile) }
    func workspace(account: String = "123", token: CLITokenSource = CLITokenSource(), status: Int = 200) -> WorkspaceModel {
        let now = Self.now
        let user = CLIUserTransport(login: "user" + account, account: account, status: status)
        let auth = cliAuth(storage: storage, token: token, user: user, now: { now })
        return WorkspaceModel(storage: storage, authentication: auth, transport: network)
    }
    func remove() { try? FileManager.default.removeItem(at: root) }
}

private actor StartupTransport: GitHubTransport {
    private(set) var requests = 0
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        requests += 1
        throw GitHubFailure.unavailable
    }
}
