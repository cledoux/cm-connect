//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleFindingJSON = `{
  "FilePath": "src/auth/auth.go",
  "StartLine": 1,
  "Title": "Hardcoded Credential",
  "Analysis": "Remove credential",
  "Severity": "HIGH",
  "VulnType": "CWE-798",
  "Snippet": "package auth"
}`

// Scenario 13: Fix with Positional Finding File (REQ-0002, REQ-0003, REQ-0008)
func TestFixPositionalFindingFile(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	findingPath := filepath.Join(ws, "test-finding.json")
	if err := os.WriteFile(findingPath, []byte(sampleFindingJSON), 0666); err != nil {
		t.Fatalf("failed to write finding JSON fixture: %v", err)
	}

	stdout, stderr, exitCode := runDocker(t, 15*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "fix", "/workspace/test-finding.json")

	t.Logf("fix positional output: exit=%d stdout=%s stderr=%s", exitCode, stdout, stderr)
	if exitCode != 0 && exitCode != 1 {
		t.Errorf("fix positional exit code = %d, want 0 (fixed) or 1 (unresolved)", exitCode)
	}
}

// Scenario 14: Fix with Stdin Ingestion Channel (-) (REQ-0002, REQ-0008)
func TestFixStdinIngestionChannel(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	stdin := strings.NewReader(sampleFindingJSON)
	stdout, stderr, exitCode := runDocker(t, 15*time.Second, stdin, "-i", "--rm", "-v", ws+":/workspace", image, "fix", "-")

	t.Logf("fix stdin output: exit=%d stdout=%s stderr=%s", exitCode, stdout, stderr)
	if exitCode != 0 && exitCode != 1 {
		t.Errorf("fix stdin exit code = %d, want 0 (fixed) or 1 (unresolved)", exitCode)
	}
}

// Scenario 15: Error on Fix Without Target Argument (REQ-0002)
func TestFixMissingTargetArgument(t *testing.T) {
	image := getImageName()
	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", image, "fix")

	if exitCode != 2 {
		t.Errorf("fix missing target exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr, "missing target finding argument") {
		t.Errorf("stderr %q does not contain %q", stderr, "missing target finding argument")
	}
}

// Scenario 16: Error on Non-Existent Finding File (REQ-0002)
func TestFixNonExistentFindingFile(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "fix", "/workspace/non_existent.json")

	if exitCode != 2 {
		t.Errorf("fix non-existent file exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr, "finding file not found") {
		t.Errorf("stderr %q does not contain %q", stderr, "finding file not found")
	}
}

// Scenario 17: Strict Subcommand Dispatch for Fix (Reject cm fix Prefix) (REQ-0001)
func TestFixRejectCMFixPrefix(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	findingPath := filepath.Join(ws, "test-finding.json")
	if err := os.WriteFile(findingPath, []byte(sampleFindingJSON), 0666); err != nil {
		t.Fatalf("failed to write finding fixture: %v", err)
	}

	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "cm", "fix", "/workspace/test-finding.json")

	if exitCode != 2 {
		t.Errorf("cm fix prefix invocation exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr, "unrecognized subcommand 'cm'") {
		t.Errorf("stderr %q does not contain %q", stderr, "unrecognized subcommand 'cm'")
	}
}

// Scenario 18: Fix Subcommand Help Flag (REQ-0002)
func TestFixHelpFlag(t *testing.T) {
	image := getImageName()
	stdout, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", image, "fix", "--help")

	if exitCode != 0 {
		t.Errorf("fix --help exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout+stderr, "cm-runner fix") {
		t.Errorf("fix --help output %q missing 'cm-runner fix'", stdout+stderr)
	}
}

// Scenario 19: Host Workspace Immutability Validation (REQ-0007, ADR-0003)
func TestHostWorkspaceImmutability(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	findingPath := filepath.Join(ws, "test-finding.json")
	if err := os.WriteFile(findingPath, []byte(sampleFindingJSON), 0666); err != nil {
		t.Fatalf("failed to write finding fixture: %v", err)
	}

	targetFile := filepath.Join(ws, "src", "auth", "auth.go")
	initialChecksum := calculateSHA256(t, targetFile)

	runDocker(t, 15*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "fix", "/workspace/test-finding.json")

	finalChecksum := calculateSHA256(t, targetFile)
	if initialChecksum != finalChecksum {
		t.Errorf("host workspace modified! initial SHA = %s, final SHA = %s", initialChecksum, finalChecksum)
	}
}
