package cmfinding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NewFindingFromJSON parses a single JSON object into a canonical Finding.
func NewFindingFromJSON(rawJSON []byte) (*Finding, error) {
	trimmed := bytes.TrimSpace(rawJSON)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty finding JSON input")
	}

	if bytes.Equal(trimmed, []byte("null")) {
		return nil, fmt.Errorf("null JSON input is invalid")
	}

	if trimmed[0] != '{' {
		return nil, fmt.Errorf("finding JSON must be an object, got %q", string(trimmed))
	}

	var finding Finding
	if err := json.Unmarshal(trimmed, &finding); err != nil {
		return nil, fmt.Errorf("invalid finding object JSON: %w", err)
	}

	if finding.FilePath == "" {
		return nil, fmt.Errorf("missing or empty file path in finding")
	}

	return &finding, nil
}

// ToImport converts a canonical Finding into a FindingImport for cm report import,
// resolving and validating the target file within workspaceRoot and applying fallback defaults.
func (f *Finding) ToImport(workspaceRoot string) (*FindingImport, error) {
	cleanPath, err := resolveWorkspacePath(f.FilePath, workspaceRoot)
	if err != nil {
		return nil, err
	}

	line := f.StartLine
	if line < 1 {
		line = 1
	}

	title := f.Title
	if title == "" {
		title = "Security Finding"
	}

	message := f.Analysis
	if message == "" {
		if f.Snippet != "" {
			message = f.Snippet
		} else {
			message = title
		}
	}

	severity := f.Severity
	if severity == "" {
		severity = "HIGH"
	} else {
		severity = strings.ToUpper(severity)
	}

	status := f.Status
	if status == "" {
		status = "OPEN"
	}

	vulnType := f.VulnType
	if vulnType == "" {
		vulnType = f.VulnID
	}

	return &FindingImport{
		FilePath: cleanPath,
		Line:     line,
		Title:    title,
		Message:  message,
		Severity: severity,
		VulnType: vulnType,
		Snippet:  f.Snippet,
		Status:   status,
	}, nil
}

// ImportFinding parses a single finding JSON object, normalizes it to FindingImport,
// and returns the serialized JSON array bytes ready for `cm report import` along with the typed FindingImport.
func ImportFinding(rawJSON []byte, workspaceRoot string) ([]byte, *FindingImport, error) {
	finding, err := NewFindingFromJSON(rawJSON)
	if err != nil {
		return nil, nil, err
	}

	importItem, err := finding.ToImport(workspaceRoot)
	if err != nil {
		return nil, nil, err
	}

	outBytes, err := json.MarshalIndent([]*FindingImport{importItem}, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal imported finding: %w", err)
	}

	return outBytes, importItem, nil
}

func resolveWorkspacePath(rawPath string, workspaceRoot string) (string, error) {
	if rawPath == "" {
		return "", fmt.Errorf("missing or empty file path in finding")
	}

	cleaned := rawPath
	if strings.HasPrefix(cleaned, "file://") {
		cleaned = strings.TrimPrefix(cleaned, "file://")
	}

	if strings.HasPrefix(cleaned, "/workspace/") {
		cleaned = strings.TrimPrefix(cleaned, "/workspace/")
	} else if cleaned == "/workspace" {
		cleaned = ""
	} else if strings.HasPrefix(cleaned, "/") {
		cleaned = strings.TrimPrefix(cleaned, "/")
	}

	cleaned = filepath.Clean(cleaned)
	if cleaned == "." || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("target file path %q traverses outside workspace", rawPath)
	}

	if workspaceRoot == "" {
		workspaceRoot = "/workspace"
	}

	fullPath := filepath.Join(workspaceRoot, cleaned)
	rel, err := filepath.Rel(workspaceRoot, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("target file %q is outside workspace %q", rawPath, workspaceRoot)
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("target file %q does not exist in workspace (%s)", cleaned, fullPath)
		}
		return "", fmt.Errorf("failed to stat target file %q: %w", fullPath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("target file %q is a directory, expected regular file", cleaned)
	}

	return cleaned, nil
}
