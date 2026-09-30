package stats

import (
	"strings"

	"github.com/richhaase/bigboard/git"
)

// Subareas refines an automatic path area by exactly one literal directory
// level. prefix is its full repository-relative path, or a descendant path.
// Configured named areas are deliberately not reinterpreted. Directory children
// have path:<prefix> IDs; direct:<prefix> is a leaf containing direct files.
// Counts overlap when one commit touches multiple children. Records keep only
// the child's changed paths, preserving identities and commit-level metadata.
func Subareas(area WorkArea, prefix string) []WorkArea {
	if !strings.HasPrefix(area.ID, "auto:") {
		return nil
	}
	root := strings.TrimPrefix(area.ID, "auto:")
	if prefix != root && !strings.HasPrefix(prefix, root+"/") {
		return nil
	}
	groups := make(map[string]*WorkArea)
	add := func(id, name string, r git.CommitRecord) {
		if groups[id] == nil {
			groups[id] = &WorkArea{ID: id, Name: name}
		}
		groups[id].Records = append(groups[id].Records, r)
	}
	for _, r := range UniqueRecords(area.Records) {
		if r.PathsUnknown {
			r.Changes = nil
			add("unknown", "Unknown paths", r)
			continue
		}
		if len(r.Changes) == 0 {
			add("empty", "No file changes", r)
			continue
		}
		paths := make(map[string][]git.PathChange)
		labels := make(map[string]string)
		for _, change := range r.Changes {
			if change.Generated || !strings.HasPrefix(change.Path, prefix+"/") {
				continue
			}
			relative := strings.TrimPrefix(change.Path, prefix+"/")
			first, _, nested := strings.Cut(relative, "/")
			id, name := "direct:"+prefix, prefix+" (direct files)"
			if nested {
				name = prefix + "/" + first
				id = "path:" + name
			}
			paths[id] = append(paths[id], change)
			labels[id] = name
		}
		for id, changes := range paths {
			copy := r
			copy.Changes = changes
			add(id, labels[id], copy)
		}
	}
	result := make([]WorkArea, 0, len(groups))
	for _, group := range groups {
		result = append(result, *group)
	}
	sortWorkAreas(result)
	return result
}
