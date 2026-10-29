package gui

import "github.com/richhaase/bigboard/stats"

type Settings struct {
	Mode                 string   `json:"mode"`
	PersonalAccount      string   `json:"personalAccount"`
	Organizations        []string `json:"organizations"`
	Repositories         []string `json:"repositories"`
	ExcludedRepositories []string `json:"excludedRepositories"`
}

type Repository struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Branch              string `json:"branch"`
	Revision            string `json:"revision"`
	Stale               bool   `json:"stale"`
	Error               string `json:"error"`
	PushedAt            string `json:"pushedAt"`
	ScannedAt           string `json:"scannedAt"`
	PRCheckedAt         string `json:"prCheckedAt"`
	PRInventoryComplete bool   `json:"prInventoryComplete"`
	PRFilesComplete     bool   `json:"prFilesComplete"`
	PRReviewersComplete bool   `json:"prReviewersComplete"`
	Fetched             bool   `json:"-"`
	Included            bool   `json:"included"`
	PRStale             bool   `json:"prStale"`
}

type File struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

type Commit struct {
	ID           string `json:"id"`
	Repo         string `json:"repo"`
	Author       string `json:"author"`
	Email        string `json:"email"`
	Date         string `json:"date"`
	Subject      string `json:"subject"`
	Added        int    `json:"added"`
	Removed      int    `json:"removed"`
	AI           bool   `json:"ai"`
	Bot          bool   `json:"bot"`
	LinesUnknown bool   `json:"linesUnknown"`
	Files        []File `json:"files"`
}

type PR struct {
	Repo              string   `json:"repo"`
	URL               string   `json:"url"`
	Title             string   `json:"title"`
	Author            string   `json:"author"`
	Review            string   `json:"review"`
	Checks            string   `json:"checks"`
	UpdatedAt         string   `json:"updatedAt"`
	Number            int      `json:"number"`
	Draft             bool     `json:"draft"`
	Complete          bool     `json:"complete"`
	Files             []string `json:"files"`
	Reviewers         []string `json:"reviewers"`
	Reviews           []Review `json:"reviews"`
	CheckedAt         string   `json:"checkedAt"`
	Mergeable         string   `json:"mergeable"`
	FileCount         int      `json:"fileCount"`
	InventoryComplete bool     `json:"inventoryComplete"`
	FilesComplete     bool     `json:"filesComplete"`
	ReviewersComplete bool     `json:"reviewersComplete"`
	ReviewsComplete   bool     `json:"reviewsComplete"`
}

type Review struct {
	Author      string `json:"author"`
	State       string `json:"state"`
	SubmittedAt string `json:"submittedAt"`
}

type Preferences struct {
	Since string `json:"since"`
	Sort  string `json:"sort"`
}

type Bookmark struct {
	SeenAt     string `json:"seenAt"`
	EligibleAt string `json:"eligibleAt"`
	Message    string `json:"message"`
	Path       string `json:"path"`
}

type Status struct {
	Phase             string   `json:"phase"`
	Message           string   `json:"message"`
	Errors            []string `json:"errors"`
	CachePath         string   `json:"cachePath"`
	ProbeRequests     int      `json:"probeRequests"`
	InventoryRequests int      `json:"inventoryRequests"`
	FileRequests      int      `json:"fileRequests"`
	AccountRequests   int      `json:"accountRequests"`
}

type Snapshot struct {
	Mode         string                          `json:"mode"`
	GeneratedAt  string                          `json:"generatedAt"`
	Viewer       string                          `json:"viewer"`
	Repositories []Repository                    `json:"repositories"`
	Commits      []Commit                        `json:"commits"`
	PullRequests []PR                            `json:"pullRequests"`
	Settings     Settings                        `json:"settings"`
	Status       Status                          `json:"status"`
	Preferences  Preferences                     `json:"preferences"`
	Groups       map[string][]string             `json:"groups"`
	ActiveGroup  string                          `json:"activeGroup"`
	WorkAreas    map[string][]stats.WorkAreaRule `json:"workAreas"`
	Bookmark     Bookmark                        `json:"bookmark"`
}

type IdentityOption struct {
	Email   string   `json:"email"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

type IdentityOptions struct {
	Identities []IdentityOption `json:"identities"`
}
type IdentityRequest struct {
	PrimaryEmail string   `json:"primaryEmail"`
	Emails       []string `json:"emails"`
	Name         string   `json:"name"`
}
type ControlledRepository struct {
	Repository
	Included   bool `json:"included"`
	Selectable bool `json:"selectable"`
	Listed     bool `json:"listed"`
	Cached     bool `json:"cached"`
	CanRemove  bool `json:"canRemove"`
}
type RepositoryControls struct {
	Repositories []ControlledRepository `json:"repositories"`
	CacheBytes   int64                  `json:"cacheBytes"`
	CachePath    string                 `json:"cachePath"`
	Errors       []string               `json:"errors"`
}
type RepositorySelection struct {
	Included []string `json:"included"`
}
type PathRequest struct {
	Repo     string `json:"repo"`
	Path     string `json:"path"`
	Revision string `json:"revision"`
}
