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
    var schemaVersion = 1
    var repositories: [String] = []
    var excludedRepositories: [String] = []
    var groups: [String: [String]] = [:]
    var identities: [String: IdentityRule] = [:]
    var workAreas: [String: [WorkArea]] = [:]
    var exclude: [String] = []
    var allFiles = false
    var aiIdentities: [String] = []
    var botIdentities: [String] = []
    var sinceDays = 14
    var contributorSort = "change"
    var requestBudget = 128
    var cachedOnly = false
    var refreshIntervalMinutes = 15
    var sourceRoots: [String] = []
}
extension WorkspaceConfiguration {
    func collected(_ repositories: [Repository], group: String = "") -> [Repository] {
        let configured = Set(self.repositories)
        let excluded = Set(excludedRepositories)
        let members = group.isEmpty ? nil : Set(groups[group] ?? [])
        return repositories.filter { repo in
            if !configured.contains(repo.id) || excluded.contains(repo.id) { return false }
            if let members, !members.contains(repo.id) { return false }
            let name = String(repo.id.split(separator: "/").last ?? "")
            return !exclude.contains { pattern in
                fnmatch(pattern, repo.id, 0) == 0 || fnmatch(pattern, name, 0) == 0
            }
        }
    }
    enum CodingKeys: String, CodingKey {
        case schemaVersion, repositories, excludedRepositories, groups, identities, workAreas, exclude
        case allFiles, aiIdentities, botIdentities, sinceDays, contributorSort, requestBudget, cachedOnly
        case refreshIntervalMinutes, sourceRoots
    }

    init(from decoder: any Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        schemaVersion = try values.decode(Int.self, forKey: .schemaVersion)
        repositories = try values.decode([String].self, forKey: .repositories)
        excludedRepositories = try values.decode([String].self, forKey: .excludedRepositories)
        groups = try values.decode([String: [String]].self, forKey: .groups)
        identities = try values.decode([String: IdentityRule].self, forKey: .identities)
        workAreas = try values.decode([String: [WorkArea]].self, forKey: .workAreas)
        exclude = try values.decode([String].self, forKey: .exclude)
        allFiles = try values.decode(Bool.self, forKey: .allFiles)
        aiIdentities = try values.decode([String].self, forKey: .aiIdentities)
        botIdentities = try values.decode([String].self, forKey: .botIdentities)
        sinceDays = try values.decode(Int.self, forKey: .sinceDays)
        contributorSort = try values.decode(String.self, forKey: .contributorSort)
        requestBudget = try values.decode(Int.self, forKey: .requestBudget)
        cachedOnly = try values.decode(Bool.self, forKey: .cachedOnly)
        refreshIntervalMinutes = try values.decodeIfPresent(Int.self, forKey: .refreshIntervalMinutes) ?? 15
        sourceRoots = try values.decodeIfPresent([String].self, forKey: .sourceRoots) ?? []
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
struct Repository: Codable, Sendable, Identifiable, Equatable {
    var id: String
    var branch = "main"
    var revision = ""
    var pushedAt: Date? = nil
    var scannedAt: Date? = nil
    var stale = false
    var error = ""
    var included = true
    var prCheckedAt: Date? = nil
    var prInventoryComplete = false
    var prFilesComplete = false
    var prReviewersComplete = false
    var prStale = false
    var path = ""
    var remote = "origin"
    var shallow = false
    var partial = false
    var name: String { id }
}
extension Repository {
    enum CodingKeys: String, CodingKey {
        case id, branch, revision, pushedAt, scannedAt, stale, error, included, prCheckedAt
        case prInventoryComplete, prFilesComplete, prReviewersComplete, prStale, path, remote, shallow, partial
    }
    init(from decoder: any Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        id = try values.decode(String.self, forKey: .id)
        branch = try values.decodeIfPresent(String.self, forKey: .branch) ?? "main"
        revision = try values.decodeIfPresent(String.self, forKey: .revision) ?? ""
        pushedAt = try values.decodeIfPresent(Date.self, forKey: .pushedAt)
        scannedAt = try values.decodeIfPresent(Date.self, forKey: .scannedAt)
        stale = try values.decodeIfPresent(Bool.self, forKey: .stale) ?? false
        error = try values.decodeIfPresent(String.self, forKey: .error) ?? ""
        included = try values.decodeIfPresent(Bool.self, forKey: .included) ?? true
        prCheckedAt = try values.decodeIfPresent(Date.self, forKey: .prCheckedAt)
        prInventoryComplete = try values.decodeIfPresent(Bool.self, forKey: .prInventoryComplete) ?? false
        prFilesComplete = try values.decodeIfPresent(Bool.self, forKey: .prFilesComplete) ?? false
        prReviewersComplete = try values.decodeIfPresent(Bool.self, forKey: .prReviewersComplete) ?? false
        prStale = try values.decodeIfPresent(Bool.self, forKey: .prStale) ?? false
        path = try values.decodeIfPresent(String.self, forKey: .path) ?? ""
        remote = try values.decodeIfPresent(String.self, forKey: .remote) ?? "origin"
        shallow = try values.decodeIfPresent(Bool.self, forKey: .shallow) ?? false
        partial = try values.decodeIfPresent(Bool.self, forKey: .partial) ?? false
    }
}
struct Review: Codable, Sendable, Equatable {
    var author: String
    var state: String
    var submittedAt: Date?
}
struct PullRequest: Codable, Sendable, Identifiable, Equatable {
    var repo: String
    var number: Int
    var title: String
    var author: String
    var url: String
    var updatedAt: Date?
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
    var checkedAt: Date? = nil
    var id: String { "\(repo)#\(number)" }
}
struct Coverage: Codable, Sendable, Equatable {
    var phase = "initial"
    var message = ""
    var errors: [String] = []
    var probeRequests = 0
    var inventoryRequests = 0
    var fileRequests = 0
    var accountRequests = 0
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
    var configured = false
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
