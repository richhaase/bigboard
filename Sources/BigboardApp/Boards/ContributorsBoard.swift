import SwiftUI

struct ContributorsBoard: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    var records: [Commit] { navigation.filteredCommits(workspace.snapshot, for: .contributors) }
    var people: [PersonRow] { navigation.peopleRows(workspace.snapshot) }
    var selected: PersonRow? {
        guard let id = navigation.contributorSelection else { return people.first }
        return people.first { $0.id == id }
    }
    var hiddenName: String? {
        guard let id = navigation.contributorSelection, selected == nil else { return nil }
        return Analysis.people(workspace.snapshot.commits).first { $0.id == id }?.name ?? id
    }
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
            }
            .padding(.horizontal, 12).padding(.bottom, 10)
            HSplitView {
                let largest = people.map(\.totals.change).max() ?? 0
                List(people, selection: $navigation.contributorSelection) { person in
                    HStack(alignment: .top) {
                        Image(systemName: person.bot ? "gearshape.2" : "person.crop.circle.fill")
                            .font(.title2).foregroundStyle(.tint)
                        VStack(alignment: .leading, spacing: 4) {
                            HStack(alignment: .firstTextBaseline) {
                                Text(person.name).fontWeight(.medium).lineLimit(1)
                                Spacer()
                                DeltaView(
                                    added: person.totals.added, removed: person.totals.removed,
                                    unknown: person.totals.unknown, font: .system(.caption, design: .monospaced))
                            }
                            HStack {
                                Text(
                                    "\(person.totals.commits.formatted()) \(person.totals.commits == 1 ? "commit" : "commits") · \(person.repos.count) \(person.repos.count == 1 ? "repository" : "repositories")"
                                )
                                Spacer()
                                Text("net \(person.totals.net.formatted(.number.sign(strategy: .always())))")
                                    .monospacedDigit()
                            }
                            .font(.caption).foregroundStyle(.secondary)
                            ImpactBar(
                                added: person.totals.added, removed: person.totals.removed, maximum: largest)
                        }
                    }
                    .padding(.vertical, 6).tag(person.id)
                }
                .frame(minWidth: 280, idealWidth: 380)
                ScrollView {
                    if let person = selected {
                        detail(person).padding(20)
                    } else if let name = hiddenName {
                        hidden(name)
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
                    navigation.showActivity(row.scope, person: person.id)
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
    private func hidden(_ name: String) -> some View {
        let filtered =
            !navigation.contributors.query.isEmpty || !navigation.contributors.people.isEmpty
            || navigation.contributors.hideBots
        return ContentUnavailableView {
            Label("\(name) is not shown", systemImage: "person.crop.circle.badge.questionmark")
        } description: {
            Text(
                filtered
                    ? "The current search or people filters hide this contributor."
                    : "This contributor has no commits in \(navigation.rangeLabel.lowercased()) for the selected repositories."
            )
        } actions: {
            if filtered { Button("Show \(name)") { navigation.revealContributor() } }
        }
    }
}
