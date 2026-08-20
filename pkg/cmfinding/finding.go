package cmfinding

// ImportedFinding represents a normalized finding ready for cm report import.
type ImportedFinding struct {
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	VulnType string `json:"vuln_type"`
	Snippet  string `json:"snippet"`
	Status   string `json:"status,omitempty"`
}
