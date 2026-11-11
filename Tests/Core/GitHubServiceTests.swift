import Foundation
import Testing
@testable import Bigboard

private actor ScriptedGitHub: GitHubTransport {
    var replies: [GitHubHTTPResponse]
    var requests: [URLRequest] = []
    var delay: Duration
    init(_ replies: [GitHubHTTPResponse], delay: Duration = .zero) { self.replies = replies; self.delay = delay }
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        requests.append(request)
        guard !replies.isEmpty else { throw GitHubFailure.unavailable }
        let reply = replies.removeFirst()
        if delay > .zero { try await Task.sleep(for: delay) }
        if reply.data.count > maximumBytes { throw GitHubFailure.outputLimit }
        return reply
    }
    var count: Int { requests.count }
    var first: URLRequest? { requests.first }
}
private let testNow = Date(timeIntervalSince1970: 1_800_000_000)
private final class GitHubTestClock: @unchecked Sendable {
    private let lock = NSLock()
    private var date = testNow
    func now() -> Date { lock.withLock { date } }
    func advance(by seconds: TimeInterval) { lock.withLock { date.addTimeInterval(seconds) } }
}
private func reply(_ json: String, status: Int = 200, headers: [String: String] = [:]) -> GitHubHTTPResponse { GitHubHTTPResponse(status: status, headers: headers, data: Data(json.utf8)) }
private let onePR = #"{"data":{"repository":{"nameWithOwner":"owner/repo","defaultBranchRef":{"name":"main","target":{"oid":"new-head"}},"pullRequests":{"totalCount":1,"pageInfo":{"hasNextPage":false},"nodes":[{"number":1,"title":"Fix","author":{"login":"rich"},"isDraft":false,"reviewDecision":"APPROVED","mergeable":"MERGEABLE","updatedAt":"2026-10-31T12:00:00Z","headRefOid":"head","baseRefOid":"base","files":{"totalCount":1,"pageInfo":{"hasNextPage":false},"nodes":[{"path":"src/a.swift"}]},"reviewRequests":{"totalCount":1,"pageInfo":{"hasNextPage":false},"nodes":[{"requestedReviewer":{"login":"reviewer"}}]},"latestReviews":{"totalCount":1,"pageInfo":{"hasNextPage":false},"nodes":[{"author":{"login":"reviewer"},"state":"APPROVED","submittedAt":"2026-10-30T12:00:00Z"}]},"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}]}}}}"#
@Test func pullRequestCollectionPreservesScopedCoverageAndScanRevision() async throws {
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([reply(onePR)]), now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo", revision: "scanned-head")], budget: 128, previous: [])
    let pr = try #require(result.pullRequests.first)
    #expect(pr.checks == "SUCCESS" && pr.review == "APPROVED")
    #expect(pr.inventoryComplete && pr.filesComplete && pr.reviewersComplete && pr.reviewsComplete)
    #expect(pr.files == ["src/a.swift"])
    #expect(result.repositories.first?.revision == "scanned-head")
    #expect(result.coverage.inventoryRequests == 1)
}
@Test func transientFailureRetainsPRsButAuthoritativeDenialDropsThem() async throws {
    let old = PullRequest(repo: "owner/repo", number: 1, title: "Old", author: "author", url: "https://github.com/owner/repo/pull/1", updatedAt: testNow)
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([reply("{}", status: 503), reply("{}", status: 403)]), now: { testNow })
    let transient = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [old])
    #expect(transient.pullRequests.count == 1)
    #expect(transient.repositories.first?.prInventoryComplete == false)
    let denial = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [old])
    #expect(denial.pullRequests.isEmpty)
    #expect(denial.repositories.filter { !$0.prStale }.isEmpty)
}
@Test func rateLimitPausesRequestsUntilCooldownEnds() async throws {
    let clock = GitHubTestClock()
    let network = ScriptedGitHub([reply("{}", status: 429, headers: ["retry-after": "60"]), reply(onePR)])
    let service = GitHubService(token: { "token" }, transport: network, now: clock.now)
    _ = await service.fetch([Repository(id: "owner/one"), Repository(id: "owner/two")], budget: 128, previous: [])
    clock.advance(by: 59)
    _ = await service.fetch([Repository(id: "owner/one")], budget: 128, previous: [])
    #expect(await network.count == 1)
    clock.advance(by: 1)
    let recovered = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    #expect(await network.count == 2)
    #expect(recovered.coverage.errors.isEmpty)
    #expect(recovered.pullRequests.map(\.number) == [1])
}
@Test func repositoryIdentifiersRejectPathEscape() {
    #expect(canonicalGitHubRepository("Owner/Repo") == "owner/repo")
    #expect(canonicalGitHubRepository("owner/..") == nil)
    #expect(canonicalGitHubRepository("owner/repo/path") == nil)
    #expect(canonicalGitHubRepository("owner/repo\n") == nil)
}
private func alteredPR(_ patch: (inout [String: Any]) -> Void, errors: [[String: Any]] = []) throws -> GitHubHTTPResponse {
    var document = try #require(JSONSerialization.jsonObject(with: Data(onePR.utf8)) as? [String: Any])
    var data = try #require(document["data"] as? [String: Any])
    var repository = try #require(data["repository"] as? [String: Any])
    var connection = try #require(repository["pullRequests"] as? [String: Any])
    var node = try #require((connection["nodes"] as? [[String: Any]])?.first)
    patch(&node)
    connection["nodes"] = [node]; repository["pullRequests"] = connection; data["repository"] = repository; document["data"] = data
    if !errors.isEmpty { document["errors"] = errors }
    return GitHubHTTPResponse(status: 200, headers: [:], data: try JSONSerialization.data(withJSONObject: document))
}

private func inventoryPage(_ numbers: [Int], total: Int, next: String? = nil) throws -> GitHubHTTPResponse {
    var document = try #require(JSONSerialization.jsonObject(with: Data(onePR.utf8)) as? [String: Any])
    var data = try #require(document["data"] as? [String: Any])
    var repository = try #require(data["repository"] as? [String: Any])
    var connection = try #require(repository["pullRequests"] as? [String: Any])
    let template = try #require((connection["nodes"] as? [[String: Any]])?.first)
    connection["nodes"] = numbers.map { number in
        var node = template
        node["number"] = number
        node["title"] = "Current PR \(number)"
        return node
    }
    connection["totalCount"] = total
    connection["pageInfo"] = ["hasNextPage": next != nil, "endCursor": next as Any? ?? NSNull()]
    repository["pullRequests"] = connection
    data["repository"] = repository
    document["data"] = data
    return GitHubHTTPResponse(status: 200, headers: [:], data: try JSONSerialization.data(withJSONObject: document))
}

private func graphCursor(in request: URLRequest) throws -> String? {
    let body = try #require(request.httpBody)
    let document = try #require(JSONSerialization.jsonObject(with: body) as? [String: Any])
    let variables = try #require(document["variables"] as? [String: Any])
    return variables["cursor"] as? String
}

private func mergedNode(
    _ number: Int, age: TimeInterval = 60, branch: String = "main", changedFiles: Int = 1
) -> [String: Any] {
    let formatter = ISO8601DateFormatter()
    let mergedAt = formatter.string(from: testNow.addingTimeInterval(-age))
    return [
        "number": number, "title": "Merged PR \(number)", "author": ["login": "author"],
        "repository": ["nameWithOwner": "owner/repo"], "state": "MERGED", "baseRefName": branch,
        "mergedAt": mergedAt, "updatedAt": mergedAt, "changedFiles": changedFiles,
        "headRefOid": "head", "baseRefOid": "base",
        "files": ["totalCount": 1, "pageInfo": ["hasNextPage": false], "nodes": [["path": "src/a.swift"]]],
    ]
}

private func mergedPage(
    _ nodes: [[String: Any]], count: Int? = nil, next: String? = nil
) throws -> GitHubHTTPResponse {
    let data = try JSONSerialization.data(withJSONObject: [
        "data": [
            "repository": ["nameWithOwner": "owner/repo"],
            "search": [
                "issueCount": count ?? nodes.count, "nodes": nodes,
                "pageInfo": ["hasNextPage": next != nil, "endCursor": next as Any? ?? NSNull()],
            ],
        ],
    ])
    return GitHubHTTPResponse(status: 200, headers: [:], data: data)
}

@Test func mergedInventoryPaginatesWithDateAndBranchScope() async throws {
    let network = ScriptedGitHub([
        reply(onePR),
        try mergedPage([mergedNode(2)], count: 2, next: "second-page"),
        try mergedPage([mergedNode(3, branch: "release")], count: 2),
        try mergedPage([mergedNode(4, age: 20 * 86_400)]),
    ])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 16, previous: [], previousMerged: [])

    #expect(result.pullRequests.map(\.number) == [1])
    #expect(result.mergedPullRequests.map(\.number) == [2, 4])
    #expect(result.mergedPullRequests.first?.mergedAt == testNow.addingTimeInterval(-60))
    #expect(result.mergedPullRequests.allSatisfy { $0.filesComplete })
    #expect(result.repositories.first?.mergedPRCoverage == MergedPRCoverage(checkedAt: testNow, inventoryComplete: true))
    #expect(result.coverage.inventoryRequests == 1)
    #expect(result.coverage.mergedInventoryRequests == 3)
    #expect(result.coverage.errors.isEmpty)

    let requests = await network.requests
    try #require(requests.count == 4)
    #expect(try graphCursor(in: requests[2]) == "second-page")
    let body = try #require(requests[1].httpBody)
    let document = try #require(JSONSerialization.jsonObject(with: body) as? [String: Any])
    let variables = try #require(document["variables"] as? [String: Any])
    let search = try #require(variables["search"] as? String)
    #expect(search.contains("repo:owner/repo is:pr is:merged merged:"))
    #expect(search.contains(" base:\"main\""))
    #expect(search.contains(".."))
}

@Test func cappedOlderHistoryPreservesRecentCoverageAndPreviouslyKnownMerges() async throws {
    let network = ScriptedGitHub([reply(onePR), try mergedPage([mergedNode(2)]), try mergedPage([], count: 1200)])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let older = PullRequest(
        repo: "owner/repo", number: 9, title: "Older", author: "author", url: "",
        updatedAt: testNow, mergedAt: testNow.addingTimeInterval(-20 * 86_400))
    let result = await service.fetch(
        [Repository(id: "owner/repo")], budget: 3, previous: [], previousMerged: [older])

    #expect(result.mergedPullRequests.map(\.number) == [2, 9])
    let coverage = try #require(result.repositories.first?.mergedPRCoverage)
    #expect(coverage.inventoryComplete && !coverage.stale)
    #expect(coverage.coveredFrom == testNow.addingTimeInterval(1 - 15 * 86_400))
    #expect(coverage.checkedAt == testNow)
    #expect(result.coverage.errors.isEmpty)
    #expect(await network.count == 3)
}

@Test func mergedCollectionPreservesOpenInventoryPriorityUnderTightBudget() async {
    let network = ScriptedGitHub([reply(onePR), reply(onePR.replacingOccurrences(of: "owner/repo", with: "owner/other"))])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let result = await service.fetch(
        [Repository(id: "owner/repo"), Repository(id: "owner/other")], budget: 2, previous: [], previousMerged: [])

    #expect(result.repositories.map(\.prInventoryComplete) == [true, true])
    #expect(result.coverage.inventoryRequests == 2)
    #expect(result.coverage.mergedInventoryRequests == 0)
    #expect(await network.count == 2)
}

@Test func incompleteMergedFilePathsDoNotInvalidateInventory() async throws {
    let network = ScriptedGitHub([
        reply(onePR), try mergedPage([mergedNode(2, changedFiles: 2)]), try mergedPage([]), reply("{}", status: 503),
    ])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 12, previous: [], previousMerged: [])

    let pr = try #require(result.mergedPullRequests.first)
    #expect(pr.files == ["src/a.swift"])
    #expect(pr.fileCount == 2 && !pr.filesComplete)
    #expect(result.repositories.first?.mergedPRCoverage.inventoryComplete == true)
    #expect(result.coverage.mergedFileRequests == 1)
    #expect(result.coverage.errors.contains { $0.contains("merged PR file paths") })
}

@Test func transientMergedFailurePreservesVerifiedPriorCoverageAsStale() async throws {
    let network = ScriptedGitHub([reply(onePR), reply("{}", status: 503)])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let priorCheck = testNow.addingTimeInterval(-600)
    let repo = Repository(
        id: "owner/repo", mergedPRCoverage: MergedPRCoverage(checkedAt: priorCheck, inventoryComplete: true))
    let prior = PullRequest(
        repo: repo.id, number: 2, title: "Prior merge", author: "author", url: "", updatedAt: priorCheck,
        mergedAt: priorCheck)
    let result = await service.fetch([repo], budget: 4, previous: [], previousMerged: [prior])

    #expect(result.mergedPullRequests == [prior])
    #expect(result.repositories.first?.mergedPRCoverage == MergedPRCoverage(
        checkedAt: priorCheck, inventoryComplete: true, stale: true))
}

@Test func mergedAccessDenialDropsPreviouslyKnownMerges() async throws {
    let network = ScriptedGitHub([reply(onePR), try mergedPage([mergedNode(2)]), reply("{}", status: 403)])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let older = PullRequest(
        repo: "owner/repo", number: 9, title: "Older", author: "author", url: "",
        updatedAt: testNow, mergedAt: testNow.addingTimeInterval(-20 * 86_400))
    let result = await service.fetch(
        [Repository(id: "owner/repo")], budget: 12, previous: [], previousMerged: [older])

    #expect(result.mergedPullRequests.isEmpty)
    #expect(result.pullRequests.map(\.number) == [1])
    #expect(result.repositories.first?.mergedPRCoverage == MergedPRCoverage(stale: true))
}

@Test func pullRequestInventoryFollowsCursorAndCombinesPages() async throws {
    let network = ScriptedGitHub([
        try inventoryPage([1], total: 2, next: "second-page"),
        try inventoryPage([2], total: 2)
    ])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 2, previous: [])
    #expect(result.pullRequests.map(\.number) == [1, 2])
    #expect(result.pullRequests.allSatisfy { $0.inventoryComplete })
    #expect(result.repositories.first?.prInventoryComplete == true)
    #expect(result.coverage.errors.isEmpty)
    #expect(result.coverage.inventoryRequests == 2)
    let requests = await network.requests
    try #require(requests.count == 2)
    #expect(try graphCursor(in: requests[0]) == nil)
    #expect(try graphCursor(in: requests[1]) == "second-page")
}

@Test(arguments: [false, true])
func interruptedInventoryRetainsOldEvidenceUntilCompleteRefresh(exhaustBudget: Bool) async throws {
    var replies = [try inventoryPage([1], total: 2, next: "second-page")]
    if !exhaustBudget { replies.append(reply("{}", status: 503)) }
    replies.append(try inventoryPage([1, 2], total: 2))
    let network = ScriptedGitHub(replies)
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let previous = [1, 99].map {
        PullRequest(repo: "owner/repo", number: $0, title: "Previously saved", author: "author",
                    url: "https://github.com/owner/repo/pull/\($0)", updatedAt: testNow)
    }
    let partial = await service.fetch(
        [Repository(id: "owner/repo")], budget: exhaustBudget ? 1 : 10, previous: previous)
    #expect(partial.pullRequests.map(\.number) == [1, 99])
    #expect(partial.pullRequests.first?.title == "Current PR 1")
    #expect(partial.pullRequests.allSatisfy { !$0.inventoryComplete })
    #expect(partial.repositories.first?.prInventoryComplete == false)
    #expect(partial.repositories.first?.prStale == true)
    #expect(!partial.coverage.errors.isEmpty)
    #expect(await network.count == (exhaustBudget ? 1 : 2))

    let recovered = await service.fetch(partial.repositories, budget: 10, previous: partial.pullRequests)
    #expect(recovered.pullRequests.map(\.number) == [1, 2])
    #expect(recovered.pullRequests.allSatisfy { $0.inventoryComplete })
    #expect(recovered.repositories.first?.prInventoryComplete == true)
    #expect(recovered.repositories.first?.prStale == false)
    #expect(recovered.coverage.errors.isEmpty)
    #expect(await network.count == (exhaustBudget ? 2 : 3))
}

@Test(arguments: ["headRefOid", "baseRefOid"])
func changingPullRequestRevisionCannotCombineFilePages(changedField: String) async throws {
    let inventory = try alteredPR {
        $0["files"] = ["totalCount": 2, "nodes": [], "pageInfo": ["hasNextPage": true]]
    }
    func filePage(path: String, next: String?, changed: Bool) throws -> GitHubHTTPResponse {
        var node: [String: Any] = [
            "number": 1, "headRefOid": "head", "baseRefOid": "base", "updatedAt": "2026-10-31T12:00:00Z",
            "files": ["totalCount": 2, "nodes": [["path": path]],
                      "pageInfo": ["hasNextPage": next != nil, "endCursor": next as Any? ?? NSNull()]]
        ]
        if changed { node[changedField] = "changed-revision" }
        let data = try JSONSerialization.data(withJSONObject: [
            "data": ["repository": ["nameWithOwner": "owner/repo", "pullRequest": node]]
        ])
        return GitHubHTTPResponse(status: 200, headers: [:], data: data)
    }
    let network = ScriptedGitHub([
        inventory,
        try filePage(path: "src/original.swift", next: "more-files", changed: false),
        try filePage(path: "src/changed.swift", next: nil, changed: true)
    ])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 10, previous: [])
    let pr = try #require(result.pullRequests.first)
    #expect(pr.files == ["src/original.swift"])
    #expect(!pr.filesComplete && pr.inventoryComplete)
    #expect(result.repositories.first?.prFilesComplete == false)
    #expect(result.repositories.first?.prInventoryComplete == true)
    #expect(!result.coverage.errors.isEmpty)
    #expect(result.coverage.fileRequests == 2)
    let requests = await network.requests
    try #require(requests.count == 3)
    #expect(try graphCursor(in: requests[2]) == "more-files")
}

@Test func supportingFieldDenialDoesNotRevokeRepositoryOrInventory() async throws {
    let partial = try alteredPR({ $0["reviewRequests"] = NSNull() }, errors: [["type": "FORBIDDEN", "path": ["repository", "pullRequests", "nodes", 0, "reviewRequests"]]])
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([partial]), now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    #expect(result.repositories.first?.prInventoryComplete == true)
    #expect(result.repositories.first?.prReviewersComplete == false)
    #expect(result.pullRequests.count == 1)
    #expect(!result.coverage.errors.isEmpty)
}
@Test func nullCheckRollupStateIsUnknownWithoutDroppingPR() async throws {
    let partial = try alteredPR { $0["commits"] = ["nodes": [["commit": ["statusCheckRollup": ["state": NSNull()]]]]] }
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([partial]), now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    #expect(result.pullRequests.first?.checks == "UNKNOWN")
    #expect(result.repositories.first?.prInventoryComplete == true)
}
@Test func missingInventoryNodesIsNotCompleteZero() async throws {
    let malformed = reply(#"{"data":{"repository":{"nameWithOwner":"owner/repo","pullRequests":{"totalCount":0,"pageInfo":{"hasNextPage":false}}}}}"#)
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([malformed]), now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    #expect(result.repositories.first?.prInventoryComplete == false)
    #expect(!result.coverage.errors.isEmpty)
}
@Test func fileLimitRetainsThousandPathsAndIncompleteCoverage() async throws {
    var responses = [try alteredPR { $0["files"] = ["totalCount": 1001, "pageInfo": ["hasNextPage": true, "endCursor": "initial"], "nodes": [["path": "file0"]]] }]
    for page in 0..<10 {
        let raw: [String: Any] = ["number": 1, "headRefOid": "head", "baseRefOid": "base", "updatedAt": "2026-10-31T12:00:00Z", "files": ["totalCount": 1001, "pageInfo": ["hasNextPage": true, "endCursor": "page\(page)"], "nodes": (0..<100).map { ["path": "file\(page * 100 + $0)"] }]]
        responses.append(GitHubHTTPResponse(status: 200, headers: [:], data: try JSONSerialization.data(withJSONObject: ["data": ["repository": ["nameWithOwner": "owner/repo", "pullRequest": raw]]])))
    }
    let network = ScriptedGitHub(responses)
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    #expect(result.pullRequests.first?.files.count == 1000)
    #expect(result.pullRequests.first?.filesComplete == false)
    #expect(result.repositories.first?.prInventoryComplete == true)
    #expect(result.coverage.fileRequests == 10)
    #expect(!result.coverage.errors.isEmpty)
    #expect(await network.count == 11)
}
@Test func authenticationDenialBlocksRemainingRepositoryRequests() async throws {
    let network = ScriptedGitHub([reply("{}", status: 401)])
    let service = GitHubService(token: { "token" }, transport: network, now: { testNow })
    let result = await service.fetch([Repository(id: "owner/one"), Repository(id: "owner/two")], budget: 128, previous: [])
    #expect(result.authenticationRequired)
    #expect(result.repositories.map(\.id) == ["owner/one", "owner/two"] && result.repositories.filter { !$0.prStale }.isEmpty)
    #expect(await network.count == 1)
}
@Test func pullRequestDetailsIncludeAgeSizeLabelsAndFailingChecks() async throws {
    let detailed = try alteredPR {
        $0["createdAt"] = "2026-10-01T12:00:00Z"
        $0["additions"] = 120
        $0["deletions"] = 30
        $0["labels"] = ["nodes": [["name": "bug"], ["name": "ui"], ["name": "bug"]]]
        $0["commits"] = ["nodes": [["commit": ["statusCheckRollup": [
            "state": "FAILURE",
            "contexts": [
                "pageInfo": ["hasNextPage": false],
                "nodes": [
                    ["__typename": "CheckRun", "name": "test", "conclusion": "FAILURE"],
                    ["__typename": "CheckRun", "name": "lint", "conclusion": "SUCCESS"],
                    ["__typename": "StatusContext", "context": "ci/legacy", "state": "ERROR"],
                    ["__typename": "CheckRun", "name": "deploy", "conclusion": "TIMED_OUT"],
                ],
            ],
        ]]]]]
    }
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([detailed]), now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    let pr = try #require(result.pullRequests.first)
    #expect(pr.createdAt == githubDate("2026-10-01T12:00:00Z"))
    #expect(pr.additions == 120 && pr.deletions == 30)
    #expect(pr.labels == ["bug", "ui"])
    #expect(pr.checks == "FAILURE")
    #expect(pr.failingChecks == ["ci/legacy", "deploy", "test"])
    #expect(pr.failingChecksComplete)
}
@Test func missingPullRequestDetailsStayUnknown() async throws {
    let service = GitHubService(token: { "token" }, transport: ScriptedGitHub([reply(onePR)]), now: { testNow })
    let result = await service.fetch([Repository(id: "owner/repo")], budget: 128, previous: [])
    let pr = try #require(result.pullRequests.first)
    #expect(pr.createdAt == nil && pr.additions == nil && pr.deletions == nil)
    #expect(pr.labels.isEmpty && pr.failingChecks.isEmpty)
    #expect(pr.failingChecksComplete)
}
