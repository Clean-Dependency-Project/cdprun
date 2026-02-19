package hfpoc

// Keep these structs minimal and JSON-friendly: the PoC's main job is to
// produce machine-readable evidence (stdout) and structured logs (stderr).

type IntakeCriteria struct {
	AllowedLicenses     []string `json:"allowed_licenses"`
	AllowMissingLicense bool     `json:"allow_missing_license"`
	RequireModelCard    bool     `json:"require_model_card"`

	// Model card validations (lightweight).
	RequiredCardFields []string `json:"required_card_fields"`
	DenyTags           []string `json:"deny_tags"`

	// Popularity/age validations.
	MinDownloads30d int `json:"min_downloads_30d"`
	MinAgeDays      int `json:"min_age_days"`
}

type Policy struct {
	UpstreamRepoID string            `json:"upstream_repo_id"`
	UpstreamSHA    string            `json:"upstream_sha"`
	OrgRepoID      string            `json:"org_repo_id"`
	OrgSHA         string            `json:"org_sha"`
	AllowedFiles   []string          `json:"allowed_files"`
	SHA256ByFile   map[string]string `json:"sha256_by_file"`
}

type Result struct {
	Status  string `json:"status"` // ok|failed
	Message string `json:"message,omitempty"`

	Command string `json:"command,omitempty"`

	UpstreamRepoID string `json:"upstream_repo_id,omitempty"`
	UpstreamSHA    string `json:"upstream_sha,omitempty"`
	OrgRepoID      string `json:"org_repo_id,omitempty"`
	OrgSHA         string `json:"org_sha,omitempty"`

	SelectedFiles []string `json:"selected_files,omitempty"`

	// Evidence / counters.
	Downloaded int `json:"downloaded,omitempty"`
	Uploaded   int `json:"uploaded,omitempty"`
	Verified   int `json:"verified,omitempty"`

	Errors []string `json:"errors,omitempty"`

	// Extra details for debugging/traceability (still JSON).
	Details map[string]any `json:"details,omitempty"`
}

type ModelInfo struct {
	ID           string   `json:"id"`
	SHA          string   `json:"sha"`
	Private      bool     `json:"private"`
	Gated        any      `json:"gated"`
	LastModified string   `json:"lastModified"`
	Tags         []string `json:"tags"`
	Downloads    int      `json:"downloads"`
	CreatedAt    string   `json:"createdAt"`

	CardData map[string]any `json:"cardData"`
	Siblings []struct {
		RFilename string `json:"rfilename"`
	} `json:"siblings"`
}

type CreateRepoRequest struct {
	Name         string `json:"name"`
	Organization string `json:"organization,omitempty"`
	Private      bool   `json:"private"`
	Type         string `json:"type"` // model|dataset|space
}

type CreateRepoResponse struct {
	URL string `json:"url"`
}
