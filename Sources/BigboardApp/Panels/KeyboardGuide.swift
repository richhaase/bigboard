import SwiftUI

struct KeyboardGuide: View {
    let commands: [(String, String)] = [
        ("⌘1 / ⌘2 / ⌘3 / ⌘4", "Heat Maps / Activity / Contributors / Pull requests"),
        ("⌘F", "Find on the current board"),
        ("⌘⇧F", "Open Filters"),
        ("⌘⇧I", "Repository Filter"),
        ("⌘⇧D", "Data Status"),
        ("⌘R", "Refresh repository evidence"),
        ("⌘,", "Workspace Settings"),
        ("⌘[", "Back in Heat Maps or Activity"),
        ("⌘⇧C", "Copy view as Markdown"),
        ("⌘Return", "Open selection on GitHub"),
        ("⌘⌥1–4", "Activity: Directories, Commits, People, Related"),
        ("⌘⌥M / ⌘⌥A / ⌘⌥R", "PRs: needs my attention / authored by me / review requested"),
        ("Return / →", "Explore the selected directory row"),
        ("Esc", "Dismiss the active popover or sheet"),
    ]
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text(
                    "The Boards menu lists shortcuts for the current board. Use Tab to move focus and Space to toggle a focused checkbox."
                )
                .foregroundStyle(.secondary)
                ForEach(commands, id: \.0) { key, action in
                    HStack {
                        Text(key).font(.body.monospaced()).frame(width: 180, alignment: .leading)
                        Text(action)
                        Spacer()
                    }
                    Divider()
                }
                Text(
                    "Heat Maps shows where and when activity occurred in the selected range. Contribution totals describe changes; they do not measure productivity. Pull-request evidence may be partial; Data status explains coverage."
                )
                .foregroundStyle(.secondary)
            }
            .padding(24)
        }
    }
}
