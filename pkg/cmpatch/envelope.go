package cmpatch

// Hunk represents a structured file replacement block parsed from a unified diff.
type Hunk struct {
	FilePath    string `json:"file_path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

// ChangeEnvelope is the machine-readable remediation payload emitted to stdout.
type ChangeEnvelope struct {
	FindingID     string   `json:"finding_id"`
	Status        string   `json:"status"` // "FIXED" or "UNRESOLVED"
	VulnType      string   `json:"vuln_type,omitempty"`
	Title         string   `json:"title,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	FilesModified []string `json:"files_modified"`
	Patch         string   `json:"patch"`
	Hunks         []Hunk   `json:"hunks"`
}
