package git_test

import (
	"reflect"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func TestSetIdentityDisplayNameWithoutMerge(t *testing.T) {
	got, err := git.SetIdentityDisplayName(nil, " NEW@example.com ", " Display Name ")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]git.Identity{"new@example.com": {Name: "Display Name"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("display name mapping = %+v, want %+v", got, want)
	}
}

func TestSetIdentityDisplayNamePreservesIdentityGroups(t *testing.T) {
	for _, tc := range []struct {
		name, email, displayName, wantName string
	}{
		{"canonical", "primary@example.com", " New Name ", "New Name"},
		{"alias", " FIRST-OLD@example.com ", " Alias Name ", "Alias Name"},
		{"clear", "primary@example.com", "  ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := map[string]git.Identity{
				"primary@example.com": {Aliases: []string{"first-old@example.com", "second-old@example.com"}, Name: "Original Name"},
				"other@example.com":   {Aliases: []string{"other-old@example.com"}, Name: "Unrelated"},
			}
			before, err := git.NormalizeIdentities(existing)
			if err != nil {
				t.Fatal(err)
			}
			got, err := git.SetIdentityDisplayName(existing, tc.email, tc.displayName)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]git.Identity{
				"primary@example.com": {Aliases: []string{"first-old@example.com", "second-old@example.com"}, Name: tc.wantName},
				"other@example.com":   {Aliases: []string{"other-old@example.com"}, Name: "Unrelated"},
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("display name mapping = %+v, want %+v", got, want)
			}
			got["primary@example.com"].Aliases[0] = "changed@example.com"
			got["other@example.com"].Aliases[0] = "changed-other@example.com"
			if !reflect.DeepEqual(existing, before) {
				t.Fatalf("original mapping changed: %+v", existing)
			}
		})
	}
}

func TestSetIdentityDisplayNameRejectsInvalidEmail(t *testing.T) {
	for _, email := range []string{"", "not an email", "Name <valid@example.com>"} {
		if _, err := git.SetIdentityDisplayName(nil, email, "Name"); err == nil {
			t.Fatalf("invalid email %q accepted", email)
		}
	}
	existing := map[string]git.Identity{"primary@example.com": {Aliases: []string{"invalid"}}}
	if _, err := git.SetIdentityDisplayName(existing, "primary@example.com", "Name"); err == nil {
		t.Fatal("invalid existing mapping accepted")
	}
}
