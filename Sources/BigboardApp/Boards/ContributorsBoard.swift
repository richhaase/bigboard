import SwiftUI
import AppKit
import UniformTypeIdentifiers

struct ContributorsBoard: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    var records: [Commit] { navigation.filteredCommits(workspace.snapshot, for: .contributors) }
    var people: [PersonRow] { navigation.peopleRows(workspace.snapshot) }
    var selected: PersonRow? { people.first { $0.id == navigation.contributorSelection } ?? people.first }
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                BoardSearch(navigation: navigation)
                PeopleFilter(records: workspace.snapshot.commits, filters: $navigation.contributors)
            }
            .padding(.horizontal, 12).padding(.vertical, 10)
            HStack {
                Picker("Sort", selection: $navigation.contributors.sort) {
                    Text("Lines changed").tag("change")
                    Text("Commits").tag("commits")
                    Text("Additions").tag("added")
                    Text("Removals").tag("removed")
                    Text("Net lines").tag("net")
                    Text("AI share").tag("ai")
                    Text("Name").tag("name")
                    Text("Recent").tag("recent")
                }
                .frame(width: 210)
                Button {
                    navigation.contributors.reversed.toggle()
                } label: {
                    Image(systemName: "arrow.up.arrow.down")
                }
                Spacer()
                Button("Identities…", systemImage: "person.crop.circle.badge.checkmark") {
                    navigation.panel = .identities
                }
                Button("Export JSON…", systemImage: "square.and.arrow.up") { export() }
            }
            .padding(.horizontal, 12).padding(.bottom, 10)
            HSplitView {
                List(people, selection: $navigation.contributorSelection) { person in
                    HStack {
                        Image(systemName: person.bot ? "gearshape.2" : "person.crop.circle.fill")
                            .font(.title2).foregroundStyle(.tint)
                        VStack(alignment: .leading, spacing: 4) {
                            Text(person.name).fontWeight(.medium)
                            Text("\(person.totals.commits) commits · \(person.repos.count) repositories")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Text(person.totals.change.formatted()).monospacedDigit()
                    }
                    .padding(.vertical, 6).tag(person.id)
                }
                .frame(minWidth: 280, idealWidth: 380)
                ScrollView {
                    if let person = selected {
                        detail(person).padding(20)
                    } else {
                        EmptyBoard(
                            title: "No contributors match",
                            message: "Adjust the search, dates, or bot filter.")
                    }
                }
                .frame(minWidth: 430)
            }
        }
    }
    private func detail(_ person: PersonRow) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Image(systemName: "person.crop.circle.fill").font(.system(size: 48)).foregroundStyle(.tint)
                VStack(alignment: .leading) {
                    Text(person.name).font(.largeTitle.bold())
                    Text(person.email.isEmpty ? "No email recorded" : person.email)
                        .foregroundStyle(.secondary).textSelection(.enabled)
                }
            }
            HStack {
                MetricView(title: "Commits", value: person.totals.commits.formatted())
                MetricView(title: "Repositories", value: "\(person.repos.count)")
                MetricView(
                    title: "Active days",
                    value: "\(Set(person.records.map { Calendar.current.startOfDay(for: $0.date) }).count)")
            }
            DeltaView(
                added: person.totals.added, removed: person.totals.removed, unknown: person.totals.unknown)
            HStack {
                Text("AI assisted \(Int(person.totals.aiPercent))%")
                Text("Net \(person.totals.net)")
                Text(
                    "Churn \(person.totals.added == 0 ? "0.00" : String(format: "%.2f", Double(person.totals.removed) / Double(person.totals.added)))"
                )
            }
            .font(.caption).foregroundStyle(.secondary)
            SectionTitle(title: "Contribution rhythm", detail: navigation.rangeLabel)
            ActivityTimeline(points: navigation.timelinePoints(person.records, snapshot: workspace.snapshot))
            ActivityCalendar(
                records: person.records,
                days: 0,
                since: navigation.dateRange.start(relativeTo: workspace.snapshot.generatedAt),
                now: workspace.snapshot.generatedAt,
                rangeLabel: navigation.rangeLabel
            )
            SectionTitle(title: "Where work happened")
            ForEach(
                Analysis.activityRows(
                    person.records, repositories: navigation.visibleRepositories(workspace.snapshot)
                )
                .filter { $0.totals.commits > 0 }
            ) { row in
                Button {
                    navigation.explore(row.scope)
                    navigation.board = .activity
                } label: {
                    HStack {
                        Text(row.label)
                        Spacer()
                        DeltaView(added: row.totals.added, removed: row.totals.removed)
                    }
                    .padding(.vertical, 5)
                }
                .buttonStyle(.plain)
            }
            SectionTitle(title: "Recent commits")
            CommitList(records: person.records)
        }
    }
    private func export() {
        let panel = NSSavePanel()
        panel.allowedContentTypes = [.json]
        panel.nameFieldStringValue = "bigboard-contributors.json"
        panel.title = "Export contributor statistics"
        guard panel.runModal() == .OK, let url = panel.url else { return }
        do { try workspace.exportJSON().write(to: url, options: .atomic) } catch {
            workspace.error = error.localizedDescription
        }
    }
}
