package stats

import (
	"sort"
	"strings"

	"github.com/richhaase/bigboard/git"
)

type DirectoryEntry struct {
	ID, Path, Name string
	IsDir          bool
	Unknown        bool
	Empty          bool
	Records        []git.CommitRecord
}

func DirectoryChildren(records []git.CommitRecord, prefix string) []DirectoryEntry {
	groups := make(map[string]*DirectoryEntry)
	add := func(entry DirectoryEntry, record git.CommitRecord) {
		if groups[entry.ID] == nil {
			groups[entry.ID] = &entry
		}
		groups[entry.ID].Records = append(groups[entry.ID].Records, record)
	}
	for _, record := range UniqueRecords(records) {
		if record.PathsUnknown {
			if prefix == "" {
				add(DirectoryEntry{ID: "unknown", Name: "Unknown paths", Unknown: true}, directoryRecord(record, nil))
			}
			continue
		}
		changes := make(map[string][]git.PathChange)
		entries := make(map[string]DirectoryEntry)
		counted := false
		for _, change := range record.Changes {
			if change.Generated || change.Path == "" {
				continue
			}
			counted = true
			relative := change.Path
			if prefix != "" {
				var ok bool
				relative, ok = strings.CutPrefix(relative, prefix+"/")
				if !ok {
					continue
				}
			}
			name, _, nested := strings.Cut(relative, "/")
			path := name
			if prefix != "" {
				path = prefix + "/" + name
			}
			id := "file:" + path
			if nested {
				id = "dir:" + path
			}
			entries[id] = DirectoryEntry{ID: id, Path: path, Name: name, IsDir: nested}
			changes[id] = append(changes[id], change)
		}
		if !counted && prefix == "" {
			add(DirectoryEntry{ID: "empty", Name: "No counted file changes", Empty: true}, directoryRecord(record, nil))
		}
		for id, paths := range changes {
			add(entries[id], directoryRecord(record, paths))
		}
	}
	result := make([]DirectoryEntry, 0, len(groups))
	for _, entry := range groups {
		result = append(result, *entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func DirectoryRecords(records []git.CommitRecord, id string) []git.CommitRecord {
	kind, path, _ := strings.Cut(id, ":")
	var result []git.CommitRecord
	for _, record := range UniqueRecords(records) {
		if record.PathsUnknown {
			if id == "unknown" || id == "dir:" {
				result = append(result, directoryRecord(record, nil))
			}
			continue
		}
		var changes []git.PathChange
		counted := false
		for _, change := range record.Changes {
			if change.Generated || change.Path == "" {
				continue
			}
			counted = true
			if kind == "file" && change.Path == path || kind == "dir" && (path == "" || strings.HasPrefix(change.Path, path+"/")) {
				changes = append(changes, change)
			}
		}
		if len(changes) > 0 || !counted && (id == "empty" || id == "dir:") {
			result = append(result, directoryRecord(record, changes))
		}
	}
	return result
}

func directoryRecord(record git.CommitRecord, changes []git.PathChange) git.CommitRecord {
	record.Added, record.Removed = 0, 0
	record.Changes = append([]git.PathChange(nil), changes...)
	record.LinesUnknown = record.LinesUnknown || record.PathsUnknown
	for _, change := range changes {
		if change.Binary {
			continue
		}
		if !change.LinesKnown {
			record.LinesUnknown = true
			continue
		}
		record.Added += change.Added
		record.Removed += change.Removed
	}
	if record.LinesUnknown {
		record.Added, record.Removed = 0, 0
	}
	return record
}
