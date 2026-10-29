package gui

type Settings struct {
	Mode            string   `json:"mode"`
	PersonalAccount string   `json:"personalAccount"`
	Organizations   []string `json:"organizations"`
	Repositories    []string `json:"repositories"`
}

type Repository struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Branch   string `json:"branch"`
	Revision string `json:"revision"`
	Stale    bool   `json:"stale"`
	Error    string `json:"error"`
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
	Repo      string   `json:"repo"`
	URL       string   `json:"url"`
	Title     string   `json:"title"`
	Author    string   `json:"author"`
	Review    string   `json:"review"`
	Checks    string   `json:"checks"`
	UpdatedAt string   `json:"updatedAt"`
	Number    int      `json:"number"`
	Draft     bool     `json:"draft"`
	Complete  bool     `json:"complete"`
	Files     []string `json:"files"`
	Reviewers []string `json:"reviewers"`
}

type Status struct {
	Phase     string   `json:"phase"`
	Message   string   `json:"message"`
	Errors    []string `json:"errors"`
	CachePath string   `json:"cachePath"`
}

type Snapshot struct {
	Mode         string       `json:"mode"`
	GeneratedAt  string       `json:"generatedAt"`
	Viewer       string       `json:"viewer"`
	Repositories []Repository `json:"repositories"`
	Commits      []Commit     `json:"commits"`
	PullRequests []PR         `json:"pullRequests"`
	Settings     Settings     `json:"settings"`
	Status       Status       `json:"status"`
}
