//go:build integration

package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// workflowFinding represents the PascalCase JSON schema emitted by cm report.
type workflowFinding struct {
	FindingID   string `json:"FindingID"`
	SessionID   string `json:"SessionID"`
	Title       string `json:"Title"`
	FilePath    string `json:"FilePath"`
	Severity    string `json:"Severity"`
	Confidence  int    `json:"Confidence"`
	Analysis    string `json:"Analysis"`
	Snippet     string `json:"Snippet"`
	VulnType    string `json:"VulnType"`
	VulnID      string `json:"VulnID"`
	Fingerprint string `json:"Fingerprint"`
	Status      string `json:"Status"`
	StartLine   int    `json:"StartLine"`
	EndLine     int    `json:"EndLine"`
}

// workflowHunk represents a modified code hunk in a ChangeEnvelope.
type workflowHunk struct {
	FilePath    string `json:"file_path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

// workflowChangeEnvelope represents the structured patch change envelope emitted on stdout by cm-runner fix.
type workflowChangeEnvelope struct {
	FindingID     string         `json:"finding_id"`
	Status        string         `json:"status"`
	VulnType      string         `json:"vuln_type"`
	Title         string         `json:"title"`
	Summary       string         `json:"summary"`
	FilesModified []string       `json:"files_modified"`
	Patch         string         `json:"patch"`
	Hunks         []workflowHunk `json:"hunks"`
}

// getWorkflowFixturesDir returns the absolute path to tests/fixtures/workflow.
func getWorkflowFixturesDir(t *testing.T) string {
	t.Helper()
	repoRoot := getRepoRoot(t)
	fixturesDir := filepath.Join(repoRoot, "tests", "fixtures", "workflow")
	if info, err := os.Stat(fixturesDir); err != nil || !info.IsDir() {
		t.Fatalf("fixtures directory does not exist at %s: %v", fixturesDir, err)
	}
	return fixturesDir
}

// Scenario 1: Verify tests/fixtures/workflow directory exists (REQ-0008)
func TestWorkflowFixturesDirectory(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	if dir == "" {
		t.Errorf("getWorkflowFixturesDir() returned empty path")
	}
}

// Scenario 2: Verify commit.diff represents multi-file diff (REQ-0001)
func TestWorkflowCommitDiff(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	diffPath := filepath.Join(dir, "commit.diff")

	content, err := os.ReadFile(diffPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", diffPath, err)
	}

	diffStr := string(content)
	if len(strings.TrimSpace(diffStr)) == 0 {
		t.Fatalf("commit.diff is empty")
	}

	if !strings.Contains(diffStr, "pkg/auth/store.go") {
		t.Errorf("commit.diff does not contain expected diff for pkg/auth/store.go")
	}
	if !strings.Contains(diffStr, "cmd/server/main.go") {
		t.Errorf("commit.diff does not contain expected diff for cmd/server/main.go")
	}
}

// Scenario 3: Verify findings_in_diff.json schema and keys (REQ-0004)
func TestWorkflowFindingsInDiff(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	filePath := filepath.Join(dir, "findings_in_diff.json")

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filePath, err)
	}

	var findings []workflowFinding
	if err := json.Unmarshal(content, &findings); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", filePath, err)
	}

	if len(findings) < 2 {
		t.Errorf("findings_in_diff.json contains %d findings, want >= 2", len(findings))
	}

	for i, f := range findings {
		if f.FindingID == "" {
			t.Errorf("findings[%d].FindingID is empty", i)
		}
		if f.FilePath == "" {
			t.Errorf("findings[%d].FilePath is empty", i)
		}
		if f.StartLine <= 0 {
			t.Errorf("findings[%d].StartLine = %d, want >= 1", i, f.StartLine)
		}
		if f.Severity == "" {
			t.Errorf("findings[%d].Severity is empty", i)
		}
		if f.Title == "" {
			t.Errorf("findings[%d].Title is empty", i)
		}
		if f.Analysis == "" {
			t.Errorf("findings[%d].Analysis is empty", i)
		}
	}
}

// Scenario 4: Verify findings_out_of_diff.json schema (REQ-0004)
func TestWorkflowFindingsOutOfDiff(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	filePath := filepath.Join(dir, "findings_out_of_diff.json")

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filePath, err)
	}

	var findings []workflowFinding
	if err := json.Unmarshal(content, &findings); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", filePath, err)
	}

	if len(findings) < 2 {
		t.Errorf("findings_out_of_diff.json contains %d findings, want >= 2", len(findings))
	}

	hasLegacy := false
	for _, f := range findings {
		if f.FilePath == "legacy/db.go" {
			hasLegacy = true
		}
	}
	if !hasLegacy {
		t.Errorf("findings_out_of_diff.json does not contain expected out-of-diff finding in legacy/db.go")
	}
}

// Scenario 5: Verify findings_mixed.json multi-severity composition (REQ-0004)
func TestWorkflowFindingsMixed(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	filePath := filepath.Join(dir, "findings_mixed.json")

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filePath, err)
	}

	var findings []workflowFinding
	if err := json.Unmarshal(content, &findings); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", filePath, err)
	}

	if len(findings) < 4 {
		t.Errorf("findings_mixed.json contains %d findings, want >= 4", len(findings))
	}

	severityMap := make(map[string]int)
	for _, f := range findings {
		severityMap[f.Severity]++
	}

	for _, expectedSev := range []string{"CRITICAL", "HIGH", "LOW"} {
		if severityMap[expectedSev] == 0 {
			t.Errorf("findings_mixed.json missing expected severity %q", expectedSev)
		}
	}
}

// Scenario 6: Verify change_envelope_single_line.json structure (REQ-0006)
func TestWorkflowChangeEnvelopeSingleLine(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	filePath := filepath.Join(dir, "change_envelope_single_line.json")

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filePath, err)
	}

	var env workflowChangeEnvelope
	if err := json.Unmarshal(content, &env); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", filePath, err)
	}

	if env.Status != "FIXED" {
		t.Errorf("status = %q, want FIXED", env.Status)
	}
	if len(env.FilesModified) == 0 {
		t.Errorf("files_modified is empty")
	}
	if len(env.Patch) == 0 {
		t.Errorf("patch is empty")
	}
	if len(env.Hunks) == 0 {
		t.Fatalf("hunks array is empty")
	}
	if env.Hunks[0].StartLine != env.Hunks[0].EndLine {
		t.Errorf("expected single-line hunk (start_line == end_line), got %d != %d", env.Hunks[0].StartLine, env.Hunks[0].EndLine)
	}
}

// Scenario 7: Verify change_envelope_multiline.json structure (REQ-0006)
func TestWorkflowChangeEnvelopeMultiline(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	filePath := filepath.Join(dir, "change_envelope_multiline.json")

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filePath, err)
	}

	var env workflowChangeEnvelope
	if err := json.Unmarshal(content, &env); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", filePath, err)
	}

	if env.Status != "FIXED" {
		t.Errorf("status = %q, want FIXED", env.Status)
	}
	if len(env.FilesModified) == 0 {
		t.Errorf("files_modified is empty")
	}
	if len(env.Patch) == 0 {
		t.Errorf("patch is empty")
	}
	if len(env.Hunks) == 0 {
		t.Fatalf("hunks array is empty")
	}
	if env.Hunks[0].StartLine >= env.Hunks[0].EndLine {
		t.Errorf("expected multi-line hunk (start_line < end_line), got %d >= %d", env.Hunks[0].StartLine, env.Hunks[0].EndLine)
	}
}

// Scenario 8: Verify change_envelope_unresolved.json structure (REQ-0006)
func TestWorkflowChangeEnvelopeUnresolved(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	filePath := filepath.Join(dir, "change_envelope_unresolved.json")

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filePath, err)
	}

	var env workflowChangeEnvelope
	if err := json.Unmarshal(content, &env); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", filePath, err)
	}

	if env.Status != "UNRESOLVED" {
		t.Errorf("status = %q, want UNRESOLVED", env.Status)
	}
	if len(env.FilesModified) != 0 {
		t.Errorf("files_modified = %v, want empty", env.FilesModified)
	}
	if len(env.Patch) != 0 {
		t.Errorf("patch = %q, want empty", env.Patch)
	}
	if len(env.Hunks) != 0 {
		t.Errorf("hunks count = %d, want 0", len(env.Hunks))
	}
}
