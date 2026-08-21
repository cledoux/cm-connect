package cmfinding

// Finding represents the canonical finding schema emitted by CodeMender's `cm report -f json`.
type Finding struct {
	FindingID       string  `json:"FindingID,omitempty"`
	SessionID       string  `json:"SessionID,omitempty"`
	Title           string  `json:"Title,omitempty"`
	FilePath        string  `json:"FilePath"`
	Severity        string  `json:"Severity,omitempty"`
	Confidence      float64 `json:"Confidence,omitempty"`
	ConfidenceLevel string  `json:"ConfidenceLevel,omitempty"`
	Analysis        string  `json:"Analysis,omitempty"`
	Snippet         string  `json:"Snippet,omitempty"`
	VulnType        string  `json:"VulnType,omitempty"`
	VulnID          string  `json:"VulnID,omitempty"`
	Fingerprint     string  `json:"Fingerprint,omitempty"`
	Status          string  `json:"Status,omitempty"`
	SourceStage     string  `json:"SourceStage,omitempty"`
	FindingJSON     string  `json:"FindingJSON,omitempty"`
	UpdatedAt       string  `json:"UpdatedAt,omitempty"`
	StartLine       int     `json:"StartLine,omitempty"`
	EndLine         int     `json:"EndLine,omitempty"`
	DismissReason   string  `json:"DismissReason,omitempty"`
}

// FindingImport represents the schema consumed by CodeMender's `cm report import`.
type FindingImport struct {
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	VulnType string `json:"vuln_type"`
	Snippet  string `json:"snippet"`
	Status   string `json:"status,omitempty"`
}
