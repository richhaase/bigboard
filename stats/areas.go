package stats

import (
	"fmt"
	"sort"
	"strings"

	"github.com/richhaase/bigboard/git"
)

// WorkAreaRule assigns literal repository-relative file or directory prefixes
// to a named work area. The longest matching prefix wins.
type WorkAreaRule struct {
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}

// WorkArea is path evidence, not ownership or a semantic feature boundary.
// Records retain commit-level line counts, not area-specific line counts.
type WorkArea struct {
	ID      string
	Name    string
	Records []git.CommitRecord
}

type areaPrefix struct{ prefix, id, name string }

// WorkAreaDefinition freezes automatic grouping from a complete repository scan.
// Reuse it across active time filters so an area's meaning does not change.
// Records supplied to both DefineWorkAreas and Build must belong to one repo.
type WorkAreaDefinition struct {
	rules     []areaPrefix
	automatic []areaPrefix
}

// ValidateWorkAreaRules rejects ambiguous names/prefixes and unsafe paths.
// Prefixes are literal (no glob expansion); trailing directory slashes are allowed.
func ValidateWorkAreaRules(rules []WorkAreaRule) error {
	names := make(map[string]bool)
	prefixes := make(map[string]string)
	for _, rule := range rules {
		name := strings.TrimSpace(rule.Name)
		if name == "" {
			return fmt.Errorf("work area name must not be empty")
		}
		if names[name] {
			return fmt.Errorf("duplicate work area name %q", name)
		}
		names[name] = true
		if len(rule.Paths) == 0 {
			return fmt.Errorf("work area %q requires paths", name)
		}
		for _, raw := range rule.Paths {
			p := strings.TrimSuffix(raw, "/")
			if p == "" || strings.TrimSpace(p) == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00") || (len(p) >= 2 && p[1] == ':') {
				return fmt.Errorf("work area %q has invalid relative path %q", name, raw)
			}
			for _, part := range strings.Split(p, "/") {
				if part == "" || part == "." || part == ".." {
					return fmt.Errorf("work area %q has invalid relative path %q", name, raw)
				}
			}
			if prior, ok := prefixes[p]; ok && prior != name {
				return fmt.Errorf("path %q belongs to both %q and %q", p, prior, name)
			}
			prefixes[p] = name
		}
	}
	return nil
}

// DefineWorkAreas builds a reusable mapping. Validate configuration with
// ValidateWorkAreaRules first; invalid rules are ignored rather than producing
// ambiguous assignments. Automatic groups use full paths as their labels.
func DefineWorkAreas(records []git.CommitRecord, rules []WorkAreaRule) WorkAreaDefinition {
	var d WorkAreaDefinition
	if ValidateWorkAreaRules(rules) == nil {
		for _, rule := range rules {
			for _, p := range rule.Paths {
				d.rules = append(d.rules, areaPrefix{strings.TrimSuffix(p, "/"), "named:" + strings.TrimSpace(rule.Name), strings.TrimSpace(rule.Name)})
			}
		}
	}
	sortAreaPrefixes(d.rules)
	root := &areaDirectory{children: make(map[string]*areaDirectory)}
	for _, r := range records {
		if r.PathsUnknown {
			continue
		}
		for _, change := range r.Changes {
			if change.Generated || change.Path == "" {
				continue
			}
			addAreaPath(root, change.Path)
		}
	}
	d.automatic = automaticAreaPrefixes(root)
	return d
}

func addAreaPath(root *areaDirectory, path string) {
	parts := strings.Split(path, "/")
	node := root
	for _, dir := range parts[:len(parts)-1] {
		if node.children[dir] == nil {
			node.children[dir] = &areaDirectory{children: make(map[string]*areaDirectory)}
		}
		node = node.children[dir]
	}
}

func automaticAreaPrefixes(root *areaDirectory) []areaPrefix {
	var prefixes []areaPrefix
	var visit func(string, string, *areaDirectory)
	visit = func(prefix, base string, node *areaDirectory) {
		// Expand structural containers, retaining direct files in the container's
		// own area. This also collapses generic single-child directory chains.
		if genericAreaDirectory(base) {
			for child, next := range node.children {
				visit(prefix+"/"+child, child, next)
			}
		}
		prefixes = append(prefixes, areaPrefix{prefix, "auto:" + prefix, prefix})
	}
	for name, node := range root.children {
		visit(name, name, node)
	}
	sortAreaPrefixes(prefixes)
	return prefixes
}

type areaDirectory struct {
	children map[string]*areaDirectory
}

func genericAreaDirectory(name string) bool {
	switch name {
	case "src", "apps", "packages", "services", "cmd", "internal", "lib", "main", "java", "kotlin", "com", "org":
		return true
	default:
		return false
	}
}

func sortAreaPrefixes(prefixes []areaPrefix) {
	sort.Slice(prefixes, func(i, j int) bool {
		if len(prefixes[i].prefix) != len(prefixes[j].prefix) {
			return len(prefixes[i].prefix) > len(prefixes[j].prefix)
		}
		if prefixes[i].prefix != prefixes[j].prefix {
			return prefixes[i].prefix < prefixes[j].prefix
		}
		return prefixes[i].id < prefixes[j].id
	})
}

func (d WorkAreaDefinition) areaFor(p string) areaPrefix {
	for _, rules := range [][]areaPrefix{d.rules, d.automatic} {
		for _, rule := range rules {
			if p == rule.prefix || strings.HasPrefix(p, rule.prefix+"/") {
				return rule
			}
		}
	}
	if !strings.Contains(p, "/") {
		return areaPrefix{id: "root", name: "Repository root"}
	}
	// An incremental caller can supply an unseen path. Its top-level fallback
	// remains a literal directory, never an inferred semantic category.
	first, _, _ := strings.Cut(p, "/")
	return areaPrefix{id: "auto:" + first, name: first}
}

// ClassifyPaths maps path evidence to unique work-area identities without
// creating commit records or changing the definition. Unseen directories use
// the same automatic grouping as local history, so PR-only areas can appear.
// Generated paths and PreviousPath metadata are ignored, as in Build. Callers
// with a confirmed rename may supply its origin as a separate PathChange.
// Unknown adds an explicit area without discarding any known path evidence;
// this supports partial file lists. An empty known list means no file changes.
func (d WorkAreaDefinition) ClassifyPaths(changes []git.PathChange, unknown bool) []WorkArea {
	root := &areaDirectory{children: make(map[string]*areaDirectory)}
	for _, change := range changes {
		if !change.Generated && change.Path != "" {
			addAreaPath(root, change.Path)
		}
	}
	// Copy the slice before extending or sorting: a definition may be reused
	// concurrently and its existing mapping must remain unchanged.
	d.automatic = append(append([]areaPrefix(nil), d.automatic...), automaticAreaPrefixes(root)...)
	sortAreaPrefixes(d.automatic)
	areas := make(map[string]WorkArea)
	for _, change := range changes {
		if change.Generated || change.Path == "" {
			continue
		}
		area := d.areaFor(change.Path)
		areas[area.id] = WorkArea{ID: area.id, Name: area.name}
	}
	if len(areas) == 0 {
		switch {
		case len(changes) > 0:
			areas["excluded"] = WorkArea{ID: "excluded", Name: "Excluded files"}
		case !unknown:
			areas["empty"] = WorkArea{ID: "empty", Name: "No file changes"}
		}
	}
	if unknown {
		areas["unknown"] = WorkArea{ID: "unknown", Name: "Unknown paths"}
	}
	result := make([]WorkArea, 0, len(areas))
	for _, area := range areas {
		result = append(result, area)
	}
	sortWorkAreas(result)
	return result
}

// Build assigns each commit once per touched area and keeps only matching
// Changes in that area's record. PreviousPath is metadata only: a copy source
// is not evidence that the source area changed. Generated paths are omitted;
// a commit containing only excluded paths has its own explicit group.
func (d WorkAreaDefinition) Build(records []git.CommitRecord) []WorkArea {
	areas := make(map[string]*WorkArea)
	add := func(area areaPrefix, record git.CommitRecord) {
		if areas[area.id] == nil {
			areas[area.id] = &WorkArea{ID: area.id, Name: area.name}
		}
		areas[area.id].Records = append(areas[area.id].Records, record)
	}
	for _, record := range UniqueRecords(records) {
		if record.PathsUnknown {
			record.Changes = nil
			add(areaPrefix{id: "unknown", name: "Unknown paths"}, record)
			continue
		}
		if len(record.Changes) == 0 {
			add(areaPrefix{id: "empty", name: "No file changes"}, record)
			continue
		}
		grouped := make(map[string][]git.PathChange)
		labels := make(map[string]areaPrefix)
		for _, change := range record.Changes {
			if change.Generated || change.Path == "" {
				continue
			}
			area := d.areaFor(change.Path)
			grouped[area.id] = append(grouped[area.id], change)
			labels[area.id] = area
		}
		if len(grouped) == 0 {
			add(areaPrefix{id: "excluded", name: "Excluded files"}, record)
			continue
		}
		for id, changes := range grouped {
			copy := record
			copy.Changes = changes
			add(labels[id], copy)
		}
	}
	result := make([]WorkArea, 0, len(areas))
	for _, area := range areas {
		result = append(result, *area)
	}
	sortWorkAreas(result)
	return result
}

func sortWorkAreas(areas []WorkArea) {
	sort.Slice(areas, func(i, j int) bool {
		if areas[i].Name != areas[j].Name {
			return areas[i].Name < areas[j].Name
		}
		return areas[i].ID < areas[j].ID
	})
}

// BuildWorkAreas is convenient when records already contain the full scan.
// Use DefineWorkAreas(...).Build(...) when applying active time filters.
func BuildWorkAreas(records []git.CommitRecord, rules []WorkAreaRule) []WorkArea {
	return DefineWorkAreas(records, rules).Build(records)
}
