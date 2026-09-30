package git

import (
	"fmt"
	"strings"
	"testing"
)

// fmtHeader builds NUL-delimited metadata in the collection format.
func fmtHeader(name, email, date string, coAuthors ...string) string {
	return "\x00" + strings.Join([]string{strings.Repeat("a", 40), name, email, date, strings.Join(coAuthors, "\x1f"), ""}, "\x00") + "\x00"
}

func TestParseGitLogBasic(t *testing.T) {
	out := strings.Join([]string{
		fmtHeader("Alice", "alice@example.com", "2026-01-02T10:00:00Z"),
		"10\t5\ta.go",
		"3\t0\tb.go",
		"",
		fmtHeader("Bob", "bob@example.com", "2026-01-01T10:00:00Z"),
		"1\t1\tc.go",
	}, "\x00") + "\x00"

	records, err := parseGitLog(out, "repo")
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].Author != "Alice" || records[0].Added != 13 || records[0].Removed != 5 {
		t.Errorf("Alice record wrong: %+v", records[0])
	}
	if records[1].Author != "Bob" || records[1].Added != 1 || records[1].Removed != 1 {
		t.Errorf("Bob record wrong: %+v", records[1])
	}
}

// TestParseGitLogPipeInName is the regression guard for the separator fix: an
// author name containing '|' must no longer drop the commit or misattribute its
// line counts to the previous commit.
func TestParseGitLogPipeInName(t *testing.T) {
	out := strings.Join([]string{
		fmtHeader("Good Dev", "good@example.com", "2026-01-02T10:00:00Z"),
		"100\t0\ta.go",
		"",
		fmtHeader("Bad|Name", "bad@example.com", "2026-01-01T10:00:00Z"),
		"50\t0\tb.go",
	}, "\x00") + "\x00"

	records, err := parseGitLog(out, "repo")
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	var pipe *CommitRecord
	for i := range records {
		if records[i].Author == "Bad|Name" {
			pipe = &records[i]
		}
	}
	if pipe == nil {
		t.Fatal("commit with '|' in author name was dropped")
	}
	if pipe.Added != 50 {
		t.Errorf("pipe-name commit lines misattributed: Added=%d, want 50", pipe.Added)
	}
	// The previous commit must NOT have absorbed the pipe commit's lines.
	for i := range records {
		if records[i].Author == "Good Dev" && records[i].Added != 100 {
			t.Errorf("Good Dev commit corrupted: Added=%d, want 100", records[i].Added)
		}
	}
}

func TestParseGitLogBinaryAndMalformed(t *testing.T) {
	out := strings.Join([]string{
		fmtHeader("Alice", "alice@example.com", "2026-01-02T10:00:00Z"),
		"-\t-\timage.png", // binary: skipped
		"7\t2\tcode.go",
		"",
	}, "\x00") + "\x00"

	records, err := parseGitLog(out, "repo")
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record (binary skipped), got %d", len(records))
	}
	if records[0].Added != 7 || records[0].Removed != 2 {
		t.Errorf("binary line not skipped correctly: %+v", records[0])
	}
}

func TestParseGitLogAIDetection(t *testing.T) {
	cases := []struct {
		name   string
		header string
		wantAI bool
	}{
		{"human", fmtHeader("Human", "human@example.com", "2026-01-01T10:00:00Z"), false},
		{"ai-as-author", fmtHeader("Claude", "noreply@anthropic.com", "2026-01-01T10:00:00Z"), true},
		{"ai-co-author", fmtHeader("Human", "human@example.com", "2026-01-01T10:00:00Z", "Claude <noreply@anthropic.com>"), true},
		{"multi-co-author-with-ai", fmtHeader("Human", "human@example.com", "2026-01-01T10:00:00Z", "Pat <pat@example.com>", "Claude <noreply@anthropic.com>"), true},
		{"multi-co-author-no-ai", fmtHeader("Human", "human@example.com", "2026-01-01T10:00:00Z", "Pat <pat@example.com>", "Sam <sam@example.com>"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.header + "\n5\t0\tfile.go\x00"
			records, err := parseGitLog(out, "repo")
			if err != nil {
				t.Fatalf("parseGitLog: %v", err)
			}
			if len(records) != 1 {
				t.Fatalf("expected 1 record, got %d", len(records))
			}
			if records[0].AIAssisted != tc.wantAI {
				t.Errorf("AIAssisted = %v, want %v (header %q)", records[0].AIAssisted, tc.wantAI, fmt.Sprintf("%q", tc.header))
			}
		})
	}
}

func TestParseGitLogPathFiltering(t *testing.T) {
	out := strings.Join([]string{
		fmtHeader("Dev", "dev@example.com", "2026-01-02T10:00:00Z"),
		"20\t1\tsrc/app.go",                  // counted
		"5000\t0\tpackage-lock.json",         // ignored (basename glob)
		"800\t0\tvendor/lib/x.go",            // ignored (dir segment)
		"3\t0\t\x00src/old.go\x00src/new.go", // counted (rename, resolves to src/new.go)
	}, "\x00") + "\x00"

	records, err := parseGitLog(out, "repo")
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Added != 23 { // 20 + 3, excluding the 5800 generated lines
		t.Errorf("Added = %d, want 23 (generated/vendored excluded)", records[0].Added)
	}
}

func TestShouldCountPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"src/app.go", true},
		{"build/x.go", false},
		{"mybuild/x.go", true},
		{"vendor/naïve.lock", false},
		{"go.sum", false},
		{"a/b/package-lock.json", false},
		{"vendor/literal => source.go", false},
		{"src/{old.go => new.go}", true},
		{"node_modules/pkg/index.js", false},
	}
	for _, tc := range cases {
		if got := shouldCountPath(tc.path); got != tc.want {
			t.Errorf("shouldCountPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestShouldCountPathToggle(t *testing.T) {
	prev := FilterGeneratedPaths
	FilterGeneratedPaths = false
	t.Cleanup(func() { FilterGeneratedPaths = prev })
	for _, p := range []string{"build/x.go", "go.sum", "vendor/lib.go"} {
		if !shouldCountPath(p) {
			t.Errorf("with filtering off, shouldCountPath(%q) = false, want true", p)
		}
	}
}

func TestPathFilterUsesExplicitOptions(t *testing.T) {
	if defaultPathFilter(CollectOptions{}).shouldCount("vendor/lib.go") {
		t.Error("default explicit options should filter vendored paths")
	}
	if !defaultPathFilter(CollectOptions{IncludeGenerated: true}).shouldCount("vendor/lib.go") {
		t.Error("IncludeGenerated should count vendored paths")
	}
}

func TestParseGitLogRejectsMalformedMetadata(t *testing.T) {
	for _, data := range []string{
		fmtHeader("Ghost", "ghost@example.com", "not-a-date"),
		fmtHeader("A", "a@test", "2026-01-01T00:00:00Z") + "1\t0\t\x00old\x00",
		fmtHeader("A", "a@test", "2026-01-01T00:00:00Z") + "1\t0\tunterminated",
	} {
		if _, err := parseGitLog(data, "repo"); err == nil {
			t.Errorf("accepted malformed log %q", data)
		}
	}
}

func TestParseGitLogSubject(t *testing.T) {
	for _, subject := range []string{"", "Fix parsing | preserve delimiters", "Fix\tcontrol\x1fbytes\x1b[31m", "Handle naïve subjects 🚀"} {
		t.Run(fmt.Sprintf("%q", subject), func(t *testing.T) {
			header := fmtHeader("Alice", "alice@example.com", "2026-01-02T10:00:00Z", "Claude <noreply@anthropic.com>")
			// Replace the final empty subject, retaining its NUL terminator.
			header = header[:len(header)-1] + subject + "\x00"
			out := header + "\n3\t1\ta.go\x00" + fmtHeader("Bob", "bob@example.com", "2026-01-01T10:00:00Z") + "\n7\t2\tb.go\x00"
			records, err := parseGitLog(out, "repo")
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 2 || records[0].Subject != subject || records[1].Subject != "" {
				t.Fatalf("subjects not preserved: %+v", records)
			}
			if !records[0].AIAssisted || records[0].Added != 3 || records[0].Removed != 1 || records[1].Added != 7 || records[1].Removed != 2 {
				t.Fatalf("subject changed metadata or line attribution: %+v", records)
			}
		})
	}
}
