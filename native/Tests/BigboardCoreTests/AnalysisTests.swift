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
