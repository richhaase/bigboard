package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func statisticsText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

func TestNarrowStatisticsPreserveDetailAndPaging(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	for _, profile := range []termenv.Profile{termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(profile)
		for _, width := range []int{40, 70, 80, 100} {
			t.Run(fmt.Sprintf("profile%d/width%d", profile, width), func(t *testing.T) {
				name := "組織/very-long-repository-name-with-full-identity-🚀-é-tail"
				date := time.Date(2026, 9, 1, 12, 0, 0, 0, time.Local)
				as := stats.AuthorStats{Name: "Ada", Commits: 22345678901, Added: 323456789012, Removed: 423456789013, Net: -100000000001, AICommits: 2234567890, UnknownLineCommits: 1, ActiveDays: 31, FirstCommit: date, LastCommit: date.AddDate(0, 0, 2), PerRepo: map[string]*stats.RepoContribution{name: {Commits: 12345678901, Added: 123456789012, Removed: 223456789013, Net: -100000000001, AICommits: 1234567890, UnknownLineCommits: 1, TotalChange: 346913578025}, "second-repository": {Commits: 10000000000, Added: 200000000000, Removed: 200000000000, AICommits: 1000000000, TotalChange: 400000000000}}}
				records := []git.CommitRecord{{Author: "Ada", Date: date, RepoName: name, LinesUnknown: true, AIAssisted: true}}
				m := Model{viewMode: ViewOperative, activeOperative: "Ada", authors: []stats.AuthorStats{as}, allRecords: records, timeIdx: len(TimePresets) - 1, width: width, height: 16}
				raw := m.operativeDetailContent()
				for _, line := range strings.Split(raw, "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatalf("raw line exceeds %d: %q", width, line)
					}
				}
				wanted := []string{"22,345,678,901", "323,456,789,012?", "423,456,789,013?", "2234567890", "second-repository", name, "COMMITS", "12,345,678,901", "ADDED", "123,456,789,012?", "REMOVED", "223,456,789,013?", "NET", "-100,000,000,001?", "AI", "9%", "ACTIVE31days", "FIRST2026-09-01", "LAST2026-09-03", "CHURN", "Linetotalsareincomplete(shallowhistory).", "Sep2026", "◆1", "ACTIVITYMATRIX"}
				var pages strings.Builder
				collected := 0
				for {
					view := m.View()
					for _, line := range strings.Split(view, "\n") {
						if ansi.StringWidth(line) > width {
							t.Fatalf("view line exceeds %d: %q", width, line)
						}
					}
					// Only collect the viewport body; the two pinned footer rows are not
					// part of a wrapped value and would otherwise interrupt its text.
					lines := strings.Split(ansi.Strip(view), "\n")
					end := min(len(lines), m.height-2)
					start := max(0, collected-m.statDetailOffset)
					pages.WriteString(strings.Join(lines[start:end], "\n"))
					collected = m.statDetailOffset + end
					old := m.statDetailOffset
					next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
					m = next.(Model)
					if old == m.statDetailOffset {
						break
					}
				}
				for _, want := range wanted {
					if !strings.Contains(statisticsText(raw), statisticsText(want)) {
						t.Errorf("raw detail lost %q", want)
					}
					if !strings.Contains(statisticsText(pages.String()), statisticsText(want)) {
						t.Errorf("paged detail lost %q", want)
					}
				}
			})
		}
	}
}

func TestRepoStatisticsWideDynamicColumns(t *testing.T) {
	as := &stats.AuthorStats{PerRepo: map[string]*stats.RepoContribution{"工具/engine": {Commits: 123456789012, Added: 123456789012, Removed: 223456789013, Net: -100000000001, TotalChange: 346913578025, AICommits: 12345678901}}}
	for _, width := range []int{40, 70, 80, 100, 160} {
		out := OperativeView{}.renderRepoBreakdown(as, width)
		for _, line := range strings.Split(out, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
		}
		for _, want := range []string{"工具/engine", "123,456,789,012", "223,456,789,013", "-100,000,000,001", "9%"} {
			if !strings.Contains(statisticsText(out), statisticsText(want)) {
				t.Fatalf("width %d lost %q", width, want)
			}
		}
	}
}
