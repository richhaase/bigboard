import Foundation
@testable import Bigboard

final class CLIUserTransport: GitHubTransport, @unchecked Sendable {
    private let lock = NSLock()
    private var login: String
    private var account: String
    private var status: Int
    private var requests = 0
    init(login: String = "fixture", account: String = "123", status: Int = 200) {
        self.login = login
        self.account = account
        self.status = status
    }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        let (login, account, status) = lock.withLock {
            requests += 1
            return (self.login, self.account, self.status)
        }
        if status == 0 { throw GitHubFailure.unavailable }
        return GitHubHTTPResponse(
            status: status, headers: [:], data: Data(#"{"login":"\#(login)","id":\#(account)}"#.utf8))
    }
    func respond(login: String? = nil, account: String? = nil, status: Int? = nil) {
        lock.withLock {
            if let login { self.login = login }
            if let account { self.account = account }
            if let status { self.status = status }
        }
    }
    var count: Int { lock.withLock { requests } }
}
final class CLITokenSource: @unchecked Sendable {
    private let lock = NSLock()
    private var value: String?
    private var failure: (any Error)?
    private var calls = 0
    init(_ value: String? = "fixture-access") { self.value = value }
    func set(_ value: String?, failure: (any Error)? = nil) {
        lock.withLock {
            self.value = value
            self.failure = failure
        }
    }
    var count: Int { lock.withLock { calls } }
    func read(_ arguments: [String]) throws -> String {
        try lock.withLock {
            calls += 1
            if let failure { throw failure }
            guard arguments == ["auth", "token", "--hostname", "github.com"], let value else {
                throw GitCommandFailure(status: 1, detail: "not logged in")
            }
            return value
        }
    }
}
func cliAuth(
    storage: WorkspaceStorage? = nil, login: String = "fixture", account: String = "123",
    token: CLITokenSource = CLITokenSource(), user: CLIUserTransport? = nil,
    now: @escaping @Sendable () -> Date = Date.init
) -> GitHubCLIAuth {
    let transport = user ?? CLIUserTransport(login: login, account: account)
    return GitHubCLIAuth(
        storage: storage, run: { try token.read($0) }, transport: transport, now: now)
}
@discardableResult func isolateSources(
    _ storage: WorkspaceStorage, sources: URL,
    configure: (inout WorkspaceConfiguration) -> Void = { _ in }
) throws -> URL {
    try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
    var config = try storage.loadConfiguration()
    config.sourceRoots = [sources.path]
    configure(&config)
    try storage.saveConfiguration(config)
    return sources
}
