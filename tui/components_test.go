package tui

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderStatBoxesWidth(t *testing.T) {
	for _, width := range []int{40, 50, 60, 78, 80, 96, 120} {
		out := RenderStatBoxes(1234567, 9876543, 1234567, 4321, width)
		for _, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line %q display width %d exceeds %d", width, line, w, width)
			}
		}
	}
}

func TestRenderStatBoxesNonPositiveWidth(t *testing.T) {
	if RenderStatBoxes(0, 0, 0, 0, 0) == "" {
		t.Error("width 0 should still render")
	}
	if RenderStatBoxes(10, 20, 30, 0, -5) == "" {
		t.Error("negative width should not panic and should render")
	}
}

func TestRenderStatBoxesSubOnePercent(t *testing.T) {
	out := RenderStatBoxes(1000, 0, 0, 3, 120)
	if !strings.Contains(out, "<1%") {
		t.Errorf("expected <1%% for 3/1000:\n%s", out)
	}
	if !strings.Contains(out, "(3)") {
		t.Errorf("expected raw count (3):\n%s", out)
	}
	if strings.Contains(out, "0%") {
		t.Errorf("should not show 0%% when count is positive:\n%s", out)
	}

	half := RenderStatBoxes(100, 0, 0, 50, 120)
	if !strings.Contains(half, "50%") {
		t.Errorf("expected 50%% for 50/100:\n%s", half)
	}
}

func TestRenderImpactBar(t *testing.T) {
	// Full bar produces blocks
	result := RenderImpactBar(80, 20, 100, 20)
	if !strings.Contains(result, "█") {
		t.Errorf("expected blocks in full bar, got: %q", result)
	}

	// Zero value has no blocks
	empty := RenderImpactBar(0, 0, 100, 20)
	if strings.Contains(empty, "█") {
		t.Errorf("expected no blocks for zero value, got: %q", empty)
	}

	// Small positive value has at least one block
	small := RenderImpactBar(1, 0, 1000000, 20)
	if !strings.Contains(small, "█") {
		t.Errorf("expected at least one block for small positive value, got: %q", small)
	}
}

func TestFormatNumber(t *testing.T) {
	minInt := -int(^uint(0)>>1) - 1
	minIntFormatted := "-2,147,483,648"
	if strconv.IntSize == 64 {
		minIntFormatted = "-9,223,372,036,854,775,808"
	}
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
		{minInt, minIntFormatted},
	}
	for _, tt := range tests {
		got := FormatNumber(tt.input)
		if got != tt.expected {
			t.Errorf("FormatNumber(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDisplayTextRemovesTerminalControls(t *testing.T) {
	input := "\x1b]52;c;Y2xpcGJvYXJk\aAlice\n\x1b[31mRed\x1b[0m\x7f"
	got := displayText(input)
	if strings.ContainsAny(got, "\x1b\n\r\x7f") {
		t.Fatalf("displayText retained terminal controls: %q", got)
	}
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "Red") {
		t.Fatalf("displayText removed printable content: %q", got)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		width    int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world foo", 10, "hello w..."},
		{"hi", 2, "hi"},
	}
	for _, tt := range tests {
		got := Truncate(tt.input, tt.width)
		if got != tt.expected {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.width, got, tt.expected)
		}
	}
}

func TestTruncateMultibyte(t *testing.T) {
	// CJK (double-width) and accented Latin must never be split mid-rune into
	// invalid UTF-8, and the result's display width must respect the limit.
	cases := []struct {
		input string
		width int
	}{
		{"日本語テスト名前あいうえお", 10},
		{"日本語テスト名前あいうえお", 5},
		{"Łukasz Kowalski Świątek", 8},
		{"日本語", 2},
	}
	for _, tc := range cases {
		got := Truncate(tc.input, tc.width)
		if !utf8.ValidString(got) {
			t.Errorf("Truncate(%q, %d) = %q is not valid UTF-8", tc.input, tc.width, got)
		}
		if w := lipgloss.Width(got); w > tc.width {
			t.Errorf("Truncate(%q, %d) display width = %d, exceeds %d", tc.input, tc.width, w, tc.width)
		}
	}
}

func TestTruncateNonPositiveWidth(t *testing.T) {
	// Must not panic on zero or negative width (latent slice-bounds bug).
	if got := Truncate("hello", 0); got != "" {
		t.Errorf("Truncate(_, 0) = %q, want empty", got)
	}
	if got := Truncate("hello", -3); got != "" {
		t.Errorf("Truncate(_, -3) = %q, want empty", got)
	}
}

func TestPadRight(t *testing.T) {
	if got := padRight("ab", 5); got != "ab   " {
		t.Errorf("padRight(\"ab\", 5) = %q, want %q", got, "ab   ")
	}
	// Already at/over width: returned unchanged.
	if got := padRight("abcdef", 4); got != "abcdef" {
		t.Errorf("padRight over width changed string: %q", got)
	}
	// Wide glyphs are padded by display width, not byte count.
	if got := padRight("日本", 6); lipgloss.Width(got) != 6 {
		t.Errorf("padRight(\"日本\", 6) display width = %d, want 6", lipgloss.Width(got))
	}
}

func TestRenderHeader(t *testing.T) {
	result := RenderHeader(80, 15, 0, "v0.1.0")
	if !strings.Contains(result, "repos") {
		t.Errorf("expected repo count in header output, got: %q", result)
	}
	for _, line := range strings.Split(RenderHeader(40, 15, 0, "v0.1.0"), "\n") {
		if width := lipgloss.Width(line); width > 40 {
			t.Errorf("compact header line width = %d, want <= 40: %q", width, line)
		}
	}
}

func TestRenderRepoCount(t *testing.T) {
	// No exclusions
	result := RenderRepoCount(15, 0)
	if !strings.Contains(result, "15 repos") {
		t.Errorf("expected '15 repos', got: %q", result)
	}

	// With exclusions
	result2 := RenderRepoCount(15, 3)
	if !strings.Contains(result2, "12/15 repos") {
		t.Errorf("expected '12/15 repos', got: %q", result2)
	}
}
