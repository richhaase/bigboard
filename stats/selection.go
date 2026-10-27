package stats

import "github.com/richhaase/bigboard/git"

func UniqueRecordsWithMatchingCopy(records []git.CommitRecord, matches func(git.CommitRecord) bool) []git.CommitRecord {
	members := make(map[string]bool)
	var result []git.CommitRecord
	for _, record := range records {
		if !matches(record) {
			continue
		}
		if record.CommitID == "" {
			result = append(result, record)
		} else {
			members[record.CommitID] = true
		}
	}
	for _, record := range UniqueRecords(records) {
		if record.CommitID != "" && members[record.CommitID] {
			result = append(result, record)
		}
	}
	return result
}

func FilterByCanonicalIdentity(records []git.CommitRecord, identities map[string]bool) []git.CommitRecord {
	canonical := make(map[string]string)
	for _, record := range UniqueRecords(records) {
		if record.CommitID != "" {
			canonical[record.CommitID] = IdentityID(record)
		}
	}
	var result []git.CommitRecord
	for _, record := range records {
		id := IdentityID(record)
		if record.CommitID != "" {
			id = canonical[record.CommitID]
		}
		if identities[id] {
			result = append(result, record)
		}
	}
	return result
}
