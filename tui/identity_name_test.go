package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
)

func TestIdentityDisplayNameReviewsAndSavesOneContributor(t *testing.T) {
	for _, aliases := range [][]string{nil, {"historic@example.com"}} {
		m := identityMergeFixture()
		m.options.Identities = map[string]git.Identity{
			"primary@example.com": {Name: "Old override", Aliases: aliases},
			"other@example.com":   {Name: "Other", Aliases: []string{"other-old@example.com"}},
		}
		before, err := git.NormalizeIdentities(m.options.Identities)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		m.options.SaveIdentities = func(identities map[string]git.Identity) error {
			calls++
			if identities["primary@example.com"].Name != "Preferred Name" || !reflect.DeepEqual(identities["primary@example.com"].Aliases, aliases) || !reflect.DeepEqual(identities["other@example.com"], before["other@example.com"]) {
				t.Fatalf("name-only save changed email mappings: %+v", identities)
			}
			return nil
		}
		m = keyboardContractPress(t, m, "m", "n", "ctrl+u", "Preferred Name", "enter", "enter")
		if !m.identityMerge.review || m.identityMerge.selectedCount() != 1 || calls != 0 || !reflect.DeepEqual(m.options.Identities, before) {
			t.Fatal("name edit required a merge or saved before confirmation")
		}
		if m.currentHelp().title != "Review display name" {
			t.Fatal("name-only review described a merge")
		}
		m, cmd := applyLocal(t, m, key("enter"))
		if cmd == nil || !m.identityMerge.saving {
			t.Fatal("confirmation did not schedule name saving")
		}
		m, _ = applyLocal(t, m, cmd())
		if calls != 1 || m.identityMerge.open || m.options.Identities["primary@example.com"].Name != "Preferred Name" || !m.scan.localOnly || m.identityReselect != "email:primary@example.com" {
			t.Fatal("saved name did not use the existing local refresh path")
		}
		m.Close()
	}
}

func TestIdentityDisplayNameClearCancelAndFailure(t *testing.T) {
	m := identityMergeFixture()
	m.options.Identities = map[string]git.Identity{"primary@example.com": {Name: "Old override", Aliases: []string{"historic@example.com"}}}
	m = keyboardContractPress(t, m, "m", "n", "ctrl+u", "discarded", "esc")
	if m.identityMerge.name != "Old override" || m.identityMerge.nameEdited {
		t.Fatal("canceling the name editor changed the draft")
	}
	m = keyboardContractPress(t, m, "n", "ctrl+u", "enter", "enter")
	if !m.identityMerge.review || m.identityMerge.pending["primary@example.com"].Name != "" || !reflect.DeepEqual(m.identityMerge.pending["primary@example.com"].Aliases, []string{"historic@example.com"}) {
		t.Fatal("clearing the name changed aliases or retained the old override")
	}
	calls := 0
	m.options.SaveIdentities = func(map[string]git.Identity) error {
		calls++
		return errors.New("read-only config")
	}
	m, cmd := applyLocal(t, m, key("enter"))
	m, _ = applyLocal(t, m, cmd())
	if calls != 1 || !m.identityMerge.review || m.identityMerge.saving || !strings.Contains(m.identityMerge.error, "Not saved") || m.options.Identities["primary@example.com"].Name != "Old override" {
		t.Fatal("failed name save lost the draft or changed configuration")
	}
	m = keyboardContractPress(t, m, "esc", "esc")
	if m.identityMerge.open || m.options.Identities["primary@example.com"].Name != "Old override" {
		t.Fatal("canceling name changes modified the applied override")
	}
}

func TestIdentityDisplayNameReviewFitsAndExplainsNameOnlySave(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 36}} {
		m := identityMergeFixture()
		m.width, m.height = size[0], size[1]
		m = keyboardContractPress(t, m, "m", "n", "Display Name", "enter", "enter")
		view := ansi.Strip(m.View())
		for _, want := range []string{"Review display name", "Name: Display Name", "Name only", "Enter save name"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%v missing %q:\n%s", size, want, view)
			}
		}
		if strings.Contains(view, "Enter merge and save") || len(strings.Split(view, "\n")) != m.height {
			t.Fatal("name-only review lost its frame or offered a merge")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > m.width {
				t.Fatal("name-only review overflowed")
			}
		}
	}
}

func TestIdentityPrimaryChangeLoadsConfiguredNameUnlessEdited(t *testing.T) {
	for _, edited := range []bool{false, true} {
		m := identityMergeFixture()
		m.options.Identities = map[string]git.Identity{
			"primary@example.com": {Name: "First"}, "alias@example.com": {Name: "Second"},
		}
		m = keyboardContractPress(t, m, "m")
		want := "Second"
		if edited {
			m = keyboardContractPress(t, m, "n", "ctrl+u", "Custom", "enter")
			want = "Custom"
		}
		m = selectMergeEmail(t, m, "alias@example.com")
		m = keyboardContractPress(t, m, "k")
		if m.identityMerge.name != want {
			t.Fatalf("primary change name = %q, want %q", m.identityMerge.name, want)
		}
	}
}
