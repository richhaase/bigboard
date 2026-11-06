import Foundation
import Darwin

struct IdentityRule: Codable, Sendable, Equatable {
    var aliases: [String] = []
    var name: String = ""
}
struct WorkArea: Codable, Sendable, Equatable {
    var name: String
    var paths: [String]
    static func canonicalPrefix(_ path: String) -> String {
        path.hasSuffix("/") ? String(path.dropLast()) : path
    }
}
struct WorkspaceConfiguration: Codable, Sendable, Equatable {
    var repositories: [String] = []
    var groups: [String: [String]] = [:]
    var identities: [String: IdentityRule] = [:]
    var workAreas: [String: [WorkArea]] = [:]
    var exclude: [String] = []
    var allFiles = false
    var aiIdentities: [String] = []
    var botIdentities: [String] = []
    var sinceDays = 14
    var contributorSort = "recent"
    var requestBudget = 128
    var cachedOnly = false
    var refreshIntervalMinutes = 15
    var sourceRoots: [String] = []
}
extension WorkspaceConfiguration {
    func collected(_ repositories: [Repository], group: String = "") -> [Repository] {
        let configured = Set(self.repositories)
        let members = group.isEmpty ? nil : Set(groups[group] ?? [])
        return repositories.filter { repo in
            if !configured.contains(repo.id) { return false }
            if let members, !members.contains(repo.id) { return false }
            let name = String(repo.id.split(separator: "/").last ?? "")
            return !exclude.contains { pattern in
                fnmatch(pattern, repo.id, 0) == 0 || fnmatch(pattern, name, 0) == 0
            }
        }
    }
}
struct FileChange: Codable, Sendable, Equatable {
    var path: String
    var previousPath: String = ""
    var added = 0
    var removed = 0
    var binary = false
    var excludedReason: String = ""
}
struct Commit: Codable, Sendable, Identifiable, Equatable {
    var id: String
    var repo: String
    var author: String
    var email: String
    var date: Date
    var subject: String
    var added = 0
    var removed = 0
    var ai = false
    var bot = false
    var linesUnknown = false
    var pathsUnknown = false
    var files: [FileChange] = []
    var identity: String {
        email.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            ? "\(repo):\(author)" : email.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    }
    var change: Int { added + removed }
}
struct Repository: Sendable, Identifiable, Equatable {
    var id: String
    var branch = "main"
    var revision = ""
    var pushedAt: Date? = nil
    var scannedAt: Date? = nil
    var stale = false
    var error = ""
    var prCheckedAt: Date? = nil
    var prInventoryComplete = false
    var prFilesComplete = false
    var prReviewersComplete = false
    var prStale = false
    var path = ""
    var remote = "origin"
    var shallow = false
    var partial = false
    var skippedCommits = 0
}
struct Review: Sendable, Equatable {
    var author: String
    var state: String
    var submittedAt: Date?
}
struct PullRequest: Sendable, Identifiable, Equatable {
    var repo: String
    var number: Int
    var title: String
    var author: String
    var url: String
    var updatedAt: Date?
    var createdAt: Date? = nil
    var additions: Int? = nil
    var deletions: Int? = nil
    var labels: [String] = []
    var failingChecks: [String] = []
    var failingChecksComplete = false
    var draft = false
    var review = "UNKNOWN"
    var checks = "UNKNOWN"
    var mergeable = "UNKNOWN"
    var headOID = ""
    var baseOID = ""
    var files: [String] = []
    var fileCount = 0
    var reviewers: [String] = []
    var reviews: [Review] = []
    var inventoryComplete = false
    var filesComplete = false
    var reviewersComplete = false
    var reviewsComplete = false
    var id: String { "\(repo)#\(number)" }
}
struct Coverage: Sendable, Equatable {
    var phase = "initial"
    var message = ""
    var errors: [String] = []
    var inventoryRequests = 0
    var fileRequests = 0
}
struct Snapshot: Sendable {
    var generatedAt = Date()
    var accountID = ""
    var viewer = ""
    var repositories: [Repository] = []
    var commits: [Commit] = []
    var pullRequests: [PullRequest] = []
    var coverage = Coverage()
    var cachedOnly = false
}
struct ActivityScope: Hashable, Sendable {
    var repo = ""
    var path = ""
    var isFile = false
    var areaName = ""
    func contains(_ candidate: String) -> Bool {
        path.isEmpty || (isFile ? candidate == path : candidate.hasPrefix(path + "/"))
    }
}
struct AuthStatus: Sendable {
    var signedIn = false
    var login = ""
    var accountID = ""
    var error = ""
}
enum BigboardError: LocalizedError, Sendable {
    case message(String)
    var errorDescription: String? {
        switch self {
        case .message(let value): value
        }
    }
}
