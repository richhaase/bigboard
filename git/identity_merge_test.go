package git_test

import (
	"reflect"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func TestMergeIdentitiesPreservesGroupsAndUnrelatedMappings(t *testing.T) {
	existing := map[string]git.Identity{
		"primary@example.com": {Aliases: []string{"primary-old@example.com"}, Name: "Primary Name"},
		"second@example.com":  {Aliases: []string{"second-old@example.com"}, Name: "Secondary Name"},
		"other@example.com":   {Aliases: []string{"other-old@example.com"}, Name: "Unrelated"},
	}
	before, err := git.NormalizeIdentities(existing)
	if err != nil {
		t.Fatal(err)
	}
	got, err := git.MergeIdentities(existing, " PRIMARY@example.com ", []string{"primary@example.com", "second@example.com", "third@example.com"}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]git.Identity{
		"primary@example.com": {Aliases: []string{"primary-old@example.com", "second-old@example.com", "second@example.com", "third@example.com"}, Name: "Primary Name"},
		"other@example.com":   {Aliases: []string{"other-old@example.com"}, Name: "Unrelated"},
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(existing, before) {
		t.Fatalf("merged=%+v original=%+v", got, existing)
	}
	again, err := git.MergeIdentities(got, "other@example.com", []string{"primary@example.com", "other@example.com"}, " Combined Name ")
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again["other@example.com"].Name != "Combined Name" || len(again["other@example.com"].Aliases) != 6 {
		t.Fatalf("second merge lost transitive aliases: %+v", again)
	}
}

func TestMergeIdentitiesRejectsAmbiguousSelections(t *testing.T) {
	existing := map[string]git.Identity{"first@example.com": {Aliases: []string{"alias@example.com"}}}
	for _, tc := range []struct {
		name, primary string
		emails        []string
	}{
		{"missing primary", "", []string{"first@example.com", "second@example.com"}},
		{"unselected primary", "third@example.com", []string{"first@example.com", "second@example.com"}},
		{"missing email", "first@example.com", []string{"first@example.com", ""}},
		{"name selection", "first@example.com", []string{"first@example.com", "Same Name"}},
		{"one identity", "first@example.com", []string{"first@example.com"}},
		{"same group", "first@example.com", []string{"first@example.com", "alias@example.com"}},
		{"case duplicate", "first@example.com", []string{"first@example.com", " FIRST@example.com "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := git.MergeIdentities(existing, tc.primary, tc.emails, ""); err == nil {
				t.Fatal("ambiguous merge accepted")
			}
		})
	}
}
