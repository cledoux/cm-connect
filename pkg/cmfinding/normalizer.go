package cmfinding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type rawFinding struct {
	FilePath      string `json:"FilePath"`
	FilePathAlt   string `json:"file_path"`
	FilePathCamel string `json:"filePath"`

	StartLine    int `json:"StartLine"`
	StartLineAlt int `json:"start_line"`
	Line         int `json:"Line"`
	LineAlt      int `json:"line"`

	Title    string `json:"Title"`
	TitleAlt string `json:"title"`

	Analysis    string `json:"Analysis"`
	AnalysisAlt string `json:"analysis"`
	Message     string `json:"Message"`
	MessageAlt  string `json:"message"`

	Severity    string `json:"Severity"`
	SeverityAlt string `json:"severity"`

	VulnType      string `json:"VulnType"`
	VulnTypeAlt   string `json:"vuln_type"`
	VulnTypeCamel string `json:"vulnType"`
	VulnID        string `json:"VulnID"`
	VulnIDAlt     string `json:"vuln_id"`
	VulnIDCamel   string `json:"vulnId"`

	Snippet    string `json:"Snippet"`
	SnippetAlt string `json:"snippet"`

	Status    string `json:"Status"`
	StatusAlt string `json:"status"`
}

func (r *rawFinding) getFilePath() string {
	if r.FilePath != "" {
		return r.FilePath
	}
	if r.FilePathAlt != "" {
		return r.FilePathAlt
	}
	return r.FilePathCamel
}

func (r *rawFinding) getLine() int {
	if r.StartLine != 0 {
		return r.StartLine
	}
	if r.StartLineAlt != 0 {
		return r.StartLineAlt
	}
	if r.Line != 0 {
		return r.Line
	}
	return r.LineAlt
}

func (r *rawFinding) getTitle() string {
	if r.Title != "" {
		return r.Title
	}
	return r.TitleAlt
}

func (r *rawFinding) getMessage() string {
	if r.Analysis != "" {
		return r.Analysis
	}
	if r.AnalysisAlt != "" {
		return r.AnalysisAlt
	}
	if r.Message != "" {
		return r.Message
	}
	return r.MessageAlt
}

func (r *rawFinding) getSeverity() string {
	if r.Severity != "" {
		return r.Severity
	}
	return r.SeverityAlt
}

func (r *rawFinding) getVulnType() string {
	if r.VulnType != "" {
		return r.VulnType
	}
	if r.VulnTypeAlt != "" {
		return r.VulnTypeAlt
	}
	if r.VulnTypeCamel != "" {
		return r.VulnTypeCamel
	}
	if r.VulnID != "" {
		return r.VulnID
	}
	if r.VulnIDAlt != "" {
		return r.VulnIDAlt
	}
	return r.VulnIDCamel
}

func (r *rawFinding) getSnippet() string {
	if r.Snippet != "" {
		return r.Snippet
	}
	return r.SnippetAlt
}

func (r *rawFinding) getStatus() string {
	if r.Status != "" {
		return r.Status
	}
	return r.StatusAlt
}

// Normalize parses a single finding payload (JSON object or 1-element array),
// validates target file existence in workspaceRoot, normalizes fields according to REQ-0003,
// and returns the serialized JSON array bytes and the typed ImportedFinding.
func Normalize(rawJSON []byte, workspaceRoot string) ([]byte, *ImportedFinding, error) {
	trimmed := bytes.TrimSpace(rawJSON)
	if len(trimmed) == 0 {
		return nil, nil, fmt.Errorf("empty finding JSON input")
	}

	// Disallow literal null or non-object/array JSON primitives
	if bytes.Equal(trimmed, []byte("null")) {
		return nil, nil, fmt.Errorf("null JSON input is invalid")
	}

	var raw rawFinding
	if trimmed[0] == '[' {
		var list []rawFinding
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, nil, fmt.Errorf("invalid finding array JSON: %w", err)
		}
		if len(list) != 1 {
			return nil, nil, fmt.Errorf("finding array must contain exactly 1 element, got %d", len(list))
		}
		raw = list[0]
	} else if trimmed[0] == '{' {
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return nil, nil, fmt.Errorf("invalid finding object JSON: %w", err)
		}
	} else {
		return nil, nil, fmt.Errorf("finding JSON must be an object or 1-element array")
	}

	rawPath := raw.getFilePath()
	if rawPath == "" {
		return nil, nil, fmt.Errorf("missing or empty file path in finding")
	}

	// Strip file:// prefix
	cleaned := rawPath
	if strings.HasPrefix(cleaned, "file://") {
		cleaned = strings.TrimPrefix(cleaned, "file://")
	}

	// Strip /workspace/ or workspace prefix if present
	if strings.HasPrefix(cleaned, "/workspace/") {
		cleaned = strings.TrimPrefix(cleaned, "/workspace/")
	} else if cleaned == "/workspace" {
		cleaned = ""
	} else if strings.HasPrefix(cleaned, "/") {
		cleaned = strings.TrimPrefix(cleaned, "/")
	}

	cleaned = filepath.Clean(cleaned)
	if cleaned == "." || strings.HasPrefix(cleaned, "..") {
		return nil, nil, fmt.Errorf("target file path %q traverses outside workspace", rawPath)
	}

	if workspaceRoot == "" {
		workspaceRoot = "/workspace"
	}

	fullPath := filepath.Join(workspaceRoot, cleaned)
	rel, err := filepath.Rel(workspaceRoot, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, nil, fmt.Errorf("target file %q is outside workspace %q", rawPath, workspaceRoot)
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("target file %q does not exist in workspace (%s)", cleaned, fullPath)
		}
		return nil, nil, fmt.Errorf("failed to stat target file %q: %w", fullPath, err)
	}
	if info.IsDir() {
		return nil, nil, fmt.Errorf("target file %q is a directory, expected regular file", cleaned)
	}

	line := raw.getLine()
	if line < 1 {
		line = 1
	}

	title := raw.getTitle()
	if title == "" {
		title = "Security Finding"
	}

	snippet := raw.getSnippet()

	message := raw.getMessage()
	if message == "" {
		if snippet != "" {
			message = snippet
		} else {
			message = title
		}
	}

	severity := raw.getSeverity()
	if severity == "" {
		severity = "HIGH"
	} else {
		severity = strings.ToUpper(severity)
	}

	status := raw.getStatus()
	if status == "" {
		status = "OPEN"
	}

	vulnType := raw.getVulnType()

	imported := &ImportedFinding{
		FilePath: cleaned,
		Line:     line,
		Title:    title,
		Message:  message,
		Severity: severity,
		VulnType: vulnType,
		Snippet:  snippet,
		Status:   status,
	}

	outBytes, err := json.MarshalIndent([]*ImportedFinding{imported}, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal imported finding: %w", err)
	}

	return outBytes, imported, nil
}
