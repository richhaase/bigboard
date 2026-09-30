package git

// IsGeneratedPath reports whether the default generated/vendor filter excludes
// a repository-relative path. It is independent of legacy global filter settings;
// callers that include generated files should bypass this exclusion explicitly.
func IsGeneratedPath(path string) bool {
	return !defaultPathFilter(CollectOptions{}).shouldCount(path)
}
