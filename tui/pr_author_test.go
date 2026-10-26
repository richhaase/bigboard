package tui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestPRRowsReserveAuthorAtEveryWidth(t *testing.T) {
	for _, width := range []int{40, 80, 110, 120, 160, 180} {
		for _, author := range []string{"ada", strings.Repeat("long-user-", 8), strings.Repeat("界", 40), ""} {
			m := prFixture()
			m.width, m.height = width, 36
			pr := m.visiblePRs()[0]
			pr.Author, pr.Title = author, "A readable change title"
			pr.ReviewDecision, pr.CheckState = "CHANGES_REQUESTED", "FAILURE"
			rows := m.prTableRow(pr, strings.Repeat("long-repository-", 8), true, 0, width)
			plain := ansi.Strip(strings.Join(rows, "\n"))
			if !strings.Contains(plain, "Changes req.") || !strings.Contains(plain, "Failing") {
				t.Fatalf("%d author column displaced status fields:\n%s", width, plain)
			}
			if author == "" {
				if !strings.Contains(plain, "unavailable") {
					t.Fatalf("%d missing author lost its explicit label", width)
				}
			} else if strings.Count(plain, "@") != 1 || !strings.Contains(plain, "@"+string([]rune(author)[0])) {
				t.Fatalf("%d author is hidden or duplicated:\n%s", width, plain)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > width || !utf8.ValidString(row) {
					t.Fatalf("%d author row overflowed or split UTF-8", width)
				}
			}
			if width >= 110 {
				l := resolvePRTable(width)
				start := 4 + l.repo + 1 + l.number + 1 + l.state + 1
				if got := ansi.Cut(plain, start, start+l.author); got != padCells(githubIdentity(author), l.author) {
					t.Fatalf("%d author did not occupy its column: %q", width, got)
				}
				if got := strings.TrimSpace(ansi.Cut(ansi.Strip(prTableColumns(width)), start, start+l.author)); got != "AUTHOR" || l.title < 24 {
					t.Fatalf("%d author heading misaligned or title space reduced too far", width)
				}
			}
		}
	}
}

func TestPRAuthorSortAndFiltersMatchVisibleColumn(t *testing.T) {
	m := prOnlySelectionFixture()
	snapshot := m.prState.snapshots["org/repo"]
	for i, author := range []string{"Zulu", "ada", "Mira"} {
		snapshot.prs[i].Author = author
	}
	m.prState.snapshots["org/repo"] = snapshot
	m = keyboardContractPress(t, m, "p")
	selected := m.selectedPRKey()
	m = keyboardContractPress(t, m, "s", "s")
	assertAuthors := func(want []string) {
		t.Helper()
		prs := m.visiblePRs()
		if len(prs) != len(want) {
			t.Fatalf("wanted %d authors, got %d", len(want), len(prs))
		}
		rows := m.renderPRBoardRows(prs, m.width)
		for i, author := range want {
			if prs[i].Author != author || !strings.Contains(ansi.Strip(rows[2+i]), "@"+author) {
				t.Fatalf("row %d did not display sorted author %s", i, author)
			}
		}
		if m.selectedPRKey() != selected {
			t.Fatal("author sorting/filtering moved a retained selection")
		}
	}
	if m.prSortLabel() != "Author ↑" {
		t.Fatal("s did not expose author sorting")
	}
	assertAuthors([]string{"ada", "Mira", "Zulu"})
	m = keyboardContractPress(t, m, "S")
	if m.prSortLabel() != "Author ↓" {
		t.Fatal("S did not reverse author sorting")
	}
	assertAuthors([]string{"Zulu", "Mira", "ada"})
	m = chooseFilterOption(t, m, "author", "zulu")
	m = chooseFilterOption(t, m, "author", "ada")
	m = keyboardContractPress(t, m, "enter")
	assertAuthors([]string{"Zulu", "ada"})
}

func TestPRAuthorTableKeepsFullBoardInsideTerminal(t *testing.T) {
	for _, width := range []int{40, 80, 110, 120, 160, 180} {
		m := boardLayoutFixture()
		m.width, m.height = width, 36
		for repo, snapshot := range m.prState.snapshots {
			for i := range snapshot.prs {
				snapshot.prs[i].Author = fmt.Sprintf("author-%d-%s", i, strings.Repeat("界username", 12))
			}
			m.prState.snapshots[repo] = snapshot
		}
		m = keyboardContractPress(t, m, "p", "s", "s", "end")
		lines := strings.Split(m.View(), "\n")
		if len(lines) != m.height {
			t.Fatalf("%d author table changed screen height", width)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("%d author table exceeded terminal width", width)
			}
		}
	}
}
