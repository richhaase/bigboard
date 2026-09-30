package stats

import (
	"reflect"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func areaRecord(id string, paths ...string) git.CommitRecord {
	r := git.CommitRecord{CommitID: id, Author: "Alex", Email: "alex@example.test", RepoID: "/repo"}
	for _, p := range paths {
		r.Changes = append(r.Changes, git.PathChange{Path: p})
	}
	return r
}

func areaNames(areas []WorkArea) []string {
	names := make([]string, 0, len(areas))
	for _, a := range areas {
		names = append(names, a.Name)
	}
	return names
}

func TestWorkAreasDefaultStructure(t *testing.T) {
	records := []git.CommitRecord{
		areaRecord("a", "git/log.go", "tui/app.go", "docs/guides/setup.md", "README.md"),
		areaRecord("b", "src/auth/login.ts", "src/payments/charge.ts", "services/payments/api/main.go", "services/search/query.go"),
		areaRecord("c", "apps/web/src/main.ts", "packages/ui/button.ts", "cmd/bigboard/main.go", "internal/auth/token.go", "lib/codec/decode.go"),
		areaRecord("d", "src/main/java/com/acme/Main.java"),
	}
	want := []string{"Repository root", "apps/web", "cmd/bigboard", "docs", "git", "internal/auth", "lib/codec", "packages/ui", "services/payments", "services/search", "src/auth", "src/main/java/com/acme", "src/payments", "tui"}
	if got := areaNames(BuildWorkAreas(records, nil)); !reflect.DeepEqual(got, want) {
		t.Fatalf("areas = %v, want %v", got, want)
	}
}

func TestWorkAreasLiteralRulesMostSpecific(t *testing.T) {
	rules := []WorkAreaRule{
		{Name: "Backend", Paths: []string{"src/", "services"}},
		{Name: "Sign in", Paths: []string{"src/auth", "services/identity"}},
		{Name: "Token", Paths: []string{"src/auth/token.go"}},
		{Name: "Literal", Paths: []string{"literal*"}},
	}
	if err := ValidateWorkAreaRules(rules); err != nil {
		t.Fatal(err)
	}
	records := []git.CommitRecord{areaRecord("one", "src/auth/token.go", "src/auth/login.go", "src/authentication/check.go", "services/identity/lookup.go", "docs/index.md", "literal*/file", "literal-other/file")}
	areas := BuildWorkAreas(records, rules)
	got := make(map[string][]string)
	for _, a := range areas {
		if len(a.Records) != 1 {
			t.Fatalf("%s has %d records", a.Name, len(a.Records))
		}
		for _, c := range a.Records[0].Changes {
			got[a.Name] = append(got[a.Name], c.Path)
		}
	}
	want := map[string][]string{"Backend": {"src/authentication/check.go"}, "Sign in": {"src/auth/login.go", "services/identity/lookup.go"}, "Token": {"src/auth/token.go"}, "docs": {"docs/index.md"}, "Literal": {"literal*/file"}, "literal-other": {"literal-other/file"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapping = %#v, want %#v", got, want)
	}
}

func TestWorkAreaRulesValidation(t *testing.T) {
	tests := []struct {
		name  string
		rules []WorkAreaRule
	}{
		{"empty name", []WorkAreaRule{{Name: " ", Paths: []string{"src"}}}},
		{"duplicate name", []WorkAreaRule{{Name: "A", Paths: []string{"src"}}, {Name: " A ", Paths: []string{"docs"}}}},
		{"no paths", []WorkAreaRule{{Name: "A"}}},
		{"conflicting prefix", []WorkAreaRule{{Name: "A", Paths: []string{"src/"}}, {Name: "B", Paths: []string{"src"}}}},
	}
	for _, p := range []string{"", " ", "/src", "../src", "src/../docs", "src/./docs", "src//docs", ".", "C:/src", "src\\docs", "src\x00docs", "src//"} {
		tests = append(tests, struct {
			name  string
			rules []WorkAreaRule
		}{"invalid " + p, []WorkAreaRule{{Name: "A", Paths: []string{p}}}})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateWorkAreaRules(tc.rules); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if err := ValidateWorkAreaRules([]WorkAreaRule{{Name: "A", Paths: []string{"src", "src/", "a b/file.go"}}, {Name: "B", Paths: []string{"src/auth"}}}); err != nil {
		t.Fatalf("valid nested rules: %v", err)
	}
}

func TestWorkAreasEvidenceCases(t *testing.T) {
	mixed := areaRecord("mixed", "git/git.go", "go.sum")
	mixed.Changes[1].Generated = true
	excluded := areaRecord("generated", "vendor/pkg/a.go")
	excluded.Changes[0].Generated = true
	empty := areaRecord("empty")
	unknown := areaRecord("unknown", "invented/file.go")
	unknown.PathsUnknown = true
	binary := areaRecord("binary", "images/logo.png") // zero line counts do not mean an empty diff
	copy := areaRecord("copy", "docs/copied.go")
	copy.Changes[0].PreviousPath = "git/source.go"
	areas := BuildWorkAreas([]git.CommitRecord{mixed, excluded, empty, unknown, binary, copy}, nil)
	want := []string{"Excluded files", "No file changes", "Unknown paths", "docs", "git", "images"}
	if got := areaNames(areas); !reflect.DeepEqual(got, want) {
		t.Fatalf("areas = %v", got)
	}
	for _, a := range areas {
		if len(a.Records) != 1 {
			t.Fatalf("%s: %d records", a.Name, len(a.Records))
		}
		switch a.Name {
		case "git":
			if len(a.Records[0].Changes) != 1 || a.Records[0].CommitID != "mixed" {
				t.Fatalf("source or generated path leaked: %#v", a)
			}
		case "Unknown paths":
			if len(a.Records[0].Changes) != 0 {
				t.Fatal("unknown paths must discard apparent boundary diff")
			}
		case "docs":
			if a.Records[0].Changes[0].PreviousPath != "git/source.go" {
				t.Fatal("copy metadata lost")
			}
		}
	}
	if len(mixed.Changes) != 2 || len(unknown.Changes) != 1 {
		t.Fatal("input records mutated")
	}
}

func TestWorkAreasStableDefinitionAndDedup(t *testing.T) {
	old := areaRecord("old", "src/auth/login.go", "src/billing/invoice.go")
	recent := areaRecord("recent", "src/auth/logout.go", "src/auth/login.go", "README.md")
	definition := DefineWorkAreas([]git.CommitRecord{old, recent}, nil)
	got := definition.Build([]git.CommitRecord{recent, recent})
	if names := areaNames(got); !reflect.DeepEqual(names, []string{"Repository root", "src/auth"}) {
		t.Fatalf("filtered areas = %v", names)
	}
	for _, a := range got {
		if len(a.Records) != 1 {
			t.Fatalf("duplicate commit in %s", a.Name)
		}
	}
	if got[1].ID != "auto:src/auth" || len(got[1].Records[0].Changes) != 2 {
		t.Fatalf("wrong subset: %#v", got[1])
	}
	if got := definition.Build(nil); len(got) != 0 {
		t.Fatalf("empty filter returned areas %v", got)
	}
}

func TestWorkAreasNameDoesNotCollideWithAutomatic(t *testing.T) {
	rules := []WorkAreaRule{{Name: "docs", Paths: []string{"src/auth"}}}
	got := BuildWorkAreas([]git.CommitRecord{areaRecord("one", "src/auth/login.go", "docs/index.md")}, rules)
	if len(got) != 2 || got[0].ID == got[1].ID {
		t.Fatalf("named and automatic identity collision: %#v", got)
	}
}
