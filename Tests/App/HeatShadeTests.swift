import Foundation
import Testing
@testable import Bigboard

struct HeatShadeTests {
    @Test func stepsFollowTheSquareRootOfTheShareOfThePeak() {
        #expect(HeatShade.step(0, maximum: 100) == 0)
        #expect(HeatShade.step(5, maximum: 0) == 0)
        #expect(HeatShade.step(1, maximum: 100) == 1)
        #expect(HeatShade.step(6, maximum: 100) == 1)
        #expect(HeatShade.step(7, maximum: 100) == 2)
        #expect(HeatShade.step(25, maximum: 100) == 2)
        #expect(HeatShade.step(26, maximum: 100) == 3)
        #expect(HeatShade.step(57, maximum: 100) == 4)
        #expect(HeatShade.step(100, maximum: 100) == 4)
        #expect(HeatShade.step(500, maximum: 100) == 4)
    }
    @Test func legendBoundsMatchTheSteps() {
        let maximum = 43_413
        for step in 1...HeatShade.steps {
            let bound = HeatShade.lowerBound(step, maximum: maximum)
            #expect(HeatShade.step(bound, maximum: maximum) == step)
            if bound > 1 { #expect(HeatShade.step(bound - 1, maximum: maximum) == step - 1) }
        }
    }
    @Test func aQuietRowUsesItsOwnPeak() {
        #expect(HeatShade.step(411, maximum: 43_413) == 1)
        #expect(HeatShade.step(411, maximum: 411) == 4)
    }
    @Test @MainActor func sharedScaleIsOffByDefaultAndResetsWithTheAccount() {
        let state = BoardState()
        #expect(!state.heatSharedScale)
        state.heatSharedScale = true
        state.resetForAccount(WorkspaceConfiguration())
        #expect(!state.heatSharedScale)
    }
}
