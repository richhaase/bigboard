package tui

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
)

func settingsRecencyModel(t *testing.T, repositories []git.Repository) Model {
	t.Helper()
	options := settingsTestOptions()
	options.Initial.PersonalAccount = "viewer"
	options.Discover = func(context.Context, SettingsConfig) ([]git.Repository, error) {
		return repositories, nil
	}
	m := settingsTestOpen(t, options)
	m, discover := applyLocal(t, m, keyboardContractKey("enter"))
	return settingsTestApplyCommand(t, m, discover)
}

func settingsRecencyRows(view string) []string {
	var rows []string
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		if strings.Contains(line, "[x]") || strings.Contains(line, "[ ]") {
			rows = append(rows, line)
		}
	}
	return rows
}

func TestSettingsRepositoryRecencyOrderAndSearchPreserveIdentity(t *testing.T) {
	now := time.Now()
	tied := now.Add(-time.Hour)
	repositories := []git.Repository{
		{ID: "viewer/unknown", Name: "viewer/unknown"},
		{ID: "viewer/tied", Name: "viewer/tied", PushedAt: tied},
		{ID: "acme/old", Name: "acme/old", PushedAt: now.Add(-72 * time.Hour)},
		{ID: "acme/unknown", Name: "acme/unknown"},
		{ID: "viewer/recent", Name: "viewer/recent", PushedAt: now.Add(-15 * time.Minute)},
		{ID: "acme/tied", Name: "acme/tied", PushedAt: tied},
	}
	original := slices.Clone(repositories)
	m := settingsRecencyModel(t, repositories)
	want := []string{"viewer/recent", "acme/tied", "viewer/tied", "acme/old", "acme/unknown", "viewer/unknown"}
	if !reflect.DeepEqual(m.settings.choices(), want) {
		t.Fatalf("repositories are not newest first with deterministic ties and unknowns last: %v", m.settings.choices())
	}
	if !reflect.DeepEqual(repositories, original) {
		t.Fatal("sorting changed the discovery provider's repository slice")
	}
	m.settings.repositories[0].Name = "draft label"
	if !reflect.DeepEqual(repositories, original) {
		t.Fatal("the repository draft still shares the discovery provider's slice")
	}
	m = keyboardContractPress(t, m, "/", "VIEWER/", "enter")
	if want := []string{"viewer/recent", "viewer/tied", "viewer/unknown"}; !reflect.DeepEqual(m.settings.choices(), want) {
		t.Fatalf("search lost recency order: %v", m.settings.choices())
	}
	before := m
	m = keyboardContractPress(t, m, "down", " ")
	if m.settings.selectedRepositories["viewer/tied"] || !before.settings.selectedRepositories["viewer/tied"] {
		t.Fatal("Space did not stage the highlighted repository independently")
	}
	for _, id := range []string{"viewer/recent", "viewer/unknown", "acme/tied", "acme/old", "acme/unknown"} {
		if !m.settings.selectedRepositories[id] {
			t.Fatalf("Space changed a different repository: %s", id)
		}
	}
}

func TestSettingsRepositoryRecencyPagingUsesVisibleFilteredRows(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			now := time.Now()
			var repositories []git.Repository
			for i := range 20 {
				pushed := now.Add(-time.Duration(20-i) * time.Hour)
				personal, organization := fmt.Sprintf("viewer/r%02d", i), fmt.Sprintf("acme/r%02d", i)
				repositories = append(repositories,
					git.Repository{ID: personal, Name: personal, PushedAt: pushed},
					git.Repository{ID: organization, Name: organization, PushedAt: pushed.Add(30 * time.Minute)},
				)
			}
			m := settingsRecencyModel(t, repositories)
			m.width, m.height = size[0], size[1]
			m = keyboardContractPress(t, m, "/", "viewer/", "enter")
			choices := m.settings.choices()
			if len(choices) != 20 || choices[0] != "viewer/r19" || choices[19] != "viewer/r00" {
				t.Fatalf("filtered list lost its recency order: %v", choices)
			}
			page := len(settingsRecencyRows(m.View()))
			if page < 1 || page >= len(choices) {
				t.Fatalf("repository viewport has an invalid row count: %d", page)
			}
			m = keyboardContractPress(t, m, "pgdown")
			if m.settings.cursor != page {
				t.Fatalf("PageDown moved %d rows when %d rows are visible", m.settings.cursor, page)
			}
			selected := choices[page]
			found := false
			for _, row := range settingsRecencyRows(m.View()) {
				found = found || strings.Contains(row, "› [x] "+selected)
			}
			if !found {
				t.Fatalf("PageDown did not render the selected repository %s", selected)
			}
			m = keyboardContractPress(t, m, " ")
			if m.settings.selectedRepositories[selected] {
				t.Fatalf("Space did not toggle the paged repository %s", selected)
			}
			m = keyboardContractPress(t, m, "pgup")
			if m.settings.cursor != 0 {
				t.Fatal("PageUp did not return to the first visible repository")
			}
			m = keyboardContractPress(t, m, "end", "pgup")
			if m.settings.cursor != len(choices)-1-page {
				t.Fatal("PageUp from the end did not use the filtered viewport size")
			}
		})
	}
}

func TestSettingsRepositoryLastPushColumnSurvivesNarrowLayouts(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}} {
		for _, tc := range []struct {
			name    string
			elapsed time.Duration
			age     string
		}{
			{name: "recent", elapsed: time.Second, age: "<1m ago"},
			{name: "hours", elapsed: 2*time.Hour + 15*time.Minute, age: "2h ago"},
			{name: "days", elapsed: 98 * time.Hour, age: "4d ago"},
			{name: "unknown", age: "—"},
		} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], tc.name), func(t *testing.T) {
				var pushed time.Time
				if tc.elapsed > 0 {
					pushed = time.Now().Add(-tc.elapsed)
				}
				id := "viewer/" + strings.Repeat("long-repository-", 6)
				m := settingsRecencyModel(t, []git.Repository{{ID: id, Name: id, PushedAt: pushed}})
				m.width, m.height = size[0], size[1]
				view := ansi.Strip(m.View())
				lines := strings.Split(view, "\n")
				if len(lines) != m.height || !strings.Contains(view, "LAST PUSH ↓") || !strings.Contains(view, "Enter next") || !strings.Contains(view, "Esc back") {
					t.Fatalf("repository checklist lost its column, controls, or frame: %s", view)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > m.width {
						t.Fatalf("repository checklist exceeded %d columns: %s", m.width, line)
					}
				}
				rows := settingsRecencyRows(view)
				if len(rows) != 1 || !strings.Contains(rows[0], "…") || !strings.Contains(rows[0], tc.age) {
					t.Fatalf("long repository name clipped its last-push age %q: %v", tc.age, rows)
				}
				ageEnd := strings.LastIndex(rows[0], tc.age) + len(tc.age)
				if trailing := strings.Trim(rows[0][ageEnd:], " │"); trailing != "" {
					t.Fatalf("last-push age is not the final repository column: %s", rows[0])
				}
			})
		}
	}
}
