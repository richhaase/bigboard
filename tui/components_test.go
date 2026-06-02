package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

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
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
	}
	for _, tt := range tests {
		got := FormatNumber(tt.input)
		if got != tt.expected {
			t.Errorf("FormatNumber(%d) = %q, want %q", tt.input, got, tt.expected)
		}
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
	result := RenderHeader(80, 15, 0, -1, "v0.1.0")
	if !strings.Contains(result, "repos") {
		t.Errorf("expected repo count in header output, got: %q", result)
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

func TestGlitchSeparator(t *testing.T) {
	static := glitchSeparator(80, -1)
	if !strings.Contains(static, "━") {
		t.Errorf("static separator should be a heavy rule: %q", static)
	}
	// Animated frames should differ from each other as the sweep moves.
	if glitchSeparator(80, 0) == glitchSeparator(80, 10) {
		t.Errorf("animated separator should change between frames")
	}
}
