//go:build integration

package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// workflowMatrixItem represents the dynamic matrix JSON element formatted for GitHub Actions strategy: matrix.
type workflowMatrixItem struct {
	FindingID string          `json:"finding_id"`
	FilePath  string          `json:"file_path"`
	StartLine int             `json:"start_line"`
	Severity  string          `json:"severity"`
	Title     string          `json:"title"`
	Payload   json.RawMessage `json:"payload"`
}

func getFilterScriptPath(t *testing.T) string {
	t.Helper()
	repoRoot := getRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "github-actions", "scripts", "filter_findings.jq")
	return scriptPath
}

func runFilterFindingsJQ(t *testing.T, diffPath string, findingsPath string, extraArgs ...string) []workflowMatrixItem {
	t.Helper()
	scriptPath := getFilterScriptPath(t)

	args := []string{
		"--rawfile", "diff", diffPath,
		"-f", scriptPath,
	}
	args = append(args, extraArgs...)
	args = append(args, findingsPath)

	stdout, stderr, exitCode := runCommand(t, 5*time.Second, "", nil, "jq", args...)
	if exitCode != 0 {
		t.Fatalf("jq script execution failed with exit code %d\nstderr: %s\nstdout: %s", exitCode, stderr, stdout)
	}

	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" || trimmed == "null" {
		return []workflowMatrixItem{}
	}

	var items []workflowMatrixItem
	if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
		t.Fatalf("failed to unmarshal jq output %q: %v", trimmed, err)
	}
	return items
}

// TestWorkflow_FilterFindings executes the entire suite of filter_findings test scenarios.
func TestWorkflow_FilterFindings(t *testing.T) {
	t.Run("InDiff", TestWorkflowFilterInDiff)
	t.Run("OutOfDiff", TestWorkflowFilterOutOfDiff)
	t.Run("Mixed", TestWorkflowFilterMixed)
	t.Run("MaxThrottling", TestWorkflowFilterMaxThrottling)
	t.Run("EmptyFindings", TestWorkflowFilterEmptyFindings)
	t.Run("EmptyDiff", TestWorkflowFilterEmptyDiff)
}

// Scenario 1: Verify findings wholly inside commit.diff hunks are preserved in matrix (REQ-0004)
func TestWorkflowFilterInDiff(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	diffPath := filepath.Join(dir, "commit.diff")
	findingsPath := filepath.Join(dir, "findings_in_diff.json")

	items := runFilterFindingsJQ(t, diffPath, findingsPath)

	if len(items) != 2 {
		t.Fatalf("expected 2 matrix items, got %d", len(items))
	}

	// Verify schema and fields
	for _, item := range items {
		if item.FindingID == "" {
			t.Errorf("FindingID is empty")
		}
		if item.FilePath == "" {
			t.Errorf("FilePath is empty")
		}
		if item.StartLine <= 0 {
			t.Errorf("StartLine = %d, want > 0", item.StartLine)
		}
		if item.Severity == "" {
			t.Errorf("Severity is empty")
		}
		if item.Title == "" {
			t.Errorf("Title is empty")
		}
		if len(item.Payload) == 0 {
			t.Errorf("Payload is empty")
		}

		var rawObj map[string]interface{}
		if err := json.Unmarshal(item.Payload, &rawObj); err != nil {
			t.Errorf("Payload is not valid JSON object: %v", err)
		}
		if rawObj["FindingID"] != item.FindingID {
			t.Errorf("Payload FindingID %v does not match item.FindingID %v", rawObj["FindingID"], item.FindingID)
		}
	}
}

// Scenario 2: Discard out-of-diff findings (REQ-0004)
func TestWorkflowFilterOutOfDiff(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	diffPath := filepath.Join(dir, "commit.diff")
	findingsPath := filepath.Join(dir, "findings_out_of_diff.json")

	items := runFilterFindingsJQ(t, diffPath, findingsPath)

	if len(items) != 0 {
		t.Fatalf("expected 0 matrix items for out-of-diff findings, got %d: %+v", len(items), items)
	}
}

// Scenario 3: Mixed findings are filtered and sorted by severity (REQ-0004)
func TestWorkflowFilterMixed(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	diffPath := filepath.Join(dir, "commit.diff")
	findingsPath := filepath.Join(dir, "findings_mixed.json")

	items := runFilterFindingsJQ(t, diffPath, findingsPath)

	if len(items) != 3 {
		t.Fatalf("expected 3 matrix items, got %d", len(items))
	}

	// Verify order: CRITICAL > HIGH > MEDIUM > LOW
	expectedSeverities := []string{"CRITICAL", "HIGH", "MEDIUM"}
	for i, expected := range expectedSeverities {
		if items[i].Severity != expected {
			t.Errorf("items[%d].Severity = %q, want %q", i, items[i].Severity, expected)
		}
	}

	// Verify specific finding IDs match expected in-diff items
	if items[0].FilePath != "cmd/server/main.go" || items[0].StartLine != 81 {
		t.Errorf("items[0] mismatch: %+v", items[0])
	}
	if items[1].FilePath != "pkg/auth/store.go" || items[1].StartLine != 42 {
		t.Errorf("items[1] mismatch: %+v", items[1])
	}
	if items[2].FilePath != "pkg/auth/store.go" || items[2].StartLine != 44 {
		t.Errorf("items[2] mismatch: %+v", items[2])
	}
}

// Scenario 4: Max findings threshold truncates output (REQ-0004)
func TestWorkflowFilterMaxThrottling(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	diffPath := filepath.Join(dir, "commit.diff")
	findingsPath := filepath.Join(dir, "findings_mixed.json")

	items := runFilterFindingsJQ(t, diffPath, findingsPath, "--argjson", "max_findings", "2")

	if len(items) != 2 {
		t.Fatalf("expected 2 matrix items with max_findings=2, got %d", len(items))
	}

	if items[0].Severity != "CRITICAL" {
		t.Errorf("items[0].Severity = %q, want CRITICAL", items[0].Severity)
	}
	if items[1].Severity != "HIGH" {
		t.Errorf("items[1].Severity = %q, want HIGH", items[1].Severity)
	}
}

// Scenario 5: Empty findings list returns empty matrix (REQ-0004)
func TestWorkflowFilterEmptyFindings(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	diffPath := filepath.Join(dir, "commit.diff")

	tmpFile := filepath.Join(t.TempDir(), "empty_findings.json")
	if err := os.WriteFile(tmpFile, []byte("[]"), 0644); err != nil {
		t.Fatalf("failed to write empty findings: %v", err)
	}

	items := runFilterFindingsJQ(t, diffPath, tmpFile)

	if len(items) != 0 {
		t.Fatalf("expected 0 matrix items for empty findings, got %d", len(items))
	}
}

// Scenario 6: Empty diff discards all findings (REQ-0001, REQ-0004)
func TestWorkflowFilterEmptyDiff(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	findingsPath := filepath.Join(dir, "findings_in_diff.json")

	tmpDiff := filepath.Join(t.TempDir(), "empty.diff")
	if err := os.WriteFile(tmpDiff, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write empty diff: %v", err)
	}

	items := runFilterFindingsJQ(t, tmpDiff, findingsPath)

	if len(items) != 0 {
		t.Fatalf("expected 0 matrix items for empty diff, got %d", len(items))
	}
}
