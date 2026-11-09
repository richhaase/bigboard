import Foundation
import Testing
@testable import Bigboard

@MainActor struct BoardJumpTests {
    @Test func contributorJumpClearsFiltersThatHideThePerson() {
        let state = BoardState()
        state.contributors.query = "sam"
        state.contributors.people = ["sam@example.test"]
        state.showPRs(repo: "acme/app")
        state.showContributor("alex@example.test")
        #expect(state.board == .contributors)
        #expect(state.contributorSelection == "alex@example.test")
        #expect(state.contributors.query.isEmpty)
        #expect(state.contributors.people.isEmpty)
        #expect(state.contextualRepository == nil)
    }
    @Test func contributorJumpKeepsFiltersThatIncludeThePerson() {
        let state = BoardState()
        state.contributors.people = ["alex@example.test", "sam@example.test"]
        state.contributors.hideBots = true
        state.showContributor("alex@example.test")
        #expect(state.contributors.people == ["alex@example.test", "sam@example.test"])
        #expect(state.contributors.hideBots)
        state.revealContributor()
        #expect(state.contributors.people.isEmpty && !state.contributors.hideBots)
    }
    @Test func activityJumpScopesPersonAndClearsStaleState() {
        let state = BoardState()
        state.activity.query = "old"
        state.activity.people = ["sam@example.test"]
        state.activityPeopleQuery = "sam"
        state.lens = .related
        state.relatedEvidence = RelatedEvidence(mode: "commits", values: ["x"])
        state.contributors.hideBots = true
        state.showActivity(ActivityScope(repo: "acme/app", path: "Sources"), person: "alex@example.test")
        #expect(state.board == .activity)
        #expect(state.scope == ActivityScope(repo: "acme/app", path: "Sources"))
        #expect(state.activity.query.isEmpty && state.activityPeopleQuery.isEmpty)
        #expect(state.activity.people.isEmpty)
        #expect(state.activity.hideBots)
        #expect(state.lens == .people)
        #expect(state.scopedPerson == "alex@example.test")
        #expect(state.relatedEvidence == nil)
    }
}
