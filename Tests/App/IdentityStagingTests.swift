import Foundation
import Testing
@testable import Bigboard

struct IdentityStagingTests {
    @Test func editingDropsUncheckedAliases() {
        var rules = ["a@test": IdentityRule(aliases: ["b@test", "c@test"], name: "A")]
        IdentityStaging.stage(["a@test", "b@test"], primary: "a@test", name: "A", replacing: "a@test", into: &rules)
        #expect(rules == ["a@test": IdentityRule(aliases: ["b@test"], name: "A")])
    }
    @Test func editingCanChangePrimary() {
        var rules = ["a@test": IdentityRule(aliases: ["b@test"], name: "")]
        IdentityStaging.stage(["a@test", "b@test"], primary: "b@test", name: " B ", replacing: "a@test", into: &rules)
        #expect(rules == ["b@test": IdentityRule(aliases: ["a@test"], name: "B")])
    }
    @Test func newIdentityMergesOverlappingRules() {
        var rules = [
            "a@test": IdentityRule(aliases: ["b@test"], name: "A"),
            "x@test": IdentityRule(aliases: ["y@test"], name: "X"),
        ]
        IdentityStaging.stage(["b@test", "c@test"], primary: "c@test", name: "C", replacing: nil, into: &rules)
        #expect(rules["c@test"] == IdentityRule(aliases: ["a@test", "b@test"], name: "C"))
        #expect(rules["a@test"] == nil)
        #expect(rules["x@test"] == IdentityRule(aliases: ["y@test"], name: "X"))
    }
}
