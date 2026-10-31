import Foundation
import CryptoKit
import Security

struct AuthConfiguration: Sendable {
    var clientID: String
    var clientSecret: String
    var appSlug: String
    var callbackURI = "com.richhaase.bigboard://oauth/github"
    var valid: Bool { validAuthValue(clientID) && validAuthValue(clientSecret) && !appSlug.isEmpty && appSlug.utf8.count <= 100 && appSlug.utf8.allSatisfy { (97...122).contains($0) || (48...57).contains($0) || $0 == 45 } && callbackURI == "com.richhaase.bigboard://oauth/github" }
    static func fromBundle() -> AuthConfiguration {
        func value(_ key: String) -> String { ProcessInfo.processInfo.environment[key] ?? Bundle.main.object(forInfoDictionaryKey: key) as? String ?? "" }
        return AuthConfiguration(clientID: value("BIGBOARD_GITHUB_CLIENT_ID"), clientSecret: value("BIGBOARD_GITHUB_CLIENT_SECRET"), appSlug: value("BIGBOARD_GITHUB_APP_SLUG"))
    }
}
protocol CredentialStore: Sendable {
    func load() throws -> Data?
    func save(_ data: Data) throws
    func delete() throws
}
struct KeychainCredentialStore: CredentialStore {
    var service = "com.richhaase.bigboard.github"
    private var query: [String: Any] { [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: "github.com", kSecAttrSynchronizable as String: false] }
    func load() throws -> Data? {
        var request = query
        request[kSecReturnData as String] = true
        request[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(request as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let data = result as? Data else { throw AuthFailure.storage }
        return data
    }
    func save(_ data: Data) throws {
        let update = [kSecValueData as String: data]
        var status = SecItemUpdate(query as CFDictionary, update as CFDictionary)
        if status == errSecItemNotFound {
            var item = query
            item[kSecValueData as String] = data
            item[kSecAttrLabel as String] = "Bigboard GitHub sign-in"
            status = SecItemAdd(item as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw AuthFailure.storage }
    }
    func delete() throws {
        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw AuthFailure.storage }
    }
}
enum AuthFailure: LocalizedError, Sendable {
    case configuration, storage, callback, denied, exchange, expired
    var errorDescription: String? {
        switch self {
        case .configuration: "GitHub sign-in is unavailable in this build. Publisher configuration is required."
        case .storage: "Unable to access Bigboard sign-in credentials in Keychain."
        case .callback: "This sign-in response is invalid or expired. Sign in again."
        case .denied: "GitHub sign-in was canceled or declined."
        case .exchange: "GitHub sign-in could not be completed. Try again."
        case .expired: "Your GitHub sign-in has expired. Sign in again."
        }
    }
}
struct AuthCredentials: Codable, Sendable {
    var accessToken: String
    var refreshToken: String
    var expiresAt: Date
    var refreshExpiresAt: Date
    var login: String
    var accountID: String
    var clientID: String
    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token", refreshToken = "refresh_token", expiresAt = "expires_at", refreshExpiresAt = "refresh_expires_at", login, accountID = "account_id", clientID = "client_id"
    }
    var valid: Bool { validAuthValue(accessToken) && validAuthValue(refreshToken) && validAuthValue(clientID) && validGitHubLogin(login) && UInt64(accountID).map { $0 > 0 && String($0) == accountID } == true }
}
private func validAuthValue(_ value: String) -> Bool { !value.isEmpty && value.utf8.count <= 4096 && value.utf8.allSatisfy { (33...126).contains($0) } }
actor AuthManager {
    private let configuration: AuthConfiguration
    private let store: any CredentialStore
    private let transport: any GitHubTransport
    private let now: @Sendable () -> Date
    private var loaded = false
    private var credentials: AuthCredentials?
    private var unsaved: AuthCredentials?
    private var pending: Pending?
    private var generation = 0
    private var refreshTask: Task<AuthCredentials, Error>?
    private var exchangeTask: Task<AuthCredentials, Error>?
    private var message = ""
    private var retryAt = Date.distantPast
    private var rateFailures = 0
    private struct Pending: Sendable { var state: String; var verifier: String; var expires: Date; var generation: Int }
    init(configuration: AuthConfiguration, store: any CredentialStore, transport: any GitHubTransport = URLSessionGitHubTransport(), now: @escaping @Sendable () -> Date = Date.init) {
        self.configuration = configuration; self.store = store; self.transport = transport; self.now = now
    }
    private func load() throws {
        guard !loaded else { return }
        do {
            if let data = try store.load() {
                guard data.count <= 32768 else { throw AuthFailure.storage }
                let decoder = JSONDecoder(); decoder.dateDecodingStrategy = .custom { decoder in
                    let string = try decoder.singleValueContainer().decode(String.self)
                    guard let date = githubDate(string) else { throw AuthFailure.storage }; return date
                }
                let saved = try decoder.decode(AuthCredentials.self, from: data)
                guard saved.valid else { throw AuthFailure.storage }
                if saved.clientID == configuration.clientID { credentials = saved }
            }
            loaded = true
        } catch { throw AuthFailure.storage }
    }
    private func save(_ value: AuthCredentials) throws {
        guard value.valid else { throw AuthFailure.exchange }
        let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
        do { try store.save(encoder.encode(value)) } catch { throw AuthFailure.storage }
        credentials = value; loaded = true; unsaved = nil
    }
    func status() -> AuthStatus {
        var result = AuthStatus(configured: configuration.valid)
        guard configuration.valid else { return result }
        do { try load() } catch { result.error = error.localizedDescription; return result }
        if let pending, pending.expires <= now() { self.pending = nil; message = AuthFailure.callback.localizedDescription }
        result.pending = pending != nil || exchangeTask != nil
        if let saved = credentials, saved.refreshExpiresAt > now() {
            result.signedIn = true; result.login = saved.login; result.accountID = saved.accountID
        }
        result.error = message
        return result
    }
    func start() throws -> URL {
        guard configuration.valid else { throw AuthFailure.configuration }
        cancelOperations()
        let state = try randomString(), verifier = try randomString()
        pending = Pending(state: state, verifier: verifier, expires: now().addingTimeInterval(600), generation: generation)
        var url = URLComponents(string: "https://github.com/login/oauth/authorize")!
        url.queryItems = [URLQueryItem(name: "client_id", value: configuration.clientID), URLQueryItem(name: "redirect_uri", value: configuration.callbackURI), URLQueryItem(name: "state", value: state), URLQueryItem(name: "code_challenge", value: base64URL(Data(SHA256.hash(data: Data(verifier.utf8))))), URLQueryItem(name: "code_challenge_method", value: "S256")]
        message = ""
        return url.url!
    }
    func complete(_ url: URL) async throws {
        guard configuration.valid else { throw AuthFailure.configuration }
        guard url.absoluteString.utf8.count <= 16384, let parts = URLComponents(url: url, resolvingAgainstBaseURL: false), parts.scheme == "com.richhaase.bigboard", parts.host == "oauth", parts.path == "/github", parts.percentEncodedPath == "/github", parts.port == nil, parts.user == nil, parts.password == nil, parts.fragment == nil, let attempt = pending, attempt.expires > now() else { throw AuthFailure.callback }
        let items = parts.queryItems ?? []
        let states = items.filter { $0.name == "state" }
        guard states.count == 1, constantEqual(states[0].value ?? "", attempt.state) else { throw AuthFailure.callback }
        pending = nil
        if items.contains(where: { $0.name == "error" && !($0.value ?? "").isEmpty }) { throw AuthFailure.denied }
        let codes = items.filter { $0.name == "code" }
        guard codes.count == 1, let code = codes[0].value, validAuthValue(code) else { throw AuthFailure.callback }
        let task = Task { try await self.performSignIn(code: code, verifier: attempt.verifier) }
        exchangeTask = task
        do {
            let result = try await task.value
            try Task.checkCancellation()
            guard generation == attempt.generation else { throw CancellationError() }
            try save(result); message = ""; exchangeTask = nil
        } catch {
            if generation == attempt.generation { exchangeTask = nil; message = error.localizedDescription }
            throw error
        }
    }
    private func performSignIn(code: String, verifier: String) async throws -> AuthCredentials {
        var value = try await exchange(["code": code, "redirect_uri": configuration.callbackURI, "code_verifier": verifier])
        var request = URLRequest(url: URL(string: "https://api.github.com/user")!); request.timeoutInterval = 15
        request.setValue("Bearer \(value.accessToken)", forHTTPHeaderField: "Authorization")
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.setValue("2026-03-10", forHTTPHeaderField: "X-GitHub-Api-Version")
        request.setValue("Bigboard", forHTTPHeaderField: "User-Agent")
        let response = try await transport.send(request, maximumBytes: 32768)
        guard response.status == 200 else { throw AuthFailure.exchange }
        struct User: Decodable { var login: String; var id: UInt64 }
        guard let user = try? JSONDecoder().decode(User.self, from: response.data), user.id > 0, validGitHubLogin(user.login) else { throw AuthFailure.exchange }
        value.login = user.login; value.accountID = String(user.id)
        return value
    }
    func token() async throws -> String {
        try Task.checkCancellation()
        guard configuration.valid else { throw AuthFailure.configuration }
        try load()
        if let unsaved { try save(unsaved) }
        guard let saved = credentials, saved.refreshExpiresAt > now() else { throw GitHubFailure.auth }
        if saved.expiresAt > now().addingTimeInterval(60) { return saved.accessToken }
        let expected = generation
        let task: Task<AuthCredentials, Error>
        if let existing = refreshTask { task = existing } else {
            task = Task {
                var result = try await self.exchange(["grant_type": "refresh_token", "refresh_token": saved.refreshToken])
                result.login = saved.login; result.accountID = saved.accountID
                return result
            }
            refreshTask = task
        }
        do {
            let next = try await task.value
            guard generation == expected else { throw CancellationError() }
            do { try save(next) } catch { unsaved = next; throw error }
            refreshTask = nil; message = ""; try Task.checkCancellation(); return next.accessToken
        } catch {
            if generation == expected {
                refreshTask = nil; message = error.localizedDescription
                if case AuthFailure.expired = error { credentials = nil; unsaved = nil; throw GitHubFailure.auth }
            }
            throw error
        }
    }
    func cancel() { cancelOperations(); message = "" }
    private func cancelOperations() { generation += 1; pending = nil; exchangeTask?.cancel(); exchangeTask = nil; refreshTask?.cancel(); refreshTask = nil; unsaved = nil }
    func invalidateSession() {
        cancelOperations(); credentials = nil; loaded = true; message = AuthFailure.expired.localizedDescription
    }
    func signOut() throws {
        cancelOperations(); credentials = nil; loaded = true; message = ""
        do { try store.delete() } catch { message = AuthFailure.storage.localizedDescription; throw AuthFailure.storage }
    }
    func installationURL() -> URL? { configuration.valid ? URL(string: "https://github.com/apps/\(configuration.appSlug)/installations/new") : nil }
    private func exchange(_ fields: [String: String]) async throws -> AuthCredentials {
        try Task.checkCancellation()
        guard retryAt <= now() else { throw GitHubFailure.rateLimited }
        var fields = fields; fields["client_id"] = configuration.clientID; fields["client_secret"] = configuration.clientSecret
        var body = URLComponents(); body.queryItems = fields.sorted { $0.key < $1.key }.map { URLQueryItem(name: $0.key, value: $0.value) }
        var request = URLRequest(url: URL(string: "https://github.com/login/oauth/access_token")!)
        request.httpMethod = "POST"; request.timeoutInterval = 15; request.httpBody = Data((body.percentEncodedQuery ?? "").replacingOccurrences(of: "+", with: "%2B").utf8)
        request.setValue("application/json", forHTTPHeaderField: "Accept"); request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        let response = try await transport.send(request, maximumBytes: 32768)
        if response.isRateLimited {
            retryAt = response.retryDate(now: now(), failures: rateFailures); rateFailures += 1
            throw GitHubFailure.rateLimited
        }
        struct Result: Decodable {
            var access_token: String?; var refresh_token: String?; var token_type: String?; var expires_in: Int?; var refresh_token_expires_in: Int?; var error: String?
        }
        guard let result = try? JSONDecoder().decode(Result.self, from: response.data) else { throw AuthFailure.exchange }
        if fields["grant_type"] == "refresh_token", ["bad_refresh_token", "invalid_grant", "expired_token"].contains(result.error ?? "") { throw AuthFailure.expired }
        guard response.status == 200, result.error == nil, let access = result.access_token, let refresh = result.refresh_token, validAuthValue(access), validAuthValue(refresh), result.token_type?.lowercased() == "bearer", let seconds = result.expires_in, (1...86400).contains(seconds), let lifetime = result.refresh_token_expires_in, (1...31622400).contains(lifetime) else { throw AuthFailure.exchange }
        rateFailures = 0
        return AuthCredentials(accessToken: access, refreshToken: refresh, expiresAt: now().addingTimeInterval(Double(seconds)), refreshExpiresAt: now().addingTimeInterval(Double(lifetime)), login: "", accountID: "", clientID: configuration.clientID)
    }
}
private func randomString() throws -> String {
    var bytes = [UInt8](repeating: 0, count: 32)
    guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else { throw AuthFailure.exchange }
    return base64URL(Data(bytes))
}
private func base64URL(_ data: Data) -> String { data.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "") }
private func constantEqual(_ a: String, _ b: String) -> Bool {
    let lhs = Array(a.utf8), rhs = Array(b.utf8)
    guard lhs.count == rhs.count else { return false }
    var difference: UInt8 = 0
    for index in lhs.indices { difference |= lhs[index] ^ rhs[index] }
    return difference == 0
}
