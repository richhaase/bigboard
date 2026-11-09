import Foundation
import Testing
@testable import Bigboard

@MainActor struct NavigationHistoryTests {
    @Test func boardSwitchesAndDrillsAreReversible() {
        let state = BoardState()
        #expect(!state.canGoBack && !state.canGoForward)
        state.exploreHeat(ActivityScope(repo: "acme/app"))
        state.showHeatActivity(
            ActivityScope(repo: "acme/app", path: "Sources"),
            interval: DateInterval(start: Date(timeIntervalSince1970: 0), duration: 3600))
        state.showContributor("alex@example.test")
        #expect(state.board == .contributors)
        state.goBack()
        #expect(state.board == .activity && state.scope.path == "Sources" && state.dateRange.isCustomInterval)
        state.goBack()
        #expect(state.board == .heat && state.heatScope == ActivityScope(repo: "acme/app"))
        #expect(state.dateRange == .days(14))
        state.goBack()
        #expect(state.heatScope == ActivityScope() && !state.canGoBack)
        state.goForward()
        state.goForward()
        state.goForward()
        #expect(state.board == .contributors && state.contributorSelection == "alex@example.test")
        #expect(!state.canGoForward)
    }
    @Test func newNavigationClearsForwardHistory() {
        let state = BoardState()
        state.switchBoard(.activity)
        state.switchBoard(.contributors)
        state.goBack()
        #expect(state.canGoForward)
        state.switchBoard(.pullRequests)
        #expect(!state.canGoForward)
        state.goBack()
        #expect(state.board == .activity)
    }
    @Test func selectionAndRepeatedLocationsDoNotAddHistory() {
        let state = BoardState()
        state.switchBoard(.heat)
        state.exploreHeat(ActivityScope())
        #expect(!state.canGoBack)
        state.switchBoard(.activity)
        state.activitySelection = "acme/app"
        state.switchBoard(.contributors)
        state.goBack()
        #expect(state.board == .activity && state.activitySelection == "acme/app")
        state.goBack()
        #expect(state.board == .heat && !state.canGoBack)
    }
    @Test func goingBackRestoresFiltersAJumpChanged() {
        let state = BoardState()
        state.switchBoard(.activity)
        state.activity.query = "fix"
        state.activity.people = ["sam@example.test"]
        state.heat.people = ["alex@example.test"]
        state.switchBoard(.heat)
        state.showHeatActivity(ActivityScope(repo: "acme/app"), interval: nil)
        #expect(state.activity.query.isEmpty && state.activity.people == ["alex@example.test"])
        state.goBack()
        state.goBack()
        #expect(state.board == .activity)
        #expect(state.activity.query == "fix" && state.activity.people == ["sam@example.test"])
    }
    @Test func personDrillAndContextualPRsGoBack() {
        let state = BoardState()
        state.switchBoard(.activity)
        state.setLens(.people)
        state.showPerson("alex@example.test")
        #expect(state.canGoUp)
        state.goBack()
        #expect(state.scopedPerson == nil && state.lens == .people)
        state.prs.query = "release"
        state.showPRs(repo: "acme/app")
        state.showAllPRs()
        #expect(state.contextualRepository == nil && state.prs.query == "release")
        state.goBack()
        #expect(state.contextualRepository == "acme/app" && state.prs.query.isEmpty)
    }
    @Test func historyIsBoundedAndDropsRemovedRepositories() {
        let state = BoardState()
        for index in 0..<(BoardState.historyLimit + 10) {
            state.exploreHeat(ActivityScope(repo: "acme/repo\(index)"))
        }
        #expect(state.backStack.count == BoardState.historyLimit)
        state.reconcile(Snapshot(repositories: [Repository(id: "acme/repo58")]))
        #expect(state.heatScope == ActivityScope())
        #expect(state.backStack.map(\.heatScope.repo) == ["acme/repo58"])
        state.resetForAccount(WorkspaceConfiguration())
        #expect(!state.canGoBack && !state.canGoForward)
    }
}
