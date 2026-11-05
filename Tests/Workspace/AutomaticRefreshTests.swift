import Foundation
import Testing
@testable import Bigboard

@Suite @MainActor
struct AutomaticRefreshTests {
    @Test func intervalStartsAfterEachRefreshIncludingManualRefresh() async throws {
        let fixture = try AutomaticRefreshFixture()
        defer { fixture.remove() }
        let workspace = fixture.workspace
        await workspace.start()
        #expect(await fixture.network.requests == 1)
        #expect(workspace.nextAutomaticRefresh(visible: true, modalOpen: false) == fixture.clock.now().addingTimeInterval(900))

        fixture.clock.advance(by: 899)
        await workspace.refreshAutomatically(visible: true, modalOpen: false)
        #expect(await fixture.network.requests == 1)
        fixture.clock.advance(by: 1)
        await workspace.refreshAutomatically(visible: true, modalOpen: false)
        #expect(await fixture.network.requests == 2)
        #expect(workspace.nextAutomaticRefresh(visible: true, modalOpen: false) == fixture.clock.now().addingTimeInterval(900))

        fixture.clock.advance(by: 100)
        await workspace.refresh()
        #expect(await fixture.network.requests == 3)
        fixture.clock.advance(by: 899)
        await workspace.refreshAutomatically(visible: true, modalOpen: false)
        #expect(await fixture.network.requests == 3)
        fixture.clock.advance(by: 1)
        await workspace.refreshAutomatically(visible: true, modalOpen: false)
        #expect(await fixture.network.requests == 4)
    }

    @Test(arguments: ["hidden", "sheet", "busy", "cached", "manual", "signed-out"])
    func overdueRefreshWaitsUntilEligible(condition: String) async throws {
        let fixture = try AutomaticRefreshFixture()
        defer { fixture.remove() }
        let workspace = fixture.workspace
        await workspace.start()
        fixture.clock.advance(by: 1800)
        switch condition {
        case "busy": workspace.busy = true
        case "cached": workspace.configuration.cachedOnly = true
        case "manual": workspace.configuration.refreshIntervalMinutes = 0
        case "signed-out": workspace.authStatus.signedIn = false
        default: break
        }
        let visible = condition != "hidden", modalOpen = condition == "sheet"
        #expect(workspace.nextAutomaticRefresh(visible: visible, modalOpen: modalOpen) == nil)
        await workspace.refreshAutomatically(visible: visible, modalOpen: modalOpen)
        #expect(await fixture.network.requests == 1)

        workspace.busy = false
        workspace.configuration.cachedOnly = false
        workspace.configuration.refreshIntervalMinutes = 15
        workspace.authStatus.signedIn = true
        await workspace.refreshAutomatically(visible: true, modalOpen: false)
        #expect(await fixture.network.requests == 2)
    }

    @Test func failedRefreshWaitsForTheNextInterval() async throws {
        let fixture = try AutomaticRefreshFixture()
        defer { fixture.remove() }
        let workspace = fixture.workspace
        await workspace.start()
        await fixture.network.setUnavailable(true)
        fixture.clock.advance(by: 900)
        await workspace.refreshAutomatically(visible: true, modalOpen: false)
        #expect(workspace.snapshot.coverage.phase == "partial")
        #expect(!workspace.error.isEmpty)
        #expect(await fixture.network.requests == 2)
        await workspace.refreshAutomatically(visible: true, modalOpen: false)
        #expect(await fixture.network.requests == 2)

        await fixture.network.setUnavailable(false)
        fixture.clock.advance(by: 900)
        await workspace.refreshAutomatically(visible: true, modalOpen: false)
        #expect(await fixture.network.requests == 3)
        #expect(workspace.snapshot.coverage.phase == "ready" && workspace.error.isEmpty)
    }

    @Test func intervalChangesRescheduleAndShutdownStopsAutomaticRefresh() async throws {
        let fixture = try AutomaticRefreshFixture()
        defer { fixture.remove() }
        let workspace = fixture.workspace
        #expect(workspace.nextAutomaticRefresh(visible: true, modalOpen: false) == nil)
        await workspace.start()
        workspace.configuration.refreshIntervalMinutes = 30
        #expect(workspace.nextAutomaticRefresh(visible: true, modalOpen: false) == fixture.clock.now().addingTimeInterval(1800))
        fixture.clock.advance(by: 900)
        await workspace.refreshAutomatically(visible: true, modalOpen: false)
        #expect(await fixture.network.requests == 1)
        await workspace.shutdown()
        fixture.clock.advance(by: 900)
        #expect(workspace.nextAutomaticRefresh(visible: true, modalOpen: false) == nil)
        await workspace.refreshAutomatically(visible: true, modalOpen: false)
        #expect(await fixture.network.requests == 1)
    }
}

@MainActor private struct AutomaticRefreshFixture {
    let root: URL
    let workspace: WorkspaceModel
    let clock = AutomaticRefreshClock()
    let network: AutomaticRefreshTransport

    init() throws {
        network = AutomaticRefreshTransport(clock: clock)
        root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
        let sources = root.appendingPathComponent("src")
        try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
        var config = WorkspaceConfiguration(repositories: ["owner/repo"]); config.sourceRoots = [sources.path]
        try storage.saveConfiguration(config, account: "123")
        let auth = cliAuth(storage: storage, now: clock.now)
        workspace = WorkspaceModel(storage: storage, authentication: auth, transport: network, now: clock.now)
    }
    func remove() { try? FileManager.default.removeItem(at: root) }
}

private final class AutomaticRefreshClock: @unchecked Sendable {
    private let lock = NSLock()
    private var date = Date(timeIntervalSince1970: 1_800_000_000)
    func now() -> Date { lock.withLock { date } }
    func advance(by seconds: TimeInterval) { lock.withLock { date.addTimeInterval(seconds) } }
}

private actor AutomaticRefreshTransport: GitHubTransport {
    private let clock: AutomaticRefreshClock
    private(set) var requests = 0
    private var unavailable = false
    init(clock: AutomaticRefreshClock) { self.clock = clock }
    func setUnavailable(_ value: Bool) { unavailable = value }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        requests += 1
        clock.advance(by: 10)
        switch request.url?.path {
        case "/user":
            if unavailable { return GitHubHTTPResponse(status: 503, headers: [:], data: Data("{}".utf8)) }
            return GitHubHTTPResponse(status: 200, headers: [:], data: Data(#"{"login":"fixture","id":123}"#.utf8))
        default:
            Issue.record("Unexpected request during automatic refresh: \(request.url?.path ?? "")")
            throw GitHubFailure.unavailable
        }
    }
}
