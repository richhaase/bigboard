# Repository Exclusion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to exclude repositories from BigBoard's view via CLI flags and an interactive TUI overlay.

**Architecture:** Add `--exclude` CLI flag to seed an exclusion set. Add a new `ViewRepoOverlay` mode to the TUI with checkbox list for toggling repos. Filter commit records through the exclusion set before aggregation. Show repo count indicator in the header.

**Tech Stack:** Go, Bubble Tea, Lipgloss (all existing dependencies)

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `cmd/bigboard/main.go` | Modify | Add `--exclude` flag, pass to `NewModel` |
| `tui/app.go` | Modify | Add `ViewRepoOverlay` mode, exclusion state, filtering logic |
| `tui/repooverlay.go` | Create | Overlay rendering and interaction logic |
| `tui/components.go` | Modify | Update repo count display to show excluded count |
| `stats/stats.go` | Modify | Add `FilterByRepo` function |
| `stats/stats_test.go` | Modify | Add `FilterByRepo` tests |
| `tui/repooverlay_test.go` | Create | Overlay rendering tests |

---

### Task 1: Add `FilterByRepo` to stats package

**Files:**
- Modify: `stats/stats.go`
- Modify: `stats/stats_test.go`

- [ ] **Step 1: Write the failing test**

Add to `stats/stats_test.go`:

```go
func TestFilterByRepo(t *testing.T) {
	now := time.Now()
	records := []git.CommitRecord{
		{Author: "Alice", Date: now, Added: 10, RepoName: "repo-a"},
		{Author: "Alice", Date: now, Added: 20, RepoName: "repo-b"},
		{Author: "Bob", Date: now, Added: 30, RepoName: "repo-c"},
		{Author: "Bob", Date: now, Added: 40, RepoName: "repo-a"},
	}

	excluded := map[string]bool{"repo-b": true}
	filtered := stats.FilterByRepo(records, excluded)

	if len(filtered) != 3 {
		t.Errorf("expected 3 records, got %d", len(filtered))
	}
	for _, r := range filtered {
		if r.RepoName == "repo-b" {
			t.Error("repo-b should be excluded")
		}
	}

	// Empty exclusion set returns all records
	all := stats.FilterByRepo(records, nil)
	if len(all) != 4 {
		t.Errorf("expected 4 records with nil exclusions, got %d", len(all))
	}

	// Exclude all repos
	allExcluded := map[string]bool{"repo-a": true, "repo-b": true, "repo-c": true}
	none := stats.FilterByRepo(records, allExcluded)
	if len(none) != 0 {
		t.Errorf("expected 0 records, got %d", len(none))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/rdh/src/bigboard && go test ./stats/ -run TestFilterByRepo -v`
Expected: FAIL — `stats.FilterByRepo` undefined

- [ ] **Step 3: Write minimal implementation**

Add to `stats/stats.go` after the `FilterByTime` function (after line 56):

```go
// FilterByRepo returns records not in the excluded set. Keys are repo names.
func FilterByRepo(records []git.CommitRecord, excluded map[string]bool) []git.CommitRecord {
	if len(excluded) == 0 {
		return records
	}
	out := make([]git.CommitRecord, 0, len(records))
	for _, r := range records {
		if !excluded[r.RepoName] {
			out = append(out, r)
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/rdh/src/bigboard && go test ./stats/ -run TestFilterByRepo -v`
Expected: PASS

- [ ] **Step 5: Run all stats tests**

Run: `cd /Users/rdh/src/bigboard && go test ./stats/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add stats/stats.go stats/stats_test.go
git commit -m "feat: add FilterByRepo for excluding repos from aggregation"
```

---

### Task 2: Add `--exclude` CLI flag and pass exclusion set to Model

**Files:**
- Modify: `cmd/bigboard/main.go`
- Modify: `tui/app.go`

- [ ] **Step 1: Update `NewModel` signature to accept repo paths and initial exclusions**

In `tui/app.go`, add fields to `Model` struct (after `repoNames` field, line 26):

```go
type Model struct {
	allRecords      []git.CommitRecord
	authors         []stats.AuthorStats
	repoNames       []string
	excludedRepos   map[string]bool
	viewMode        ViewMode
	selectedRow     int
	activeOperative string
	sortField       stats.SortField
	timeIdx         int
	width           int
	height          int
	loading         bool
	err             error
}
```

Update `NewModel` to accept and store exclusions:

```go
func NewModel(repoPaths []string, initialSort stats.SortField, excluded map[string]bool) Model {
	return Model{
		sortField:     initialSort,
		timeIdx:       2, // 14d
		loading:       true,
		excludedRepos: excluded,
	}
}
```

Update `recomputeAuthors` to filter by excluded repos:

```go
func (m *Model) recomputeAuthors() {
	filtered := stats.FilterByRepo(m.allRecords, m.excludedRepos)
	filtered = stats.FilterByTime(filtered, TimePresets[m.timeIdx].Duration)
	m.authors = stats.Aggregate(filtered)
	stats.Sort(m.authors, m.sortField)
}
```

- [ ] **Step 2: Add `--exclude` flag in `main.go`**

In `cmd/bigboard/main.go`, add a custom `excludeFlags` type and wire it up:

```go
type excludeFlags []string

func (e *excludeFlags) String() string { return strings.Join(*e, ",") }
func (e *excludeFlags) Set(v string) error {
	*e = append(*e, v)
	return nil
}

func main() {
	sortFlag := flag.String("sort", "total", "Initial sort: commits|added|removed|net|total")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	var excludes excludeFlags
	flag.Var(&excludes, "exclude", "Repo directory name to exclude (repeatable)")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("bigboard %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	repoPaths := git.DiscoverRepos(paths)
	if len(repoPaths) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no git repositories found in %v\n", paths)
		os.Exit(1)
	}

	// Build exclusion set from --exclude flags, matching against repo basenames
	excludedRepos := make(map[string]bool)
	for _, rp := range repoPaths {
		base := filepath.Base(rp)
		for _, ex := range excludes {
			if base == ex {
				excludedRepos[base] = true
			}
		}
	}

	initialSort := stats.SortFieldFromString(*sortFlag)
	model := tui.NewModel(repoPaths, initialSort, excludedRepos)

	p := tea.NewProgram(model, tea.WithAltScreen())

	go func() {
		p.Send(tui.LoadDataCmd(repoPaths)())
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

Add `"path/filepath"` and `"strings"` to imports in `main.go`.

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/rdh/src/bigboard && go build ./cmd/bigboard/`
Expected: Compiles without errors

- [ ] **Step 4: Run all tests**

Run: `cd /Users/rdh/src/bigboard && go test ./... -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/bigboard/main.go tui/app.go
git commit -m "feat: add --exclude CLI flag and wire exclusion set into Model"
```

---

### Task 3: Update header repo count to show exclusions

**Files:**
- Modify: `tui/app.go` (lines 230-233, the repo count rendering in `renderAggregateView`)

- [ ] **Step 1: Update the repo count display in `renderAggregateView`**

In `tui/app.go`, replace the time picker + repo count section in `renderAggregateView` (lines 231-233):

```go
	// Time picker + repo count on the same line
	timePicker := RenderTimePicker(m.timeIdx)
	repoCount := RenderRepoCount(len(m.repoNames), len(m.excludedRepos))
	sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top, timePicker, repoCount))
```

- [ ] **Step 2: Add `RenderRepoCount` to `components.go`**

Add to `tui/components.go`:

```go
// RenderRepoCount renders the repo count indicator showing excluded repos if any.
func RenderRepoCount(total, excluded int) string {
	if excluded > 0 {
		return StyleSubtitle.Render(fmt.Sprintf("  %d/%d repos", total-excluded, total))
	}
	return StyleSubtitle.Render(fmt.Sprintf("  %d repos", total))
}
```

- [ ] **Step 3: Update `renderOperativeView` to also use `RenderRepoCount`**

No change needed — the operative view doesn't show repo count in the header.

- [ ] **Step 4: Add test for `RenderRepoCount`**

Add to `tui/components_test.go`:

```go
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
```

- [ ] **Step 5: Run tests**

Run: `cd /Users/rdh/src/bigboard && go test ./tui/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add tui/app.go tui/components.go tui/components_test.go
git commit -m "feat: show excluded repo count in header"
```

---

### Task 4: Add `ViewRepoOverlay` mode and key handling

**Files:**
- Modify: `tui/app.go`

- [ ] **Step 1: Add `ViewRepoOverlay` to the `ViewMode` enum**

In `tui/app.go`, update the const block (line 17-20):

```go
const (
	ViewAggregate ViewMode = iota
	ViewOperative
	ViewRepoOverlay
)
```

- [ ] **Step 2: Add overlay state fields to `Model`**

Add after `excludedRepos` field:

```go
type Model struct {
	allRecords       []git.CommitRecord
	authors          []stats.AuthorStats
	repoNames        []string
	excludedRepos    map[string]bool
	overlayExcluded  map[string]bool // working copy while overlay is open
	overlayCursor    int
	viewMode         ViewMode
	selectedRow      int
	activeOperative  string
	sortField        stats.SortField
	timeIdx          int
	width            int
	height           int
	loading          bool
	err              error
}
```

- [ ] **Step 3: Add key handling for opening and interacting with the overlay**

In `handleKey` in `tui/app.go`, add the `"r"` key in the aggregate view section and overlay key handling.

Add this case before the `"enter"` case:

```go
	case "r":
		if m.viewMode == ViewAggregate {
			// Open repo overlay with a working copy of exclusions
			m.overlayExcluded = make(map[string]bool)
			for k, v := range m.excludedRepos {
				m.overlayExcluded[k] = v
			}
			m.overlayCursor = 0
			m.viewMode = ViewRepoOverlay
		}
```

Update the `"esc"` case to handle the overlay:

```go
	case "esc":
		switch m.viewMode {
		case ViewRepoOverlay:
			// Dismiss overlay, apply changes
			m.excludedRepos = m.overlayExcluded
			m.overlayExcluded = nil
			m.viewMode = ViewAggregate
			m.recomputeAuthors()
			m.selectedRow = 0
		case ViewOperative:
			m.viewMode = ViewAggregate
			m.activeOperative = ""
		default:
			return m, tea.Quit
		}
```

Update the `"enter"` case to also dismiss the overlay:

```go
	case "enter":
		if m.viewMode == ViewRepoOverlay {
			// Dismiss overlay, apply changes
			m.excludedRepos = m.overlayExcluded
			m.overlayExcluded = nil
			m.viewMode = ViewAggregate
			m.recomputeAuthors()
			m.selectedRow = 0
		} else if m.viewMode == ViewAggregate && m.selectedRow < len(m.authors) {
			m.activeOperative = m.authors[m.selectedRow].Name
			m.viewMode = ViewOperative
		}
```

Update `"up"/"k"` and `"down"/"j"` to handle overlay navigation:

```go
	case "up", "k":
		if m.viewMode == ViewRepoOverlay {
			if m.overlayCursor > 0 {
				m.overlayCursor--
			}
		} else if m.selectedRow > 0 {
			m.selectedRow--
		}

	case "down", "j":
		if m.viewMode == ViewRepoOverlay {
			if m.overlayCursor < len(m.repoNames)-1 {
				m.overlayCursor++
			}
		} else {
			maxRow := len(m.authors) - 1
			if maxRow > 19 {
				maxRow = 19
			}
			if m.selectedRow < maxRow {
				m.selectedRow++
			}
		}
```

Add a `" "` (space) case for toggling:

```go
	case " ":
		if m.viewMode == ViewRepoOverlay && m.overlayCursor < len(m.repoNames) {
			name := m.repoNames[m.overlayCursor]
			if m.overlayExcluded[name] {
				delete(m.overlayExcluded, name)
			} else {
				m.overlayExcluded[name] = true
			}
		}
```

- [ ] **Step 4: Route `ViewRepoOverlay` in `View()`**

In `tui/app.go`, update the `View()` method to handle the overlay:

```go
func (m Model) View() string {
	if m.loading {
		title := StyleTitle.Render("⟐ BIG BOARD ⟐")
		scanning := StyleSubtitle.Render("// SCANNING REPOSITORIES...")
		return lipgloss.JoinVertical(lipgloss.Left, title, scanning)
	}

	if m.err != nil {
		return lipgloss.NewStyle().Foreground(ColorMagenta).Render(
			fmt.Sprintf("error: %v", m.err),
		)
	}

	if m.viewMode == ViewRepoOverlay {
		return m.renderRepoOverlay()
	}

	if m.viewMode == ViewOperative {
		return m.renderOperativeView()
	}

	return m.renderAggregateView()
}
```

- [ ] **Step 5: Verify it compiles** (the `renderRepoOverlay` method will be added in Task 5)

This step should be deferred — compile check happens after Task 5.

- [ ] **Step 6: Commit** (combined with Task 5)

---

### Task 5: Create the repo overlay renderer

**Files:**
- Create: `tui/repooverlay.go`
- Create: `tui/repooverlay_test.go`

- [ ] **Step 1: Write failing test for overlay rendering**

Create `tui/repooverlay_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

func TestRenderRepoOverlayContent(t *testing.T) {
	m := Model{
		repoNames:       []string{"repo-a", "repo-b", "repo-c"},
		overlayExcluded: map[string]bool{"repo-b": true},
		overlayCursor:   0,
		viewMode:        ViewRepoOverlay,
		width:           80,
	}

	result := m.renderRepoOverlay()

	// Should contain the title
	if !strings.Contains(result, "REPOSITORIES") {
		t.Error("expected REPOSITORIES title in overlay")
	}

	// Should show repo-a as included (checkbox checked)
	if !strings.Contains(result, "[x]") {
		t.Error("expected [x] checkbox for included repo")
	}

	// Should show repo-b as excluded (checkbox unchecked)
	if !strings.Contains(result, "[ ]") {
		t.Error("expected [ ] checkbox for excluded repo")
	}

	// Should contain all repo names
	for _, name := range m.repoNames {
		if !strings.Contains(result, name) {
			t.Errorf("expected repo name %q in overlay", name)
		}
	}

	// Should contain help text
	if !strings.Contains(result, "space") || !strings.Contains(result, "toggle") {
		t.Error("expected help text mentioning space/toggle")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/rdh/src/bigboard && go test ./tui/ -run TestRenderRepoOverlayContent -v`
Expected: FAIL — `renderRepoOverlay` undefined

- [ ] **Step 3: Write the overlay renderer**

Create `tui/repooverlay.go`:

```go
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderRepoOverlay renders the full-screen repo toggle overlay.
func (m Model) renderRepoOverlay() string {
	var sections []string

	// Header
	title := StyleTitle.Render("⟐ REPOSITORIES ⟐")
	subtitle := StyleSubtitle.Render("// TOGGLE REPO INCLUSION")
	separator := StyleDimCyan.Render(strings.Repeat("─", m.width))
	sections = append(sections, lipgloss.JoinVertical(lipgloss.Left, title, subtitle, separator))

	// Sort repo names for stable display
	sorted := make([]string, len(m.repoNames))
	copy(sorted, m.repoNames)
	sort.Strings(sorted)

	// Repo list with checkboxes
	for i, name := range sorted {
		checkbox := "[x]"
		if m.overlayExcluded[name] {
			checkbox = "[ ]"
		}

		cursor := "  "
		if i == m.overlayCursor {
			cursor = StyleCursor.Render("▸ ")
		}

		checkStyle := StyleCyan
		nameStyle := StyleAuthor
		if m.overlayExcluded[name] {
			checkStyle = StyleDimWhite
			nameStyle = StyleDimWhite
		}

		line := cursor + checkStyle.Render(checkbox) + " " + nameStyle.Render(name)

		var rowStyle lipgloss.Style
		switch {
		case i == m.overlayCursor:
			rowStyle = StyleRowSelected
		case i%2 == 0:
			rowStyle = StyleRowEven
		default:
			rowStyle = StyleRowOdd
		}
		sections = append(sections, rowStyle.Render(line))
	}

	// Summary
	sections = append(sections, "")
	excluded := len(m.overlayExcluded)
	total := len(m.repoNames)
	summary := RenderRepoCount(total, excluded)
	sections = append(sections, summary)

	// Help bar
	sections = append(sections, "")
	help := StyleHelpKey.Render("[space]") + StyleHelpDesc.Render("toggle") + " " +
		StyleHelpKey.Render("[enter/esc]") + StyleHelpDesc.Render("done") + " " +
		StyleHelpKey.Render("[↑↓]") + StyleHelpDesc.Render("navigate")
	sections = append(sections, help)

	return strings.Join(sections, "\n")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/rdh/src/bigboard && go test ./tui/ -run TestRenderRepoOverlayContent -v`
Expected: PASS

- [ ] **Step 5: Verify full compile and all tests pass**

Run: `cd /Users/rdh/src/bigboard && go build ./cmd/bigboard/ && go test ./... -v`
Expected: Compiles, all tests PASS

- [ ] **Step 6: Commit Tasks 4 and 5 together**

```bash
git add tui/app.go tui/repooverlay.go tui/repooverlay_test.go
git commit -m "feat: add interactive repo overlay for toggling repo exclusion"
```

---

### Task 6: Update help bar to show `[r]` key hint

**Files:**
- Modify: `tui/components.go`

- [ ] **Step 1: Add `[r]epos` to the aggregate help bar**

In `tui/components.go`, update the aggregate bindings in `RenderHelpBar` (line 136-141):

```go
	default: // aggregate
		bindings = []struct{ key, desc string }{
			{"[q]", "uit"},
			{"[s]", "ort"},
			{"[r]", "epos"},
			{"[↵]", "detail"},
			{"[←→]", "time"},
		}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/rdh/src/bigboard && go build ./cmd/bigboard/`
Expected: Compiles

- [ ] **Step 3: Commit**

```bash
git add tui/components.go
git commit -m "feat: add [r]epos key hint to aggregate help bar"
```

---

### Task 7: Update footer to reflect active repo count

**Files:**
- Modify: `tui/app.go`
- Modify: `tui/components.go`

- [ ] **Step 1: Update `RenderFooter` to accept excluded count**

In `tui/components.go`, update `RenderFooter` signature and body:

```go
func RenderFooter(repoCount, excludedCount, width int) string {
	status := fmt.Sprintf("SYS.STATUS: NOMINAL // %d repos scanned", repoCount)
	if excludedCount > 0 {
		status = fmt.Sprintf("SYS.STATUS: NOMINAL // %d/%d repos active", repoCount-excludedCount, repoCount)
	}
	left := StyleFooter.Render(status)
	right := StyleFooter.Render(time.Now().Format("2006-01-02 15:04:05"))

	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(right)
	gap := width - leftLen - rightLen
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}
```

- [ ] **Step 2: Update all callers of `RenderFooter`**

In `tui/app.go`, update `renderAggregateView` (line 251):

```go
	sections = append(sections, RenderFooter(len(m.repoNames), len(m.excludedRepos), m.width))
```

In `tui/app.go`, update `renderOperativeView` (line 275):

```go
	footer := RenderFooter(len(m.repoNames), len(m.excludedRepos), m.width)
```

- [ ] **Step 3: Verify compile and tests**

Run: `cd /Users/rdh/src/bigboard && go build ./cmd/bigboard/ && go test ./... -v`
Expected: Compiles, all tests PASS

- [ ] **Step 4: Commit**

```bash
git add tui/app.go tui/components.go
git commit -m "feat: update footer to show active repo count when repos excluded"
```

---

### Task 8: Sort `repoNames` on data load for stable overlay display

**Files:**
- Modify: `tui/app.go`

- [ ] **Step 1: Sort repo names when data is loaded**

In `tui/app.go`, in the `DataLoadedMsg` handler (around line 127), add a sort after setting `m.repoNames`:

```go
	case DataLoadedMsg:
		if msg.Err != nil {
			m.err = msg.Err
			m.loading = false
			return m, nil
		}
		m.allRecords = msg.Records
		m.repoNames = msg.RepoNames
		sort.Strings(m.repoNames)
		m.loading = false
		m.recomputeAuthors()
```

Add `"sort"` to the imports in `tui/app.go`.

- [ ] **Step 2: Remove the sort from `renderRepoOverlay`**

In `tui/repooverlay.go`, remove the sort and copy since `m.repoNames` is already sorted. Replace:

```go
	// Sort repo names for stable display
	sorted := make([]string, len(m.repoNames))
	copy(sorted, m.repoNames)
	sort.Strings(sorted)
```

With just using `m.repoNames` directly in the loop:

```go
	// Repo list with checkboxes
	for i, name := range m.repoNames {
```

Also remove the `"sort"` import from `repooverlay.go`.

- [ ] **Step 3: Verify compile and tests**

Run: `cd /Users/rdh/src/bigboard && go build ./cmd/bigboard/ && go test ./... -v`
Expected: Compiles, all tests PASS

- [ ] **Step 4: Commit**

```bash
git add tui/app.go tui/repooverlay.go
git commit -m "refactor: sort repo names once on load instead of per-render"
```

---

### Task 9: Manual smoke test

- [ ] **Step 1: Build and run with `--exclude`**

Run: `cd /Users/rdh/src/bigboard && go build -o bigboard ./cmd/bigboard/ && ./bigboard ~/dev --exclude pod-integrations-kb`

Verify:
- Header shows `N-1/N repos` (or just `N repos` if the excluded name doesn't match)
- Footer shows active repo count
- Stats don't include the excluded repo's data
- Press `r` to open overlay — excluded repo should show `[ ]`
- Toggle a repo with Space, dismiss with Enter — stats should update
- Time range cycling should preserve exclusions
- Press `q` to quit
