package git

import (
	"bufio"
	"reflect"
	"strings"
	"testing"
)

func TestRetainedChangesLiteralPaths(t *testing.T) {
	out := fmtHeader("Dev", "dev@example.com", "1767348000") +
		"\n2\t1\t\nleading\tline.go\x00" +
		"-\t-\timage\twith\nlines.png\x00" +
		"9\t0\tvendor/generated\tfile.go\x00" +
		"0\t0\t\x00old\tname.go\x00new\nname.go\x00" +
		"-\t-\t\x00old\nbinary.png\x00vendor/copied\tbinary.png\x00" +
		fmtHeader("Empty", "empty@example.com", "1767261600")
	want := []PathChange{
		{Path: "\nleading\tline.go", Added: 2, Removed: 1, LinesKnown: true},
		{Path: "image\twith\nlines.png", Binary: true},
		{Path: "vendor/generated\tfile.go", Generated: true, Exclusion: ExclusionPath, Added: 9, LinesKnown: true},
		{Path: "new\nname.go", PreviousPath: "old\tname.go", LinesKnown: true},
		{Path: "vendor/copied\tbinary.png", PreviousPath: "old\nbinary.png", Generated: true, Exclusion: ExclusionPath, Binary: true},
	}
	for _, includeGenerated := range []bool{false, true} {
		scanner := bufio.NewScanner(strings.NewReader(out))
		scanner.Split(splitNUL)
		records, err := parseLog(scanner, Repository{ID: "repo", Name: "repo"}, defaultPathFilter(CollectOptions{IncludeGenerated: includeGenerated}), newAIMatcher(nil), nil)
		if err != nil {
			t.Fatal(err)
		}
		if includeGenerated {
			for i := range want {
				want[i].Generated, want[i].Exclusion = false, ""
			}
		}
		if len(records) != 2 || !reflect.DeepEqual(records[0].Changes, want) {
			t.Fatalf("literal changes (IncludeGenerated=%v): %+v", includeGenerated, records)
		}
		if len(records[1].Changes) != 0 || records[1].PathsUnknown || records[0].PathsUnknown {
			t.Fatalf("known empty commit confused with unknown: %+v", records)
		}
		wantAdded := 2
		if includeGenerated {
			wantAdded += 9
		}
		if records[0].Added != wantAdded || records[0].Removed != 1 {
			t.Fatalf("retained paths changed line counts: %+v", records[0])
		}
	}
}

func TestRetainedLineCountsStayWithEachFile(t *testing.T) {
	out := fmtHeader("Dev", "dev@example.com", "1767348000") +
		"\n14\t3\tserver/handler.go\x00" +
		"2\t1\tclient/view.go\x00" +
		"0\t7\tlegacy/removed.go\x00" +
		"4\t2\t\x00old/component.go\x00new/component.go\x00" +
		"90\t80\tvendor/generated.go\x00"
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Split(splitNUL)
	records, err := parseLog(scanner, Repository{ID: "repo", Name: "repo"}, defaultPathFilter(CollectOptions{}), newAIMatcher(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []PathChange{
		{Path: "server/handler.go", Added: 14, Removed: 3, LinesKnown: true},
		{Path: "client/view.go", Added: 2, Removed: 1, LinesKnown: true},
		{Path: "legacy/removed.go", Removed: 7, LinesKnown: true},
		{Path: "new/component.go", PreviousPath: "old/component.go", Added: 4, Removed: 2, LinesKnown: true},
		{Path: "vendor/generated.go", Added: 90, Removed: 80, LinesKnown: true, Generated: true, Exclusion: ExclusionPath},
	}
	if len(records) != 1 || !reflect.DeepEqual(records[0].Changes, want) {
		t.Fatalf("file-specific measurements lost: %+v", records)
	}
	if records[0].Added != 20 || records[0].Removed != 13 {
		t.Fatalf("commit totals changed: %+v", records[0])
	}
}
