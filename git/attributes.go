package git

import (
	"context"
	"path"
	"strings"
)

type attributeRule struct {
	segments  []string
	anchored  bool
	generated *bool
}

func parseAttributes(content string) []attributeRule {
	var rules []attributeRule
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") || strings.HasPrefix(fields[0], "!") || strings.HasSuffix(fields[0], "/") {
			continue
		}
		var generated *bool
		for _, attr := range fields[1:] {
			name, value, assigned := strings.Cut(attr, "=")
			state := true
			switch {
			case strings.HasPrefix(name, "-"):
				name, state = name[1:], false
			case strings.HasPrefix(name, "!"):
				continue
			case assigned:
				state = value == "true" || value == "1"
				if !state && value != "false" && value != "0" {
					continue
				}
			}
			if name == "linguist-generated" || name == "linguist-vendored" {
				if generated == nil || state {
					generated = &state
				}
			}
		}
		if generated == nil {
			continue
		}
		pattern := fields[0]
		anchored := strings.Contains(pattern, "/")
		rules = append(rules, attributeRule{segments: strings.Split(strings.TrimPrefix(pattern, "/"), "/"), anchored: anchored, generated: generated})
	}
	return rules
}

func attributeGenerated(rules []attributeRule, filePath string) *bool {
	var result *bool
	segments := strings.Split(filePath, "/")
	for _, rule := range rules {
		if rule.anchored {
			if matchSegments(rule.segments, segments) {
				result = rule.generated
			}
		} else if ok, _ := path.Match(rule.segments[0], segments[len(segments)-1]); ok {
			result = rule.generated
		}
	}
	return result
}

func matchSegments(pattern, segments []string) bool {
	if len(pattern) == 0 {
		return len(segments) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(segments); i++ {
			if matchSegments(pattern[1:], segments[i:]) {
				return true
			}
		}
		return false
	}
	if len(segments) == 0 {
		return false
	}
	if ok, _ := path.Match(pattern[0], segments[0]); !ok {
		return false
	}
	return matchSegments(pattern[1:], segments[1:])
}

func readAttributes(ctx context.Context, dir, ref string, partial bool) ([]attributeRule, error) {
	listing, err := runGitContext(ctx, dir, "ls-tree", "--name-only", "--end-of-options", ref, "--", ".gitattributes")
	if err != nil || strings.TrimSpace(listing) == "" {
		return nil, nil
	}
	content, err := runGitContext(ctx, dir, "cat-file", "blob", ref+":.gitattributes")
	if err != nil {
		if partial {
			return nil, nil
		}
		return nil, err
	}
	return parseAttributes(content), nil
}
