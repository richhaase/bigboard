package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestCollectedIdentityAliasesAgreeAcrossBoards(t *testing.T) {
	var paths []string
	for i, email := range []string{"canonical@example.com", "alias@example.com"} {
		dir := t.TempDir()
		paths = append(paths, dir)
		if err := os.WriteFile(filepath.Join(dir, "main.go"), fmt.Appendf(nil, "line %d\n", i), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "-b", "main"}, {"add", "main.go"}, {"commit", "-m", "change"}} {
			args = append([]string{"-c", "user.name=Original", "-c", "user.email=" + email, "-c", "commit.gpgSign=false", "-c", "core.hooksPath=" + t.TempDir()}, args...)
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("fixture: %v\n%s", err, out)
			}
		}
	}
	options := Options{
		PRProvider:   &fakePRProvider{},
		Identities:   map[string]git.Identity{"canonical@example.com": {Aliases: []string{"alias@example.com"}, Name: "Display Name"}},
		AIIdentities: []string{"canonical@example.com"}, BotIdentities: []string{"canonical@example.com"},
	}
	m := NewModelWithOptions(git.NewRepositories(paths), stats.SortByTotal, nil, "", len(TimePresets)-1, options)
	t.Cleanup(m.Close)
	m = runRefreshCommands(t, m, m.Init())
	if len(m.authors) != 1 || m.authors[0].ID != "email:canonical@example.com" || m.authors[0].Name != "Display Name" || m.authors[0].Commits != 2 || m.authors[0].Added != 2 || m.authors[0].AICommits != 2 || !m.authors[0].Bot {
		t.Fatalf("Contributors did not use collected aliases: %+v", m.authors)
	}
	for _, row := range m.glanceRepositories() {
		if len(row.people) != 1 || row.people[0].ID != m.authors[0].ID || row.people[0].Name != "Display Name" || row.people[0].Commits != 1 || row.people[0].Added != 1 {
			t.Fatalf("Activity disagrees with Contributors: %+v", row.people)
		}
	}
	m.activityPeople = filterValues{"email:canonical@example.com"}
	if len(m.activityRecords()) != 2 {
		t.Fatal("Activity contributor filter lost an alias")
	}
	m.activityHideBots = true
	if len(m.activityRecords()) != 0 {
		t.Fatal("Activity bot filter ignored canonical identity")
	}
}
