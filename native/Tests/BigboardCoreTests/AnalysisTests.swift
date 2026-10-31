import Foundation
import Testing
@testable import Bigboard

struct AnalysisTests {
    let now = Date(timeIntervalSince1970: 1_793_440_800)
    func commit(_ id: String, repo: String = "o/a", email: String = "a@test", days: Double = 0, added: Int = 10, removed: Int = 2, files: [FileChange] = []) -> Commit {
        Commit(id: id, repo: repo, author: email, email: email, date: now.addingTimeInterval(-days * 86400), subject: id, added: added, removed: removed, files: files)
    }
    @Test func sharedCommitsPreferCompleteThenLexicalRepo() {
        var unknown = commit("same", repo: "o/a"); unknown.linesUnknown = true
        let known = commit("same", repo: "o/z")
        #expect(Analysis.unique([unknown, known]).first?.repo == "o/z")
        #expect(Analysis.unique([known, commit("same", repo: "o/b")]).first?.repo == "o/b")
        let totals = Analysis.totals([known, known, commit("other", added: 3)])
        #expect(totals.commits == 2 && totals.added == 13 && totals.removed == 4)
    }
    @Test func emptyIdsAreNotDeduplicatedAndMissingEmailIsRepoScoped() {
        let rows = [commit("", repo: "a/a", email: ""), commit("", repo: "a/b", email: "")]
        #expect(Analysis.totals(rows).commits == 2)
        #expect(Analysis.people(rows).count == 2)
    }
    @Test func sharedMissingEmailCommitsKeepCanonicalContributor() {
        let rows = Analysis.people([commit("shared", repo: "o/a", email: ""), commit("shared", repo: "o/b", email: ""), commit("separate", repo: "o/b", email: "")])
        #expect(rows.count == 2)
        let shared = rows.first { $0.id == "o/a:" }
        #expect(shared?.totals.commits == 1)
        #expect(shared?.repos == ["o/a", "o/b"])
        #expect(rows.first { $0.id == "o/b:" }?.totals.commits == 1)
    }
    @Test func peopleRetainSharedRepoAssociations() {
        let rows = Analysis.people([commit("same", repo: "o/b"), commit("same"), commit("other", repo: "o/c")])
        #expect(rows.count == 1)
        #expect(rows[0].totals.commits == 2)
        #expect(rows[0].repos == ["o/a", "o/b", "o/c"])
    }
    @Test func scopeUsesLiteralPathBoundaryAndExcludesGenerated() {
        let records = [commit("one", files: [FileChange(path: "src/a.swift", added: 3, removed: 1), FileChange(path: "src2/b.swift", added: 90), FileChange(path: "src/out.swift", added: 70, excludedReason: "attribute")])]
        let scoped = Analysis.scoped(records, scope: ActivityScope(repo: "o/a", path: "src"))
        #expect(scoped.count == 1 && scoped[0].added == 3 && scoped[0].removed == 1)
        #expect(scoped[0].files.map(\.path) == ["src/a.swift"])
        #expect(Analysis.scoped(records, scope: ActivityScope(repo: "o/a", path: "src/a", isFile: true)).isEmpty)
        let rows = Analysis.activityRows(records, repositories: [], scope: ActivityScope(repo: "o/a"))
        #expect(Set(rows.map(\.label)) == ["src", "src2"])
    }
    @Test func workAreaRowsAndHeatExcludeSiblingFiles() throws {
        let commits = [commit("one", files: [FileChange(path: "src/core/a.swift", added: 3), FileChange(path: "src/ui/b.swift", added: 100), FileChange(path: "tools/codegen.swift", added: 7)])]
        let scope = ActivityScope(repo: "o/a", areaName: "Engine")
        let areas = ["o/a": [WorkArea(name: "Engine", paths: ["src/core", "tools"])]]
        let rows = Analysis.activityRows(commits, repositories: [], scope: scope, workAreas: areas)
        let source = try #require(rows.first { $0.label == "src" })
        #expect(source.totals.added == 3 && source.scope.areaName == "Engine")
        let children = Analysis.activityRows(commits, repositories: [], scope: source.scope, workAreas: areas)
        #expect(children.map(\.label) == ["core"])
        #expect(children.first?.scope.areaName == "Engine")
        let files = Analysis.scoped(commits, scope: ActivityScope(repo: "o/a", path: "src/core/a.swift", isFile: true, areaName: "Engine"), workAreas: areas)
        #expect(files.count == 1 && files[0].files.map(\.path) == ["src/core/a.swift"])
        #expect(files[0].added == 3)
        let heat = Analysis.heatRows(commits, repositories: [], scope: source.scope, workAreas: areas, now: now)
        #expect(heat.count == 1 && heat[0].heat.total == 3)
    }
    @Test func historicalFileAndDirectoryScopesKeepDistinctEvidence() throws {
        for prefix in ["", "src/"] {
            let path = prefix + "component"
            let newer = commit("new", added: 100, removed: 1, files: [FileChange(path: path, removed: 1), FileChange(path: path + "/new.swift", added: 100)])
            let older = commit("old", days: 1, added: 1, removed: 0, files: [FileChange(path: path, added: 1)])
            let parent = ActivityScope(repo: "o/a", path: String(prefix.dropLast()))
            for records in [[newer, older], [older, newer]] {
                let rows = Analysis.activityRows(records, repositories: [], scope: parent)
                #expect(rows.count == 2)
                #expect(Set(rows.map(\.id)) == ["file:" + path, "directory:" + path])
                let file = try #require(rows.first { $0.kind == "file" })
                let directory = try #require(rows.first { $0.kind == "directory" })
                #expect(file.totals.added == 1 && file.totals.removed == 1)
                #expect(Set(file.records.map(\.id)) == ["new", "old"])
                #expect(directory.totals.added == 100 && directory.totals.removed == 0)
                #expect(directory.records.map(\.id) == ["new"])
                #expect(directory.records.flatMap(\.files).map(\.path) == [path + "/new.swift"])
                #expect(rows.reduce(0) { $0 + $1.totals.change } == Analysis.totals(records).change)
                #expect(Analysis.activityRows(records, repositories: [], scope: file.scope).isEmpty)
                #expect(Analysis.activityRows(records, repositories: [], scope: directory.scope).map(\.id) == ["file:" + path + "/new.swift"])
                let heat = Analysis.heatRows(records, repositories: [], scope: parent, now: now)
                #expect(heat.map(\.id) == [directory.id])
                #expect(heat.first?.heat.total == 100)
            }
        }
    }
    @Test func directoryRowsPreserveScopedEvidenceAcrossMixedHistory() throws {
        var known = commit("shared", days: 5, files: [
            FileChange(path: "src/core/a.swift", added: 5, removed: 1),
            FileChange(path: "src/core/b.swift", added: 7, removed: 2),
            FileChange(path: "src/core/image.bin", binary: true),
            FileChange(path: "src/core/deeper/test.swift", added: 8),
            FileChange(path: "src/core/generated.swift", added: 900, excludedReason: "attribute"),
            FileChange(path: "src/core2/neighbor.swift", added: 90),
            FileChange(path: "src/ui/window.swift", added: 10),
            FileChange(path: "src/direct.swift", previousPath: "old/direct.swift", added: 4),
            FileChange(path: "src/legacy", removed: 3),
            FileChange(path: "src/legacy/new.swift", added: 11),
            FileChange(path: "src2/miss.swift", added: 91),
            FileChange(path: "tools/tool.swift", added: 6)
        ])
        known.ai = true; known.bot = true
        var incompleteCopy = known; incompleteCopy.linesUnknown = true
        var unknownLines = commit("unknown-lines", days: 2, files: [FileChange(path: "src/core/a.swift", added: 777)])
        unknownLines.linesUnknown = true
        var unknownPaths = commit("unknown-paths", files: [FileChange(path: "src/unknown/file.swift", added: 999)])
        unknownPaths.pathsUnknown = true
        let unnamed = commit("", days: 3, files: [FileChange(path: "src/core/a.swift", added: 2)])
        let unnamedLater = commit("", days: 1, files: [FileChange(path: "src/core/a.swift", added: 2)])
        let records = [incompleteCopy, unknownPaths, known, unknownLines, unnamed, unnamedLater, commit("other-repo", repo: "o/b", files: known.files), commit("empty")]
        let areas = ["o/a": [WorkArea(name: "Sources", paths: ["src/"]), WorkArea(name: "Engine", paths: ["src/core/", "tools/"]), WorkArea(name: "Deep", paths: ["src/core/deeper"])]]
        let cases: [(ActivityScope, Set<String>)] = [
            (ActivityScope(repo: "o/a"), ["directory:src", "directory:src2", "directory:tools"]),
            (ActivityScope(repo: "o/a", path: "src"), ["directory:src/core", "directory:src/core2", "directory:src/ui", "directory:src/legacy", "file:src/direct.swift", "file:src/legacy"]),
            (ActivityScope(repo: "o/a", areaName: "Engine"), ["directory:src", "directory:tools"]),
            (ActivityScope(repo: "o/a", path: "src", areaName: "Engine"), ["directory:src/core"]),
            (ActivityScope(repo: "o/a", path: "src/core", areaName: "Engine"), ["file:src/core/a.swift", "file:src/core/b.swift", "file:src/core/image.bin"])
        ]
        for (scope, ids) in cases {
            let rows = Analysis.activityRows(records, repositories: [], scope: scope, workAreas: areas)
            #expect(Set(rows.map(\.id)) == ids)
            for row in rows {
                let expected = Analysis.scoped(records, scope: row.scope, workAreas: areas)
                #expect(row.records == expected)
                #expect(row.totals == Analysis.totals(expected))
                #expect(row.latest == expected.map(\.date).max())
                #expect(row.scope.areaName == scope.areaName)
            }
        }
        let core = try #require(Analysis.activityRows(records, repositories: [], scope: ActivityScope(repo: "o/a", path: "src", areaName: "Engine"), workAreas: areas).first)
        #expect(core.records.map(\.id) == ["shared", "shared", "unknown-lines", "", ""])
        #expect(core.totals.commits == 4 && core.totals.added == 16 && core.totals.removed == 3 && core.totals.unknown)
        #expect(core.latest == unnamedLater.date)
        #expect(core.records[1].ai && core.records[1].bot && core.records[1].files.contains { $0.binary })
    }
    @Test func wideDirectoryRowsAndHeatRetainOneScopedRecordPerCommit() throws {
        let count = 2048
        let initialFiles = (0..<count).flatMap { index in [FileChange(path: "src/unit\(index)/a.swift", added: 1), FileChange(path: "src/unit\(index)/b.swift", added: 2)] }
        let updatedFiles = stride(from: 0, to: count, by: 2).map { FileChange(path: "src/unit\($0)/a.swift", removed: 1) }
        let records = [commit("initial", days: 2, files: initialFiles), commit("update", days: 1, files: updatedFiles)]
        let scope = ActivityScope(repo: "o/a", path: "src")
        let rows = Analysis.activityRows(records, repositories: [], scope: scope)
        #expect(rows.count == count && Set(rows.map(\.id)).count == count)
        #expect(rows.allSatisfy { $0.totals.added == 3 && $0.records.first?.files.count == 2 && $0.records.first?.id == "initial" })
        #expect(rows.reduce(0) { $0 + $1.totals.commits } == count + count / 2)
        #expect(rows.reduce(0) { $0 + $1.totals.removed } == count / 2)
        let updated = try #require(rows.first { $0.id == "directory:src/unit0" })
        #expect(updated.records.map(\.id) == ["initial", "update"])
        #expect(updated.records.map { $0.files.count } == [2, 1])
        #expect(updated.latest == records[1].date)
        let heat = Analysis.heatRows(records, repositories: [], scope: scope, now: now)
        #expect(heat.count == count)
        #expect(Set(heat.map(\.id)) == Set(rows.map(\.id)))
        #expect(heat.reduce(0) { $0 + $1.heat.total } == Double(count * 3 + count / 2))
        let files = Analysis.activityRows(records, repositories: [], scope: ActivityScope(repo: "o/a", path: "src/unit0"))
        #expect(Set(files.map(\.id)) == ["file:src/unit0/a.swift", "file:src/unit0/b.swift"])
        #expect(files.reduce(0) { $0 + $1.totals.change } == 4)
    }
    @Test func relatedPullRequestsDistinguishHistoricalFileAndDirectoryPaths() {
        let paths = ["component", "component/new.swift", "componentary/other.swift"]
        let prs = paths.enumerated().map { index, path in PullRequest(repo: "o/a", number: index + 1, title: path, author: "A", url: "", updatedAt: now, files: [path]) }
        let areas = ["o/a": [WorkArea(name: "Engine", paths: ["component"])]]
        for area in ["", "Engine"] {
            #expect(Analysis.relatedPRs(prs, scope: ActivityScope(repo: "o/a", path: "component", isFile: true, areaName: area), workAreas: areas).map(\.number) == [1])
            #expect(Analysis.relatedPRs(prs, scope: ActivityScope(repo: "o/a", path: "component", areaName: area), workAreas: areas).map(\.number) == [2])
        }
        #expect(Analysis.relatedPRs(prs, scope: ActivityScope(repo: "o/other")).isEmpty)
        #expect(Analysis.relatedPRs(prs, scope: ActivityScope(repo: "o/a")).count == 3)
    }
    @Test func longestWorkAreaPrefixWins() {
        let rules = [WorkArea(name: "All", paths: ["src/"]), WorkArea(name: "Engine", paths: ["src/core"]), WorkArea(name: "Later tie", paths: ["src/core"])]
        #expect(Analysis.classifyWorkArea("src/core/parser.swift", rules: rules) == "Engine")
        #expect(Analysis.classifyWorkArea("src/coreish.swift", rules: rules) == "All")
        #expect(Analysis.classifyWorkArea("src2/core.swift", rules: rules).isEmpty)
    }
    @Test func filterBoundaryIsStrictAndCanonicalBotAppliesAcrossCopies() {
        var bot = commit("same", repo: "o/a"); bot.bot = true
        let other = commit("same", repo: "o/z", email: "someone@test")
        #expect(Analysis.select([bot, other], hideBots: true).isEmpty)
        #expect(Analysis.select([commit("old", days: 14), commit("new", days: 13)], days: 14, now: now).map(\.id) == ["new"])
        #expect(!Analysis.inRange(nil, days: 14, now: now))
        #expect(Analysis.inRange(nil, now: now))
    }
    @Test func heatUsesPreviousWeeksBaselineAndExcludesFuture() {
        var cal = Calendar(identifier: .gregorian); cal.timeZone = TimeZone(secondsFromGMT: 0)!
        let rows = [commit("now", added: 100, removed: 0), commit("older", days: 70, added: 120, removed: 0), commit("future", days: -8, added: 1000)]
        let heat = Analysis.heatSeries(rows, now: now, calendar: cal)
        #expect(heat.series.count == 26)
        #expect(heat.recent == 25 && heat.prior == 10)
        #expect(heat.ratio == 2.5 && heat.status == "up")
        #expect(heat.total == 220 && heat.future == 1)
        #expect(heat.series.last?.usual == 10)
    }
    @Test func localDailyBucketsSpanDaylightSavingTransitions() {
        var cal = Calendar(identifier: .gregorian); cal.timeZone = TimeZone(identifier: "America/Denver")!
        let end = cal.date(from: DateComponents(year: 2026, month: 11, day: 3, hour: 12))!
        let series = Analysis.daily([], days: 4, now: end, calendar: cal)
        #expect(series.count == 5)
        #expect(series.allSatisfy { cal.component(.hour, from: $0.date) == 0 })
        #expect(zip(series, series.dropFirst()).contains { $1.date.timeIntervalSince($0.date) == 25 * 3600 })
    }
    @Test func bookmarkTimelineUsesActualSpanAndExclusiveBoundary() {
        var cal = Calendar(identifier: .gregorian); cal.timeZone = TimeZone(secondsFromGMT: 0)!
        let bookmark = now.addingTimeInterval(-45 * 86400)
        let records = [commit("before", days: 46), commit("boundary", days: 45), commit("after", days: 40), commit("recent", days: 1), commit("future", days: -8)]
        let points = Analysis.timeline(records, days: 14, since: bookmark, now: now, calendar: cal)
        let day = cal.startOfDay(for: bookmark)
        #expect(points.first?.date == cal.date(byAdding: .day, value: -(cal.component(.weekday, from: day) - 1), to: day))
        #expect(points.reduce(0) { $0 + $1.commits } == 3)
        #expect(points.filter { $0.commits > 0 }.count == 3)
        #expect(points.allSatisfy { cal.component(.weekday, from: $0.date) == 1 })
    }
    @Test func monthlyTimelineRetainsExplicitRangesBeyondOneYear() {
        var cal = Calendar(identifier: .gregorian); cal.timeZone = TimeZone(secondsFromGMT: 0)!
        let bookmark = now.addingTimeInterval(-800 * 86400)
        let records = [commit("before", days: 801), commit("boundary", days: 800), commit("old", days: 700), commit("recent", days: 1)]
        let firstMonth = cal.date(from: cal.dateComponents([.year, .month], from: bookmark))
        let points = Analysis.timeline(records, days: 14, since: bookmark, now: now, calendar: cal)
        #expect(points.first?.date == firstMonth)
        #expect(points.count > 24)
        #expect(points.reduce(0) { $0 + $1.commits } == 2)
        let explicitDays = Analysis.timeline(records, days: 800, now: now, calendar: cal)
        #expect(explicitDays.map(\.date) == points.map(\.date))
        #expect(explicitDays.map(\.commits) == points.map(\.commits))
    }
    @Test func relatedSharedEvidenceAndPeopleDiffer() {
        let records = [commit("same", repo: "o/a"), commit("same", repo: "o/b"), commit("different", repo: "o/c")]
        let shared = Analysis.related(records, scope: ActivityScope(repo: "o/a"))
        #expect(shared.map(\.scope.repo) == ["o/b"])
        let people = Analysis.related(records, scope: ActivityScope(repo: "o/a"), mode: "people")
        #expect(Set(people.map(\.scope.repo)) == ["o/b", "o/c"])
    }
    @Test func attentionRulesAreIndividualAndViewerSpecific() {
        var pr = PullRequest(repo: "o/a", number: 1, title: "Change", author: "OTHER", url: "", updatedAt: now)
        pr.reviewers = ["o/team"]
        #expect(!Analysis.needsAttention(pr, viewer: "me"))
        pr.reviewers.append("ME")
        #expect(Analysis.needsAttention(pr, viewer: "me"))
        #expect(!Analysis.needsAttention(pr, viewer: ""))
        pr.reviewers = []; pr.reviews = [Review(author: "ME", state: "COMMENTED", submittedAt: now.addingTimeInterval(-1))]
        #expect(Analysis.updatedSinceReview(pr, viewer: "me"))
        pr.author = "me"; pr.checks = "SUCCESS"; pr.review = "APPROVED"
        #expect(Analysis.needsAttention(pr, viewer: "ME"))
        pr.draft = true
        #expect(!Analysis.needsAttention(pr, viewer: "me"))
        pr.checks = "FAILURE"
        #expect(Analysis.needsAttention(pr, viewer: "me"))
    }
    @Test func prFacetsUseUnionWithinAndIntersectionBetween() {
        var a = PullRequest(repo: "o/a", number: 1, title: "Parser", author: "alice", url: "", updatedAt: now)
        a.checks = "SUCCESS"
        var b = a; b.number = 2; b.author = "bob"; b.draft = true
        var c = a; c.number = 3; c.author = "carol"
        var filters = PRFilters(); filters.authors = ["ALICE", "bob"]; filters.states = ["open"]
        #expect(Analysis.filterPRs([a,b,c], viewer: "me", filters: filters).map(\.number) == [1])
        filters.states = []; filters.query = "parser"; filters.sort = "author"; filters.reverse = true
        #expect(Analysis.filterPRs([a,b,c], viewer: "me", filters: filters).map(\.number) == [2,1])
    }
}
