import Foundation

struct GitHubIdentity: Codable, Sendable, Equatable {
    var login: String
    var accountID: String
    var valid: Bool {
        validGitHubLogin(login) && !accountID.isEmpty && accountID.allSatisfy(\.isNumber)
            && UInt64(accountID) != nil
    }
}
enum GitHubCLIFailure: LocalizedError, Sendable, Equatable {
    case missing, signedOut, rejected
    var errorDescription: String? {
        switch self {
        case .missing: "GitHub CLI is not installed. Install it with `brew install gh`, then check again."
        case .signedOut: "GitHub CLI is not signed in. Run `gh auth login` in Terminal, then check again."
        case .rejected:
            "GitHub rejected the GitHub CLI token. Run `gh auth refresh` or `gh auth login` in Terminal, then check again."
        }
    }
}
enum GitHubCLIRunner {
    static func environment(_ inherited: [String: String] = ProcessInfo.processInfo.environment) -> [String:
        String]
    {
        var environment = GitProcess.environment(inherited)
        if let directory = inherited["GH_CONFIG_DIR"], directory.hasPrefix("/") {
            environment["GH_CONFIG_DIR"] = directory
        }
        environment.merge([
            "GH_PROMPT_DISABLED": "1", "GH_NO_UPDATE_NOTIFIER": "1", "GH_NO_EXTENSION_UPDATE_NOTIFIER": "1",
            "GH_SPINNER_DISABLED": "1", "NO_COLOR": "1",
        ]) { _, new in new }
        return environment
    }
    @Sendable static func run(_ arguments: [String]) async throws -> String {
        let executable = try GitExecutable.select("gh")
        let data = try await GitProcess.execute(
            executable, arguments: arguments, environment: environment(),
            directory: URL(fileURLWithPath: "/"), timeout: 30, limit: 64 * 1024)
        return String(decoding: data, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
actor GitHubCLIAuth {
    typealias Runner = @Sendable ([String]) async throws -> String
    private let run: Runner
    private let transport: any GitHubTransport
    private let storage: WorkspaceStorage?
    private let now: @Sendable () -> Date
    private var cached: String?
    private var cachedAt = Date.distantPast
    private var identity: GitHubIdentity?
    init(
        storage: WorkspaceStorage? = nil, run: @escaping Runner = GitHubCLIRunner.run,
        transport: any GitHubTransport = URLSessionGitHubTransport(),
        now: @escaping @Sendable () -> Date = Date.init
    ) {
        self.storage = storage
        self.run = run
        self.transport = transport
        self.now = now
    }
    func token() async throws -> String {
        try Task.checkCancellation()
        if let cached, now().timeIntervalSince(cachedAt) < 300 { return cached }
        let value: String
        do {
            value = try await run(["auth", "token", "--hostname", "github.com"])
        } catch let failure as GitHubCLIFailure {
            throw failure
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            throw GitHubCLIFailure.signedOut
        }
        try Task.checkCancellation()
        guard !value.isEmpty, value.utf8.count <= 4096, value.utf8.allSatisfy({ (33...126).contains($0) })
        else { throw GitHubCLIFailure.signedOut }
        cached = value
        cachedAt = now()
        return value
    }
    func invalidate() {
        cached = nil
        cachedAt = .distantPast
    }
    func status() async -> AuthStatus {
        var result = AuthStatus()
        do {
            _ = try await token()
        } catch {
            result.error = error.localizedDescription
            return result
        }
        do {
            let user = try await GitHubService(token: { try await self.token() }, transport: transport, now: now)
                .viewer()
            let current = GitHubIdentity(login: user.login, accountID: String(user.id))
            identity = current
            try? storage?.saveIdentity(current)
        } catch let failure as GitHubFailure where failure == .auth {
            invalidate()
            result.error = GitHubCLIFailure.rejected.localizedDescription
            return result
        } catch {
            if identity == nil, let saved = try? storage?.loadIdentity() { identity = saved }
            if identity == nil {
                result.error = error.localizedDescription
                return result
            }
        }
        guard let identity else { return result }
        result.signedIn = true
        result.login = identity.login
        result.accountID = identity.accountID
        return result
    }
    func verify(using github: GitHubService, account: String) async throws -> AuthStatus {
        let user = try await github.viewer()
        guard String(user.id) == account else { throw GitHubFailure.auth }
        let current = GitHubIdentity(login: user.login, accountID: account)
        if identity != current {
            identity = current
            try? storage?.saveIdentity(current)
        }
        var result = AuthStatus(signedIn: true)
        result.login = current.login
        result.accountID = current.accountID
        return result
    }
}
