import Foundation
import AuthenticationServices
import Testing
@testable import Bigboard

@MainActor
private final class TestWebAuthentication: WebAuthenticating {
    struct Request {
        let url: URL
        let scheme: String
        let continuation: CheckedContinuation<URL, Error>
    }
    var requests: [Request] = []
    var cancellations = 0

    func authenticate(url: URL, callbackScheme: String) async throws -> URL {
        try await withCheckedThrowingContinuation {
            requests.append(Request(url: url, scheme: callbackScheme, continuation: $0))
        }
    }
    func cancel() { cancellations += 1 }
    func waitForRequests(_ count: Int) async throws {
        for _ in 0..<100 {
            if requests.count >= count { return }
            try await Task.sleep(for: .milliseconds(5))
        }
        throw BigboardError.message("Browser session did not start.")
    }
    func callback(_ index: Int) throws -> URL {
        let query = try #require(URLComponents(url: requests[index].url, resolvingAgainstBaseURL: false)?.queryItems)
        let state = try #require(query.first { $0.name == "state" }?.value)
        let redirect = try #require(query.first { $0.name == "redirect_uri" }?.value)
        var parts = try #require(URLComponents(string: redirect))
        parts.queryItems = [URLQueryItem(name: "state", value: state), URLQueryItem(name: "code", value: "fixture-code")]
        return try #require(parts.url)
    }
    func complete(_ index: Int, _ result: Result<URL, Error>) {
        requests[index].continuation.resume(with: result)
    }
}

private final class BrowserTestCredentials: CredentialStore, @unchecked Sendable {
    private let lock = NSLock()
    private var data: Data?
    func load() throws -> Data? { lock.withLock { data } }
    func save(_ data: Data) throws { lock.withLock { self.data = data } }
    func delete() throws { lock.withLock { data = nil } }
}

private actor BrowserTestTransport: GitHubTransport {
    private(set) var exchanges = 0
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        let body: String
        switch request.url?.path {
        case "/login/oauth/access_token":
            exchanges += 1
            body = #"{"access_token":"fixture-access","refresh_token":"fixture-refresh","token_type":"bearer","expires_in":28800,"refresh_token_expires_in":15811200}"#
        case "/user": body = #"{"login":"fixture","id":123}"#
        case "/user/installations": body = #"{"total_count":0,"installations":[]}"#
        default: throw GitHubFailure.unavailable
        }
        return GitHubHTTPResponse(status: 200, headers: [:], data: Data(body.utf8))
    }
}

@MainActor
private struct BrowserFixture {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    let browser = TestWebAuthentication()
    let credentials = BrowserTestCredentials()
    let network = BrowserTestTransport()
    let auth: AuthManager
    let workspace: WorkspaceModel

    init(identity: ApplicationIdentity = .production) {
        auth = AuthManager(
            configuration: AuthConfiguration(clientID: "fixture-client", clientSecret: "fixture-secret", appSlug: "fixture-app", identity: identity),
            store: credentials, transport: network)
        workspace = WorkspaceModel(
            storage: WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache")),
            authentication: auth, webAuthentication: browser, transport: network)
    }
    func remove() { try? FileManager.default.removeItem(at: root) }
}

@Suite @MainActor
struct WebAuthenticationTests {
    @Test(arguments: ["success", "cancel", "missing-result"])
    func systemCallbackDeliversOnMainActor(outcome: String) async throws {
        let url = try #require(URL(string: "com.richhaase.bigboard://oauth/github?code=fixture-code"))
        let result: Result<URL, Error> = await withCheckedContinuation { continuation in
            let callback = WebAuthentication.completionHandler { result in
                MainActor.assertIsolated()
                continuation.resume(returning: result)
            }
            DispatchQueue.global().async {
                dispatchPrecondition(condition: .notOnQueue(.main))
                switch outcome {
                case "success": callback(url, nil)
                case "cancel": callback(nil, ASWebAuthenticationSessionError(.canceledLogin))
                default: callback(nil, nil)
                }
            }
        }
        switch outcome {
        case "success": #expect(try result.get() == url)
        case "cancel":
            guard case .failure(let error) = result else { Issue.record("Expected cancellation"); return }
            #expect(error is CancellationError)
        default:
            guard case .failure(let error) = result else { Issue.record("Expected callback failure"); return }
            #expect(error as? AuthFailure == .callback)
        }
    }

    @Test(arguments: ApplicationIdentity.allCases)
    func browserCallbackCompletesSignIn(identity: ApplicationIdentity) async throws {
        let fixture = BrowserFixture(identity: identity)
        defer { fixture.remove() }
        await fixture.workspace.start()
        let signIn = Task { await fixture.workspace.signIn() }
        try await fixture.browser.waitForRequests(1)
        await fixture.workspace.signIn()
        #expect(fixture.browser.requests.count == 1)
        #expect(fixture.browser.requests[0].scheme == identity.rawValue)
        fixture.browser.complete(0, .success(try fixture.browser.callback(0)))
        await signIn.value
        #expect(fixture.workspace.authStatus.signedIn)
        #expect(fixture.workspace.authStatus.accountID == "123")
        #expect(!fixture.workspace.authStatus.pending)
        #expect(try fixture.credentials.load() != nil)
        #expect(await fixture.network.exchanges == 1)
    }

    @Test(arguments: ["cancel", "startup", "wrong-route"])
    func browserFailureClearsAttemptAndAllowsRetry(outcome: String) async throws {
        let fixture = BrowserFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let signIn = Task { await fixture.workspace.signIn() }
        try await fixture.browser.waitForRequests(1)
        switch outcome {
        case "cancel": fixture.browser.complete(0, .failure(CancellationError()))
        case "startup": fixture.browser.complete(0, .failure(BigboardError.message("Could not start GitHub sign-in.")))
        default:
            var callback = try #require(URLComponents(url: fixture.browser.callback(0), resolvingAgainstBaseURL: false))
            callback.scheme = ApplicationIdentity.development.rawValue
            fixture.browser.complete(0, .success(try #require(callback.url)))
        }
        await signIn.value
        #expect(!fixture.workspace.authStatus.pending)
        #expect(!fixture.workspace.authStatus.signedIn)
        #expect(fixture.workspace.error.isEmpty == (outcome == "cancel"))
        #expect(!(await fixture.auth.status()).pending)
        #expect(await fixture.network.exchanges == 0)
        let retry = Task { await fixture.workspace.signIn() }
        try await fixture.browser.waitForRequests(2)
        fixture.browser.complete(1, .success(try fixture.browser.callback(1)))
        await retry.value
        #expect(fixture.workspace.authStatus.signedIn)
        #expect(fixture.workspace.error.isEmpty)
    }

    @Test(arguments: ["cancel", "sign-out", "shutdown"])
    func abandonedBrowserCannotCompleteNewAttempt(action: String) async throws {
        let fixture = BrowserFixture()
        defer { fixture.remove() }
        await fixture.workspace.start()
        let signIn = Task { await fixture.workspace.signIn() }
        try await fixture.browser.waitForRequests(1)
        switch action {
        case "sign-out": #expect(await fixture.workspace.signOut())
        case "shutdown": await fixture.workspace.shutdown()
        default: await fixture.workspace.cancelSignIn()
        }
        #expect(fixture.browser.cancellations == 1)
        #expect(!fixture.workspace.authStatus.pending)
        let retry = Task { await fixture.workspace.signIn() }
        try await fixture.browser.waitForRequests(2)
        fixture.browser.complete(0, .success(try fixture.browser.callback(0)))
        await signIn.value
        #expect(fixture.workspace.authStatus.pending)
        #expect(!fixture.workspace.authStatus.signedIn)
        #expect(await fixture.network.exchanges == 0)
        fixture.browser.complete(1, .success(try fixture.browser.callback(1)))
        await retry.value
        #expect(fixture.workspace.authStatus.signedIn)
        #expect(await fixture.network.exchanges == 1)
    }
}
