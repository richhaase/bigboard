import Foundation
import Testing

@testable import Bigboard

struct GitHubLinkTests {
    @Test func commitLinksPointAtTheCommitPage() {
        let commit = Commit(
            id: "0123456789abcdef0123456789abcdef01234567", repo: "acme/widgets", author: "Ada",
            email: "ada@example.com", date: Date(timeIntervalSince1970: 0), subject: "Fix")
        #expect(commitURL(commit) == "https://github.com/acme/widgets/commit/0123456789abcdef0123456789abcdef01234567")
    }
}
