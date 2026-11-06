import Foundation
import Testing
@testable import Bigboard

@MainActor struct ViewGroupTests {
    let available = [Repository(id: "acme/app"), Repository(id: "acme/lib"), Repository(id: "acme/docs")]
    let groups = ["Product": ["acme/app", "acme/lib", "acme/gone"], "Docs": ["acme/docs"], "Missing": ["acme/gone"]]

    @Test func choosingAGroupOnlyNarrowsTheViewingChecklist() {
        let state = BoardState()
        #expect(state.showGroup(groups["Product"], available: available))
        #expect(state.repositories == ["acme/app", "acme/lib"])
        #expect(state.activeGroup(groups, available: available) == "Product")
        #expect(state.visibleRepositories(Snapshot(repositories: available)).map(\.id) == ["acme/app", "acme/lib"])
        #expect(state.showGroup(nil, available: available))
        #expect(state.repositories.isEmpty && state.activeGroup(groups, available: available) == nil)
    }
    @Test func groupsWithoutCollectedRepositoriesAreUnavailable() {
        let state = BoardState()
        state.repositories = ["acme/docs"]
        #expect(!state.groupIsAvailable(groups["Missing"]!, available: available))
        #expect(!state.showGroup(groups["Missing"], available: available))
        #expect(state.repositories == ["acme/docs"])
        #expect(state.activeGroup(groups, available: available) == "Docs")
    }
    @Test func customSelectionHasNoActiveGroup() {
        let state = BoardState()
        state.repositories = ["acme/app"]
        #expect(state.activeGroup(groups, available: available) == nil)
        #expect(state.repositoryFilterLabel == "1 selected")
    }
}
