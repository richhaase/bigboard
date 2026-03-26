package tui

import (
	"strings"
	"testing"
)

func TestRenderImpactBar(t *testing.T) {
	// Max value produces blocks
	result := RenderImpactBar(100, 100, 20)
	if !strings.Contains(result, "█") {
		t.Errorf("expected blocks in full bar, got: %q", result)
	}

	// Zero value has no blocks
	empty := RenderImpactBar(0, 100, 20)
	if strings.Contains(empty, "█") {
		t.Errorf("expected no blocks for zero value, got: %q", empty)
	}

	// Positive value has at least one block (min 1)
	small := RenderImpactBar(1, 1000000, 20)
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

func TestRenderHeader(t *testing.T) {
	result := RenderHeader(80)
	if !strings.Contains(result, "BIG BOARD") {
		t.Errorf("expected 'BIG BOARD' in header output, got: %q", result)
	}
}
