package stats

import (
	"reflect"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func TestClassifyPathsMatchesLocalWorkAreas(t *testing.T) {
	paths := []string{
		"README.md", "git/log.go", "tui/app.go", "docs/guides/setup.md",
		"src/auth/login.ts", "src/payments/charge.ts", "src/direct.go",
		"services/payments/api/main.go", "services/search/query.go",
		"apps/web/src/main.ts", "packages/ui/button.ts", "cmd/bigboard/main.go",
		"internal/auth/token.go", "lib/codec/decode.go",
		"src/main/java/com/acme/Main.java", "src/main/java/com/Direct.java",
		"literal*/file.go", "literal-other/file.go", "space dir/line\nfile.go",
	}
	record := areaRecord("local", paths...)
	for _, rules := range [][]WorkAreaRule{
		nil,
		{
			{Name: "Backend", Paths: []string{"src/", "services"}},
			{Name: "Authentication", Paths: []string{"src/auth", "internal/auth"}},
			{Name: "Login", Paths: []string{"src/auth/login.ts"}},
			{Name: "docs", Paths: []string{"tui"}},
			{Name: "Literal", Paths: []string{"literal*"}},
		},
	} {
		want := BuildWorkAreas([]git.CommitRecord{record}, rules)
		for i := range want {
			want[i].Records = nil
		}
		// The repository may have no local commits at all. PR paths should still
		// get the same literal/configured and automatic grouping as local paths.
		got := DefineWorkAreas(nil, rules).ClassifyPaths(record.Changes, false)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("classification = %#v, want %#v", got, want)
		}
	}
}

func TestClassifyPathsEvidenceStates(t *testing.T) {
	tests := []struct {
		name    string
		changes []git.PathChange
		unknown bool
		want    []WorkArea
	}{
		{
			name: "known empty", want: []WorkArea{{ID: "empty", Name: "No file changes"}},
		},
		{
			name: "unknown", unknown: true,
			want: []WorkArea{{ID: "unknown", Name: "Unknown paths"}},
		},
		{
			name: "partial evidence", unknown: true,
			changes: []git.PathChange{{Path: "src/auth/login.go"}},
			want: []WorkArea{
				{ID: "unknown", Name: "Unknown paths"},
				{ID: "auto:src/auth", Name: "src/auth"},
			},
		},
		{
			name: "excluded", changes: []git.PathChange{{Path: "go.sum", Generated: true}},
			want: []WorkArea{{ID: "excluded", Name: "Excluded files"}},
		},
		{
			name: "excluded partial", unknown: true,
			changes: []git.PathChange{{Path: "go.sum", Generated: true}},
			want: []WorkArea{
				{ID: "excluded", Name: "Excluded files"},
				{ID: "unknown", Name: "Unknown paths"},
			},
		},
		{
			name: "empty path excluded", changes: []git.PathChange{{}},
			want: []WorkArea{{ID: "excluded", Name: "Excluded files"}},
		},
		{
			name: "mixed generated duplicate and binary",
			changes: []git.PathChange{
				{Path: "README.md"}, {Path: "README.md"},
				{Path: "vendor/pkg/file.go", Generated: true}, {Path: "images/icon.png"},
			},
			want: []WorkArea{
				{ID: "root", Name: "Repository root"},
				{ID: "auto:images", Name: "images"},
			},
		},
		{
			name:    "generated inclusion controlled by caller",
			changes: []git.PathChange{{Path: "vendor/pkg/file.go"}},
			want:    []WorkArea{{ID: "auto:vendor", Name: "vendor"}},
		},
	}
	definition := DefineWorkAreas(nil, nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := definition.ClassifyPaths(tc.changes, tc.unknown)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("classification = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestClassifyPathsDoesNotTreatCopyMetadataAsChanged(t *testing.T) {
	definition := DefineWorkAreas(nil, nil)
	changes := []git.PathChange{{Path: "docs/copied.go", PreviousPath: "src/auth/source.go"}}
	got := definition.ClassifyPaths(changes, false)
	if want := []WorkArea{{ID: "auto:docs", Name: "docs"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("copy source leaked into work areas: %#v", got)
	}
	// If a provider separately establishes a rename, both touched paths can be
	// supplied explicitly. PreviousPath alone cannot distinguish a rename/copy.
	changes = append(changes, git.PathChange{Path: "src/auth/source.go"})
	got = definition.ClassifyPaths(changes, false)
	want := []WorkArea{{ID: "auto:docs", Name: "docs"}, {ID: "auto:src/auth", Name: "src/auth"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit rename evidence = %#v, want %#v", got, want)
	}
}

func TestClassifyPathsLeavesLocalDefinitionAndRecordsUnchanged(t *testing.T) {
	records := []git.CommitRecord{areaRecord("local", "src/auth/login.go", "README.md")}
	records[0].Added, records[0].Removed = 41, 7
	definition := DefineWorkAreas(records, []WorkAreaRule{{Name: "Search", Paths: []string{"services/search"}}})
	automaticBefore := append([]areaPrefix(nil), definition.automatic...)
	rulesBefore := append([]areaPrefix(nil), definition.rules...)
	localBefore := definition.Build(records)
	changes := []git.PathChange{
		{Path: "src/billing/invoice.go"}, {Path: "services/search/query.go"},
		{Path: "docs/guide.md", PreviousPath: "src/auth/login.go"},
	}
	changesBefore := append([]git.PathChange(nil), changes...)
	want := []WorkArea{
		{ID: "named:Search", Name: "Search"},
		{ID: "auto:docs", Name: "docs"},
		{ID: "auto:src/billing", Name: "src/billing"},
	}
	for range 3 {
		if got := definition.ClassifyPaths(changes, false); !reflect.DeepEqual(got, want) {
			t.Fatalf("PR-only classification = %#v, want %#v", got, want)
		}
	}
	if !reflect.DeepEqual(changes, changesBefore) {
		t.Fatal("input paths were mutated")
	}
	if !reflect.DeepEqual(definition.automatic, automaticBefore) || !reflect.DeepEqual(definition.rules, rulesBefore) {
		t.Fatal("frozen definition was mutated")
	}
	if got := definition.Build(records); !reflect.DeepEqual(got, localBefore) {
		t.Fatalf("local records or attribution changed: %#v, previously %#v", got, localBefore)
	}
	if records[0].Added != 41 || records[0].Removed != 7 || len(records) != 1 || len(records[0].Changes) != 2 {
		t.Fatalf("local statistics changed: %#v", records)
	}
}
