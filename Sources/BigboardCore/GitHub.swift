import Foundation

func githubDate(_ string: String?) -> Date? {
    guard let string else { return nil }
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return formatter.date(from: string) ?? ISO8601DateFormatter().date(from: string)
}
enum GitHubFailure: LocalizedError, Sendable {
    case auth, permission, notFound, rateLimited, unavailable, response, budget, outputLimit, incomplete
    var isAccessDenied: Bool {
        switch self {
        case .auth, .permission, .notFound: true
        default: false
        }
    }
    var errorDescription: String? {
        switch self {
        case .auth: "GitHub rejected the GitHub CLI token. Run `gh auth login` in Terminal, then check again."
        case .permission: "GitHub access is not granted for this account or repository."
        case .notFound: "The GitHub repository is unavailable."
        case .rateLimited: "GitHub requests are paused by a rate limit. Refresh after the cooldown."
        case .unavailable: "Could not connect to GitHub."
        case .response: "GitHub returned an invalid response."
        case .budget: "The refresh request budget was reached."
        case .outputLimit: "GitHub response exceeded the size limit."
        case .incomplete: "GitHub returned incomplete evidence."
        }
    }
}
struct GitHubHTTPResponse: Sendable {
    var status: Int
    var headers: [String: String]
    var data: Data
    var isRateLimited: Bool {
        if status == 429 { return true }
        guard status == 403 else { return false }
        if headers["retry-after"] != nil || headers["x-ratelimit-remaining"] == "0" { return true }
        let text = String(data: data, encoding: .utf8)?.lowercased() ?? ""
        return text.contains("rate limit") || text.contains("rate_limit")
    }
    func retryDate(now: Date, failures: Int) -> Date {
        var result = now
        if let after = headers["retry-after"] {
            if let seconds = Double(after), seconds >= 0 {
                result = now.addingTimeInterval(seconds)
            } else {
                let f = DateFormatter()
                f.locale = Locale(identifier: "en_US_POSIX")
                f.dateFormat = "EEE, dd MMM yyyy HH:mm:ss zzz"
                result = f.date(from: after) ?? now
            }
        }
        if headers["x-ratelimit-remaining"] == "0", let seconds = Double(headers["x-ratelimit-reset"] ?? "") {
            result = max(result, Date(timeIntervalSince1970: seconds))
        }
        return result > now ? result : now.addingTimeInterval(60 * pow(2, Double(min(failures, 4))))
    }
}
protocol GitHubTransport: Sendable {
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse
}
private final class NoRedirectDelegate: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    func urlSession(
        _ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
        newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void
    ) { completionHandler(nil) }
}
struct URLSessionGitHubTransport: GitHubTransport {
    private let session: URLSession
    init() {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 15
        config.timeoutIntervalForResource = 30
        config.httpCookieStorage = nil
        config.urlCache = nil
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        session = URLSession(configuration: config, delegate: NoRedirectDelegate(), delegateQueue: nil)
    }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        do {
            let (bytes, response) = try await session.bytes(for: request)
            guard let http = response as? HTTPURLResponse else {
                bytes.task.cancel()
                throw GitHubFailure.response
            }
            if response.expectedContentLength > maximumBytes {
                bytes.task.cancel()
                throw GitHubFailure.outputLimit
            }
            var data = Data()
            data.reserveCapacity(min(maximumBytes, 65536))
            for try await byte in bytes {
                if data.count >= maximumBytes {
                    bytes.task.cancel()
                    throw GitHubFailure.outputLimit
                }
                if Task.isCancelled {
                    bytes.task.cancel()
                    throw CancellationError()
                }
                data.append(byte)
            }
            var headers: [String: String] = [:]
            for key in ["ETag", "Retry-After", "X-RateLimit-Remaining", "X-RateLimit-Reset"] {
                if let value = http.value(forHTTPHeaderField: key) { headers[key.lowercased()] = value }
            }
            return GitHubHTTPResponse(status: http.statusCode, headers: headers, data: data)
        } catch is CancellationError { throw CancellationError() } catch let error as GitHubFailure {
            throw error
        } catch {
            if Task.isCancelled { throw CancellationError() }
            throw GitHubFailure.unavailable
        }
    }
}
struct GitHubResult: Sendable {
    var repositories: [Repository]
    var pullRequests: [PullRequest]
    var coverage: Coverage
    var authenticationRequired = false
    var mergedPullRequests: [PullRequest] = []
}
struct GitHubUser: Decodable, Sendable {
    var login: String
    var id: UInt64
    var valid: Bool { id > 0 && validGitHubLogin(login) }
}
actor GitHubService {
    private let token: @Sendable () async throws -> String
    private let transport: any GitHubTransport
    private let now: @Sendable () -> Date
    private var busy = false
    private var requests = 0
    private var maximumRequests = 128
    private var repositoryLimit = Int.max
    private var deadline = Date.distantFuture
    private var retryAt = Date.distantPast
    private var failures = 0
    init(
        token: @escaping @Sendable () async throws -> String,
        transport: any GitHubTransport = URLSessionGitHubTransport(),
        now: @escaping @Sendable () -> Date = Date.init
    ) {
        self.token = token
        self.transport = transport
        self.now = now
    }
    private func begin(budget: Int) async throws {
        while busy { try await Task.sleep(for: .milliseconds(10)) }
        try Task.checkCancellation()
        busy = true
        requests = 0
        maximumRequests = max(0, budget)
        repositoryLimit = Int.max
        deadline = now().addingTimeInterval(120)
    }
    private func request(_ endpoint: String, body: Data? = nil) async throws -> Data {
        try Task.checkCancellation()
        guard now() < deadline else { throw GitHubFailure.budget }
        guard now() >= retryAt else { throw GitHubFailure.rateLimited }
        guard requests < maximumRequests, requests < repositoryLimit else { throw GitHubFailure.budget }
        guard !endpoint.hasPrefix("/"), !endpoint.contains(".."), !endpoint.contains(":"),
            !endpoint.contains("\\"), !endpoint.contains("\n"), !endpoint.contains("\r"),
            let url = URL(string: "https://api.github.com/" + endpoint)
        else { throw GitHubFailure.response }
        let credential = try await token()
        try Task.checkCancellation()
        guard !credential.isEmpty, credential.utf8.allSatisfy({ (33...126).contains($0) }) else {
            throw GitHubFailure.auth
        }
        var request = URLRequest(url: url)
        request.timeoutInterval = 15
        request.setValue("Bearer \(credential)", forHTTPHeaderField: "Authorization")
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.setValue("2026-03-10", forHTTPHeaderField: "X-GitHub-Api-Version")
        request.setValue("Bigboard", forHTTPHeaderField: "User-Agent")
        if let body {
            request.httpMethod = "POST"
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        requests += 1
        let response = try await transport.send(request, maximumBytes: 2 * 1024 * 1024)
        if response.isRateLimited || response.headers["x-ratelimit-remaining"] == "0" {
            retryAt = response.retryDate(now: now(), failures: failures)
            failures += 1
        }
        if response.isRateLimited { throw GitHubFailure.rateLimited }
        switch response.status {
        case 200..<300: break
        case 401: throw GitHubFailure.auth
        case 403: throw GitHubFailure.permission
        case 404: throw GitHubFailure.notFound
        case 500...599: throw GitHubFailure.unavailable
        default: throw GitHubFailure.response
        }
        if response.headers["x-ratelimit-remaining"] != "0" { failures = 0 }
        return response.data
    }
    func viewer() async throws -> GitHubUser {
        try await begin(budget: 1)
        defer { busy = false }
        let user = try decode(GitHubUser.self, try await request("user"))
        guard user.valid else { throw GitHubFailure.response }
        return user
    }
    func fetch(
        _ repos: [Repository], budget: Int, previous: [PullRequest], previousMerged: [PullRequest]? = nil
    ) async -> GitHubResult {
        do { try await begin(budget: budget) } catch {
            return GitHubResult(
                repositories: repos, pullRequests: previous,
                coverage: Coverage(errors: [error.localizedDescription]), mergedPullRequests: previousMerged ?? [])
        }
        defer { busy = false }
        var result = GitHubResult(repositories: [], pullRequests: [], coverage: Coverage())
        let mergedReserve = previousMerged == nil ? 0 : min(maximumRequests / 2, max(repos.count, maximumRequests / 3))
        let openFileLimit = maximumRequests - mergedReserve
        let previousByID = Dictionary(previous.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for original in repos {
            var repo = original
            repositoryLimit = requests + 64
            if result.authenticationRequired {
                repo.prStale = true
                repo.prInventoryComplete = false
                result.repositories.append(repo)
                continue
            }
            let cached = previous.filter { $0.repo == repo.id }
            var rows: [PullRequest] = [], cursor: String?, complete = false, expectedCount: Int?
            var inventoryError: Error?
            do {
                guard canonicalGitHubRepository(repo.id) == repo.id else { throw GitHubFailure.response }
                for _ in 0..<4 {
                    let before = requests
                    let page: GraphEnvelope
                    do { page = try await graph(repo: repo.id, cursor: cursor) } catch {
                        result.coverage.inventoryRequests += requests - before
                        throw error
                    }
                    result.coverage.inventoryRequests += requests - before
                    do { try validateGraphErrors(page.errors) } catch let error as GitHubFailure {
                        if error.isAccessDenied || error == .rateLimited { throw error }
                        if page.errors?.allSatisfy({ $0.isSupportingField }) == true {
                            result.coverage.errors.append(
                                "\(repo.id): Some pull-request fields are unavailable.")
                        } else {
                            inventoryError = error
                        }
                    }
                    guard let data = page.data?.repository, let connection = data.pullRequests,
                        let nodes = connection.nodes, let totalCount = connection.totalCount, totalCount >= 0,
                        nodes.count <= 50, expectedCount == nil || expectedCount == totalCount
                    else { throw GitHubFailure.incomplete }
                    guard canonicalGitHubRepository(data.nameWithOwner ?? "") == repo.id else {
                        throw GitHubFailure.incomplete
                    }
                    expectedCount = totalCount
                    for raw in nodes {
                        guard let raw, raw.number > 0, raw.updatedAt.flatMap(githubDate) != nil else {
                            inventoryError = GitHubFailure.incomplete
                            continue
                        }
                        if rows.contains(where: { $0.number == raw.number }) {
                            throw GitHubFailure.incomplete
                        }
                        rows.append(convert(raw, repo: repo.id))
                    }
                    guard let info = connection.pageInfo else { throw GitHubFailure.incomplete }
                    if !info.hasNextPage {
                        complete = inventoryError == nil && totalCount == rows.count
                        break
                    }
                    guard !nodes.isEmpty, let next = info.endCursor, next != cursor, !next.isEmpty else {
                        throw GitHubFailure.incomplete
                    }
                    cursor = next
                }
                if !complete { inventoryError = GitHubFailure.incomplete }
            } catch { inventoryError = error }
            if let failure = inventoryError as? GitHubFailure, failure.isAccessDenied {
                if failure == .auth { result.authenticationRequired = true }
                repo.prInventoryComplete = false
                repo.prStale = true
                result.coverage.errors.append("\(repo.id): \(failure.localizedDescription)")
                result.repositories.append(repo)
                continue
            }
            if !complete {
                let loaded = Set(rows.map(\.id))
                rows += cached.filter { !loaded.contains($0.id) }
            }
            var deniedDuringFiles = false
            for index in rows.indices {
                rows[index].inventoryComplete = complete
                if let prior = previousByID[rows[index].id], prior.filesComplete,
                    !rows[index].headOID.isEmpty, prior.headOID == rows[index].headOID,
                    prior.baseOID == rows[index].baseOID, prior.fileCount == rows[index].fileCount,
                    prior.updatedAt == rows[index].updatedAt
                {
                    rows[index].files = prior.files
                    rows[index].filesComplete = true
                }
                if !rows[index].filesComplete, !rows[index].headOID.isEmpty {
                    let before = requests
                    let inventoryLimit = repositoryLimit
                    repositoryLimit = min(inventoryLimit, openFileLimit)
                    let collection = await collectFiles(rows[index])
                    repositoryLimit = inventoryLimit
                    rows[index] = collection.0
                    if let error = collection.1 {
                        result.coverage.errors.append(
                            "\(rows[index].id) files: \(error.localizedDescription)")
                        if let failure = error as? GitHubFailure, failure.isAccessDenied {
                            deniedDuringFiles = true
                            if failure == .auth { result.authenticationRequired = true }
                        }
                    }
                    result.coverage.fileRequests += requests - before
                    if deniedDuringFiles { break }
                }
            }
            if deniedDuringFiles {
                rows = []
                complete = false
                inventoryError = GitHubFailure.permission
            }
            repo.prCheckedAt = now()
            repo.prInventoryComplete = complete
            repo.prFilesComplete = complete && rows.allSatisfy(\.filesComplete)
            repo.prReviewersComplete =
                complete && rows.allSatisfy { $0.reviewersComplete && $0.reviewsComplete }
            repo.prStale = inventoryError != nil
            if complete && (!repo.prFilesComplete || !repo.prReviewersComplete) {
                result.coverage.errors.append("\(repo.id): File or reviewer evidence is incomplete.")
            }
            if let inventoryError {
                result.coverage.errors.append("\(repo.id): \(inventoryError.localizedDescription)")
            }
            result.repositories.append(repo)
            result.pullRequests += rows
        }
        if let previousMerged, !result.authenticationRequired {
            await fetchMerged(into: &result, previous: previousMerged)
        }
        if result.authenticationRequired {
            result.pullRequests = []
            result.mergedPullRequests = []
            for index in result.repositories.indices {
                result.repositories[index].prStale = true
                result.repositories[index].prInventoryComplete = false
                result.repositories[index].mergedPRCoverage = MergedPRCoverage(stale: true)
            }
        }
        return result
    }
    private func fetchMerged(into result: inout GitHubResult, previous: [PullRequest]) async {
        let previousByRepo = Dictionary(grouping: previous, by: \.repo)
        let checkedAt = now()
        for index in result.repositories.indices {
            guard !Task.isCancelled else { return }
            var repo = result.repositories[index]
            let remainingRepositories = result.repositories.count - index
            let available = maximumRequests - requests
            let share = available / remainingRepositories + (available % remainingRepositories == 0 ? 0 : 1)
            let repositoryBudget = min(64, share)
            let limit = requests + repositoryBudget
            repositoryLimit = limit
            let cached = previousByRepo[repo.id] ?? []
            let oldByID = Dictionary(cached.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
            var rows: [PullRequest] = []
            var verified = repo.prInventoryComplete
            var coverage = MergedPRCoverage()
            let before = requests
            do {
                try await collectMergedInventory(
                    repo, checkedAt: checkedAt, fileReserve: min(6, repositoryBudget / 4),
                    rows: &rows, coverage: &coverage, verified: &verified)
            } catch {
                guard !Task.isCancelled else { return }
                if let failure = error as? GitHubFailure, failure.isAccessDenied {
                    verified = false
                    rows = []
                    coverage = MergedPRCoverage(stale: true)
                    if failure == .auth { result.authenticationRequired = true }
                } else {
                    coverage.stale = !coverage.inventoryComplete && !cached.isEmpty
                }
                if !coverage.inventoryComplete || (error as? GitHubFailure) != .budget {
                    result.coverage.errors.append("\(repo.id) merged PR history: \(error.localizedDescription)")
                }
            }
            repositoryLimit = limit
            result.coverage.mergedInventoryRequests += requests - before
            if result.authenticationRequired { break }

            for rowIndex in rows.indices {
                guard !Task.isCancelled else { return }
                var row = rows[rowIndex]
                if let prior = oldByID[row.id] {
                    retainMergedSize(from: prior, in: &row)
                    rows[rowIndex] = row
                }
                guard !row.filesComplete else { continue }
                if let prior = oldByID[row.id], prior.filesComplete,
                    !row.headOID.isEmpty, prior.headOID == row.headOID, prior.baseOID == row.baseOID,
                    prior.fileCount == row.fileCount, prior.updatedAt == row.updatedAt, prior.mergedAt == row.mergedAt
                {
                    rows[rowIndex].files = prior.files
                    rows[rowIndex].filesComplete = true
                } else if !row.headOID.isEmpty, requests < repositoryLimit {
                    let before = requests
                    let (collected, error) = await collectFiles(row)
                    result.coverage.mergedFileRequests += requests - before
                    rows[rowIndex] = collected
                    if let failure = error as? GitHubFailure, failure.isAccessDenied {
                        verified = false
                        rows = []
                        coverage = MergedPRCoverage(stale: true)
                        if failure == .auth { result.authenticationRequired = true }
                        result.coverage.errors.append("\(repo.id) merged PR files: \(failure.localizedDescription)")
                        break
                    }
                }
            }
            if rows.contains(where: { !$0.filesComplete }) {
                result.coverage.errors.append("\(repo.id): Some merged PR file paths are unavailable.")
            }
            if verified {
                let loaded = Set(rows.map(\.id))
                rows += cached.filter { prior in
                    guard !loaded.contains(prior.id), let mergedAt = prior.mergedAt, mergedAt <= checkedAt else {
                        return false
                    }
                    return !coverage.inventoryComplete || (coverage.coveredFrom.map { mergedAt < $0 } ?? false)
                }
                if !coverage.inventoryComplete, repo.mergedPRCoverage.checkedAt != nil {
                    coverage = repo.mergedPRCoverage
                    coverage.stale = true
                }
            }
            repo.mergedPRCoverage = coverage
            result.repositories[index] = repo
            result.mergedPullRequests += rows
        }
    }

    private func retainMergedSize(from prior: PullRequest, in row: inout PullRequest) {
        guard row.additions == nil || row.deletions == nil,
            !row.headOID.isEmpty, !row.baseOID.isEmpty, prior.headOID == row.headOID, prior.baseOID == row.baseOID,
            prior.mergedAt == row.mergedAt, prior.updatedAt == row.updatedAt,
            let additions = prior.additions, additions >= 0, let deletions = prior.deletions, deletions >= 0,
            row.additions == nil || row.additions == additions, row.deletions == nil || row.deletions == deletions
        else { return }
        row.additions = additions
        row.deletions = deletions
    }

    private enum MergedWindow {
        case complete(Int), capped(Int)
    }

    private func collectMergedInventory(
        _ repo: Repository, checkedAt: Date, fileReserve: Int, rows: inout [PullRequest],
        coverage: inout MergedPRCoverage, verified: inout Bool
    ) async throws {
        guard canonicalGitHubRepository(repo.id) == repo.id, !repo.branch.isEmpty else {
            throw GitHubFailure.response
        }
        let recent: TimeInterval = 15 * 24 * 60 * 60
        var upper = Date(timeIntervalSince1970: floor(checkedAt.timeIntervalSince1970) + 1)
        let recentBoundary = upper.addingTimeInterval(-recent)
        let inventoryLimit = repositoryLimit - fileReserve
        var span: TimeInterval? = recent
        var olderCount: Int?
        while true {
            let lower = span.map { upper.addingTimeInterval(-$0) }
            switch try await collectMergedWindow(repo, from: lower, before: upper, rows: &rows, verified: &verified) {
            case .capped(let count):
                if let span, span <= 1 { throw GitHubFailure.incomplete }
                if span == nil { olderCount = count }
                span = span.map { floor($0 / 2) } ?? recent
            case .complete(let count):
                coverage = MergedPRCoverage(coveredFrom: lower, checkedAt: checkedAt, inventoryComplete: true)
                guard let lower else { return }
                if lower <= recentBoundary && rows.contains(where: { !$0.filesComplete }) {
                    repositoryLimit = inventoryLimit
                }
                upper = lower
                olderCount = olderCount.map { max(0, $0 - count) }
                if let olderCount, olderCount > 1000 {
                    span = min((span ?? recent) * 2, 365 * 24 * 60 * 60)
                } else {
                    span = nil
                }
            }
        }
    }

    private func collectMergedWindow(
        _ repo: Repository, from lower: Date?, before upper: Date, rows: inout [PullRequest], verified: inout Bool
    ) async throws -> MergedWindow {
        let formatter = ISO8601DateFormatter()
        let boundary = lower.map {
            formatter.string(from: $0) + ".." + formatter.string(from: upper.addingTimeInterval(-1))
        } ?? "<" + formatter.string(from: upper)
        let branchQualifier = repo.branch.contains { $0.isWhitespace || $0 == "\"" || $0 == "\\" }
            ? "" : " base:\"\(repo.branch)\""
        let query = "repo:\(repo.id) is:pr is:merged merged:\(boundary)\(branchQualifier) sort:updated-desc"
        var cursor: String?, cursors = Set<String>(), seen = Set<Int>(), expectedCount: Int?
        while true {
            var page = try await graph(repo: repo.id, cursor: cursor, search: query)
            try validateMergedErrors(page.errors)
            var sizesBeforeRetry: [Int: RawPR] = [:], retryFailure: GitHubFailure?
            if page.errors?.contains(where: \.isMergedSizeField) == true,
                page.data?.search?.nodes?.contains(where: { $0 == nil }) != false
            {
                do {
                    let recovered = try await graph(repo: repo.id, cursor: cursor, search: query, includeMergedSizes: false)
                    try validateMergedErrors(recovered.errors)
                    let available = page.data?.search?.nodes?.compactMap { $0 } ?? []
                    sizesBeforeRetry = Dictionary(available.map { ($0.number, $0) }, uniquingKeysWith: { first, _ in first })
                    page = recovered
                } catch let failure as GitHubFailure where !failure.isAccessDenied {
                    retryFailure = failure
                }
            }
            guard let name = page.data?.repository?.nameWithOwner else { throw GitHubFailure.notFound }
            guard canonicalGitHubRepository(name) == repo.id else { throw GitHubFailure.incomplete }
            verified = true
            guard let search = page.data?.search, let count = search.issueCount, count >= 0 else {
                throw GitHubFailure.incomplete
            }
            if expectedCount == nil && count > 1000 { return .capped(count) }
            guard let nodes = search.nodes, nodes.count <= 50, let info = search.pageInfo,
                expectedCount == nil || expectedCount == count
            else { throw retryFailure ?? GitHubFailure.incomplete }
            expectedCount = count
            var complete = true
            for node in nodes {
                guard let node, node.number > 0, node.state == "MERGED",
                    canonicalGitHubRepository(node.repository?.nameWithOwner ?? "") == repo.id,
                    let branch = node.baseRefName, !branch.isEmpty,
                    let mergedAt = githubDate(node.mergedAt), mergedAt < upper,
                    githubDate(node.updatedAt) != nil,
                    lower.map({ mergedAt >= $0 }) ?? true,
                    seen.insert(node.number).inserted
                else {
                    complete = false
                    continue
                }
                guard branch == repo.branch else { continue }
                var row = convert(node, repo: repo.id)
                row.mergedAt = mergedAt
                if let previous = sizesBeforeRetry[node.number], previous.state == "MERGED",
                    previous.baseRefName == branch, canonicalGitHubRepository(previous.repository?.nameWithOwner ?? "") == repo.id
                {
                    var prior = convert(previous, repo: repo.id)
                    prior.mergedAt = githubDate(previous.mergedAt)
                    retainMergedSize(from: prior, in: &row)
                }
                row.files = Array(Set(row.files.filter { !$0.isEmpty && !$0.contains("\0") })).sorted()
                row.filesComplete = row.filesComplete && node.changedFiles == row.files.count
                row.fileCount = node.changedFiles.flatMap { $0 >= 0 ? $0 : nil } ?? row.fileCount
                rows.append(row)
            }
            guard complete, seen.count <= count else { throw retryFailure ?? GitHubFailure.incomplete }
            if !info.hasNextPage {
                guard seen.count == count else { throw GitHubFailure.incomplete }
                return .complete(count)
            }
            guard !nodes.isEmpty, let next = info.endCursor, !next.isEmpty, cursors.insert(next).inserted
            else { throw GitHubFailure.incomplete }
            cursor = next
        }
    }

    private func collectFiles(_ original: PullRequest) async -> (PullRequest, (any Error)?) {
        var row = original, cursor: String?, paths = Set<String>(), cursors = Set<String>()
        do {
            for _ in 0..<10 {
                let envelope = try await graph(repo: row.repo, cursor: cursor, number: row.number)
                try validateGraphErrors(envelope.errors)
                guard canonicalGitHubRepository(envelope.data?.repository?.nameWithOwner ?? "") == row.repo,
                    let raw = envelope.data?.repository?.pullRequest, raw.number == row.number,
                    raw.headRefOid == row.headOID, raw.baseRefOid == row.baseOID,
                    githubDate(raw.updatedAt) == row.updatedAt, let files = raw.files,
                    files.totalCount == row.fileCount, let nodes = files.nodes, nodes.count <= 100,
                    row.mergedAt == nil || raw.changedFiles == row.fileCount,
                    let info = files.pageInfo
                else { throw GitHubFailure.incomplete }
                for file in nodes {
                    guard let file, !file.path.isEmpty, !file.path.contains("\0") else {
                        throw GitHubFailure.response
                    }
                    paths.insert(file.path)
                }
                row.files = paths.sorted()
                guard paths.count <= 1000 else { throw GitHubFailure.incomplete }
                if !info.hasNextPage {
                    row.filesComplete = files.totalCount == paths.count
                    return (row, row.filesComplete ? nil : GitHubFailure.incomplete)
                }
                guard !nodes.isEmpty, let next = info.endCursor, !next.isEmpty, cursors.insert(next).inserted
                else { throw GitHubFailure.incomplete }
                cursor = next
            }
            return (row, GitHubFailure.incomplete)
        } catch { return (row, error) }
    }
    private func graph(
        repo: String, cursor: String?, number: Int? = nil, search: String? = nil, includeMergedSizes: Bool = true
    ) async throws -> GraphEnvelope {
        let components = repo.split(separator: "/")
        guard components.count == 2 else { throw GitHubFailure.response }
        var variables: [String: Any] = [
            "owner": String(components[0]), "name": String(components[1]),
            "cursor": cursor as Any? ?? NSNull(),
        ]
        if let number { variables["number"] = number }
        if let search {
            variables["search"] = search
            variables["includeSizes"] = includeMergedSizes
        }
        let query = search != nil ? Self.mergedQuery : (number == nil ? Self.inventoryQuery : Self.filesQuery)
        let body = try JSONSerialization.data(withJSONObject: [
            "query": query, "variables": variables,
        ])
        return try decode(GraphEnvelope.self, try await request("graphql", body: body))
    }
    private func validateGraphErrors(_ errors: [GraphError]?) throws {
        guard let errors, !errors.isEmpty else { return }
        let types = errors.map { ($0.type ?? "").uppercased() }
        if types.contains("RATE_LIMITED") {
            retryAt = now().addingTimeInterval(60)
            throw GitHubFailure.rateLimited
        }
        if types.contains("UNAUTHORIZED") { throw GitHubFailure.auth }
        if errors.contains(where: { $0.type?.uppercased() == "FORBIDDEN" && $0.isRepositoryFailure }) {
            throw GitHubFailure.permission
        }
        if errors.contains(where: { $0.type?.uppercased() == "NOT_FOUND" && $0.isRepositoryFailure }) {
            throw GitHubFailure.notFound
        }
        throw GitHubFailure.incomplete
    }
    private func validateMergedErrors(_ errors: [GraphError]?) throws {
        do { try validateGraphErrors(errors) } catch let failure as GitHubFailure {
            if failure.isAccessDenied || failure == .rateLimited || errors?.allSatisfy(\.isMergedSupportingField) != true {
                throw failure
            }
        }
    }
    private func convert(_ raw: RawPR, repo: String) -> PullRequest {
        var row = PullRequest(
            repo: repo, number: raw.number, title: raw.title ?? "Untitled pull request",
            author: raw.author?.login ?? "Unknown", url: "https://github.com/\(repo)/pull/\(raw.number)",
            updatedAt: githubDate(raw.updatedAt))
        row.draft = raw.isDraft ?? false
        row.review =
            ["APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED"].contains(raw.reviewDecision ?? "")
            ? raw.reviewDecision! : "UNKNOWN"
        row.mergeable =
            ["MERGEABLE", "CONFLICTING", "UNKNOWN"].contains(raw.mergeable ?? "") ? raw.mergeable! : "UNKNOWN"
        let rollup = raw.commits?.nodes?.first??.commit?.statusCheckRollup
        let check = rollup?.state ?? "UNKNOWN"
        row.checks =
            ["SUCCESS", "FAILURE", "ERROR", "PENDING", "EXPECTED"].contains(check) ? check : "UNKNOWN"
        row.createdAt = githubDate(raw.createdAt)
        row.additions = raw.additions.flatMap { $0 >= 0 ? $0 : nil }
        row.deletions = raw.deletions.flatMap { $0 >= 0 ? $0 : nil }
        row.labels = Array(
            Set(raw.labels?.nodes?.prefix(20).compactMap { $0?.name }.filter { !$0.isEmpty } ?? [])
        ).sorted()
        let contexts = rollup?.contexts
        row.failingChecks = Array(
            Set(contexts?.nodes?.prefix(50).compactMap { $0?.failingName } ?? [])
        ).sorted()
        row.failingChecksComplete =
            row.checks == "SUCCESS"
            || (contexts?.nodes != nil && contexts?.pageInfo?.hasNextPage == false)
        row.headOID = raw.headRefOid ?? ""
        row.baseOID = raw.baseRefOid ?? ""
        row.fileCount = raw.files?.totalCount ?? 0
        row.files = raw.files?.nodes?.prefix(100).compactMap { $0?.path } ?? []
        row.filesComplete =
            raw.files?.nodes != nil && (raw.files?.nodes?.count ?? 0) <= 100
            && raw.files?.pageInfo?.hasNextPage == false && raw.files?.totalCount == row.files.count
            && Set(row.files).count == row.files.count
            && row.files.allSatisfy { !$0.isEmpty && !$0.contains("\0") }
        row.reviewers =
            raw.reviewRequests?.nodes?.prefix(50)
            .compactMap { request in
                guard let reviewer = request?.requestedReviewer else { return nil }
                if let login = reviewer.login, !login.isEmpty { return login }
                if let slug = reviewer.slug, !slug.isEmpty, let organization = reviewer.organization?.login,
                    !organization.isEmpty
                {
                    return organization + "/" + slug
                }
                return nil
            } ?? []
        row.reviewersComplete =
            raw.reviewRequests?.nodes != nil && (raw.reviewRequests?.nodes?.count ?? 0) <= 50
            && raw.reviewRequests?.pageInfo?.hasNextPage == false
            && raw.reviewRequests?.totalCount == row.reviewers.count
        row.reviews =
            raw.latestReviews?.nodes?.prefix(50)
            .compactMap { review in
                guard let review, let author = review.author?.login, !author.isEmpty else { return nil }
                return Review(
                    author: author,
                    state: ["PENDING", "COMMENTED", "APPROVED", "CHANGES_REQUESTED", "DISMISSED"]
                        .contains(review.state ?? "") ? review.state! : "UNKNOWN",
                    submittedAt: githubDate(review.submittedAt))
            } ?? []
        row.reviewsComplete =
            raw.latestReviews?.nodes != nil && (raw.latestReviews?.nodes?.count ?? 0) <= 50
            && raw.latestReviews?.pageInfo?.hasNextPage == false
            && raw.latestReviews?.totalCount == row.reviews.count
        return row
    }
    private static let inventoryQuery = """
        query BigboardInventory($owner:String!,$name:String!,$cursor:String) {
          repository(owner:$owner,name:$name) {
            nameWithOwner
            pullRequests(first:50,after:$cursor,states:OPEN,orderBy:{field:UPDATED_AT,direction:DESC}) {
              totalCount pageInfo { hasNextPage endCursor }
              nodes { number title author { login } isDraft reviewDecision mergeable createdAt updatedAt additions deletions headRefOid baseRefOid
                labels(first:20) { nodes { name } }
                reviewRequests(first:50) { totalCount pageInfo { hasNextPage endCursor } nodes { requestedReviewer { __typename ... on User { login } ... on Mannequin { login } ... on Team { slug organization { login } } } } }
                latestReviews(first:50) { totalCount pageInfo { hasNextPage endCursor } nodes { author { login } state submittedAt } }
                files(first:100) { totalCount pageInfo { hasNextPage endCursor } nodes { path } }
                commits(last:1) { nodes { commit { statusCheckRollup { state contexts(first:50) { pageInfo { hasNextPage endCursor } nodes { __typename ... on CheckRun { name conclusion } ... on StatusContext { context state } } } } } } }
              }
            }
          }
        }
        """
    private static let filesQuery = """
        query BigboardFiles($owner:String!,$name:String!,$number:Int!,$cursor:String) {
          repository(owner:$owner,name:$name) { nameWithOwner pullRequest(number:$number) { number updatedAt headRefOid baseRefOid changedFiles files(first:100,after:$cursor) { totalCount pageInfo { hasNextPage endCursor } nodes { path } } } }
        }
        """
    private static let mergedQuery = """
        query BigboardMerged($owner:String!,$name:String!,$search:String!,$cursor:String,$includeSizes:Boolean!) {
          repository(owner:$owner,name:$name) { nameWithOwner }
          search(query:$search,type:ISSUE,first:50,after:$cursor) {
            issueCount pageInfo { hasNextPage endCursor }
            nodes {
              ... on PullRequest {
                repository { nameWithOwner } number title author { login } state baseRefName mergedAt updatedAt
                headRefOid baseRefOid changedFiles
                additions @include(if:$includeSizes) deletions @include(if:$includeSizes)
                files(first:100) { totalCount pageInfo { hasNextPage endCursor } nodes { path } }
              }
            }
          }
        }
        """
}
extension GitHubFailure: Equatable {}
func validGitHubLogin(_ value: String) -> Bool {
    !value.isEmpty && value.utf8.count <= 100
        && value.utf8.allSatisfy {
            (65...90).contains($0) || (97...122).contains($0) || (48...57).contains($0) || $0 == 45
                || $0 == 95
        }
}
func canonicalGitHubRepository(_ value: String) -> String? {
    let parts = value.split(separator: "/", omittingEmptySubsequences: false)
    guard parts.count == 2, !parts[0].isEmpty, !parts[1].isEmpty, parts[0].utf8.count <= 100,
        parts[1].utf8.count <= 100
    else { return nil }
    let allowed = CharacterSet(
        charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.")
    guard parts.allSatisfy({ $0 != "." && $0 != ".." && $0.unicodeScalars.allSatisfy(allowed.contains) })
    else { return nil }
    return value.lowercased()
}
private func decode<T: Decodable>(_ type: T.Type, _ data: Data) throws -> T {
    do { return try JSONDecoder().decode(type, from: data) } catch { throw GitHubFailure.response }
}
private struct PageInfo: Decodable {
    var hasNextPage: Bool
    var endCursor: String?
}
private struct Connection<T: Decodable>: Decodable {
    var totalCount: Int?
    var pageInfo: PageInfo?
    var nodes: [T?]?
}
private struct Actor: Decodable { var login: String }
private struct RawFile: Decodable { var path: String }
private struct RawReview: Decodable {
    var author: Actor?
    var state: String?
    var submittedAt: String?
}
private struct RawReviewRequest: Decodable {
    struct Reviewer: Decodable {
        var login: String?
        var slug: String?
        var organization: Actor?
    }
    var requestedReviewer: Reviewer?
}
private struct RawLabel: Decodable { var name: String? }
private struct RawCheckContext: Decodable {
    var name: String?
    var conclusion: String?
    var context: String?
    var state: String?
    var failingName: String? {
        if let name, !name.isEmpty,
            ["FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE"].contains(conclusion ?? "")
        {
            return name
        }
        if let context, !context.isEmpty, ["FAILURE", "ERROR"].contains(state ?? "") { return context }
        return nil
    }
}
private struct RawCommit: Decodable {
    struct CommitStatus: Decodable {
        struct Rollup: Decodable {
            var state: String?
            var contexts: Connection<RawCheckContext>?
        }
        var statusCheckRollup: Rollup?
    }
    var commit: CommitStatus?
}
private struct RawPR: Decodable {
    var number: Int
    var title: String?
    var author: Actor?
    var isDraft: Bool?
    var reviewDecision: String?
    var mergeable: String?
    var createdAt: String?
    var updatedAt: String?
    var mergedAt: String?
    var state: String?
    var baseRefName: String?
    var changedFiles: Int?
    var repository: GraphRepositoryName?
    var additions: Int?
    var deletions: Int?
    var labels: Connection<RawLabel>?
    var headRefOid: String?
    var baseRefOid: String?
    var files: Connection<RawFile>?
    var reviewRequests: Connection<RawReviewRequest>?
    var latestReviews: Connection<RawReview>?
    var commits: Connection<RawCommit>?
}
private enum GraphPathPart: Decodable {
    case field(String), index(Int)
    init(from decoder: any Decoder) throws {
        let value = try decoder.singleValueContainer()
        if let field = try? value.decode(String.self) {
            self = .field(field)
        } else {
            self = .index(try value.decode(Int.self))
        }
    }
}
private struct GraphError: Decodable {
    var type: String?
    var path: [GraphPathPart]?
    var isRepositoryFailure: Bool { path == nil || path!.count <= 1 }
    var isSupportingField: Bool {
        guard let path, path.count >= 5, case .field("repository") = path[0],
            case .field("pullRequests") = path[1], case .field("nodes") = path[2], case .index = path[3],
            case .field(let field) = path[4]
        else { return false }
        return [
            "files", "reviewRequests", "latestReviews", "commits", "reviewDecision", "mergeable", "labels",
            "additions", "deletions", "createdAt",
        ]
            .contains(field)
    }
    var isMergedSupportingField: Bool {
        mergedField == "files" || mergedField == "changedFiles" || isMergedSizeField
    }
    var isMergedSizeField: Bool { mergedField == "additions" || mergedField == "deletions" }
    private var mergedField: String? {
        guard let path, path.count >= 4, case .field("search") = path[0], case .field("nodes") = path[1],
            case .index = path[2], case .field(let field) = path[3]
        else { return nil }
        return field
    }
}
private struct GraphRepositoryName: Decodable { var nameWithOwner: String? }
private struct GraphRepository: Decodable {
    var nameWithOwner: String?
    var pullRequests: Connection<RawPR>?
    var pullRequest: RawPR?
}
private struct GraphEnvelope: Decodable {
    struct GraphData: Decodable {
        var repository: GraphRepository?
        var search: GraphSearch?
    }
    struct GraphSearch: Decodable {
        var issueCount: Int?
        var pageInfo: PageInfo?
        var nodes: [RawPR?]?
    }
    var data: GraphData?
    var errors: [GraphError]?
}
