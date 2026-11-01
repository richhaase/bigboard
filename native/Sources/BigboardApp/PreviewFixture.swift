import Foundation

@MainActor enum PreviewFixture {
    static func make(now: Date = Date()) -> WorkspaceModel {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("Bigboard-preview-\(UUID().uuidString)")
        let workspace = WorkspaceModel(storage: WorkspaceStorage(supportRoot: root.appendingPathComponent("Settings"), cacheRoot: root.appendingPathComponent("Cache")), preview: true)
        let ids = ["example/atlas", "example/desktop", "example/platform", "example/docs", "example/sdk", "example/website"]
        let authors = [("Alex Morgan", "alex@example.test"), ("Sam Rivera", "sam@example.test"), ("Taylor Chen", "taylor@example.test"), ("Jordan Lee", "jordan@example.test"), ("Dependency Bot", "dependency-bot@example.test")]
        let paths = ["Sources/Workspace/Store.swift", "Sources/Workspace/Sync.swift", "Sources/Views/Activity.swift", "Sources/Views/Contributors.swift", "Tests/WorkspaceTests.swift", "Documentation/README.md"]
        let subjects = ["Keep workspace selections across refresh", "Load repository evidence in batches", "Improve the activity inspector", "Add contribution calendar", "Cover incomplete review evidence", "Document workspace setup"]
        var records: [Commit] = []
        for day in 0..<270 {
            for repositoryIndex in ids.indices where (day + repositoryIndex) % (repositoryIndex + 2) == 0 {
                let count = day < 28 && repositoryIndex < 2 ? 4 : 1
                for offset in 0..<count {
                    let person = authors[(day + repositoryIndex + offset) % authors.count]
                    let index = (day + offset) % paths.count
                    let added = 12 + (day + offset * 13) % 130
                    let removed = 2 + (day + repositoryIndex) % 30
                    let files = [FileChange(path: paths[index], added: added, removed: removed), FileChange(path: paths[(index + 1) % paths.count], added: 3, removed: 1)]
                    records.append(Commit(id: String(format: "%040x", day * 100 + repositoryIndex * 10 + offset + 1), repo: ids[repositoryIndex], author: person.0, email: person.1, date: now.addingTimeInterval(-Double(day) * 86400 - Double(offset + 1) * 1800), subject: subjects[index], added: added + 3, removed: removed + 1, ai: offset % 3 == 1, bot: person.0 == "Dependency Bot", files: files))
                }
            }
        }
        let repositories = ids.map { Repository(id: $0, revision: String(repeating: "a", count: 40), pushedAt: now.addingTimeInterval(-3600), scannedAt: now, prCheckedAt: now, prInventoryComplete: true, prFilesComplete: true, prReviewersComplete: true) }
        let titles = ["Preserve local scope during refresh", "Support nested work areas", "Avoid stale pull-request evidence", "Add review setup instructions", "Refine the repository client", "Update dependency versions"]
        let prs = ids.enumerated().map { index, repo in
            PullRequest(repo: repo, number: 120 + index, title: titles[index], author: index % 2 == 0 ? "alex" : "sam", url: "https://github.com/\(repo)/pull/\(120 + index)", updatedAt: now.addingTimeInterval(-Double(index + 1) * 7200), draft: index == 4, review: index == 1 ? "CHANGES_REQUESTED" : index == 3 ? "APPROVED" : "REVIEW_REQUIRED", checks: index == 2 ? "FAILURE" : index == 4 ? "PENDING" : "SUCCESS", mergeable: index == 2 ? "CONFLICTING" : "MERGEABLE", files: [paths[index]], fileCount: 1, reviewers: index % 2 == 1 ? ["alex"] : ["sam"], reviews: [Review(author: "sam", state: "COMMENTED", submittedAt: now.addingTimeInterval(-86400))], inventoryComplete: true, filesComplete: true, reviewersComplete: true, reviewsComplete: true, checkedAt: now)
        }
        workspace.snapshot = Snapshot(generatedAt: now, accountID: "preview", viewer: "alex", repositories: repositories, commits: records, pullRequests: prs, coverage: Coverage(phase: "preview", message: "Synthetic data for interface testing. No account or repository access."), bookmark: now.addingTimeInterval(-3 * 86400))
        workspace.configuration.repositories = ids
        workspace.configuration.groups = ["Applications": Array(ids.prefix(2)), "Libraries": [ids[2], ids[4]]]
        workspace.configuration.workAreas = [ids[0]: [WorkArea(name: "Workspace", paths: ["Sources/Workspace"]), WorkArea(name: "Interface", paths: ["Sources/Views"])]]
        workspace.authStatus = AuthStatus(configured: true, signedIn: true, login: "alex · Preview", accountID: "preview")
        workspace.discovered = repositories
        workspace.cachedRepositoryRows = repositories
        return workspace
    }
}
