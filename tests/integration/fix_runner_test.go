//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, os.WriteFile(findingPath, []byte(sampleFindingJSON), 0666))

	stdout, stderr, exitCode := runDocker(t, 15*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "fix", "/workspace/test-finding.json")

	t.Logf("fix positional output: exit=%d stdout=%s stderr=%s", exitCode, stdout, stderr)
	assert.True(t, exitCode == 0 || exitCode == 1, "fix execution must exit with 0 (fixed) or 1 (unresolved), got %d", exitCode)
}

// Scenario 14: Fix with Stdin Ingestion Channel (-) (REQ-0002, REQ-0008)
func TestFixStdinIngestionChannel(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	stdin := strings.NewReader(sampleFindingJSON)
	stdout, stderr, exitCode := runDocker(t, 15*time.Second, stdin, "-i", "--rm", "-v", ws+":/workspace", image, "fix", "-")

	t.Logf("fix stdin output: exit=%d stdout=%s stderr=%s", exitCode, stdout, stderr)
	assert.True(t, exitCode == 0 || exitCode == 1, "fix with stdin must exit with 0 (fixed) or 1 (unresolved), got %d", exitCode)
}

// Scenario 15: Error on Fix Without Target Argument (REQ-0002)
func TestFixMissingTargetArgument(t *testing.T) {
	image := getImageName()
	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", image, "fix")

	assert.Equal(t, 2, exitCode, "fix without target argument must exit with code 2")
	assert.Contains(t, stderr, "missing target finding argument")
}

// Scenario 16: Error on Non-Existent Finding File (REQ-0002)
func TestFixNonExistentFindingFile(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "fix", "/workspace/non_existent.json")

	assert.Equal(t, 2, exitCode, "fix with non-existent finding file must exit with code 2")
	assert.Contains(t, stderr, "finding file not found")
}

// Scenario 17: Strict Subcommand Dispatch for Fix (Reject cm fix Prefix) (REQ-0001)
func TestFixRejectCMFixPrefix(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	findingPath := filepath.Join(ws, "test-finding.json")
	require.NoError(t, os.WriteFile(findingPath, []byte(sampleFindingJSON), 0666))

	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "cm", "fix", "/workspace/test-finding.json")

	assert.Equal(t, 2, exitCode, "invocation with 'cm fix' must exit with code 2")
	assert.Contains(t, stderr, "unrecognized subcommand 'cm'")
}

// Scenario 18: Fix Subcommand Help Flag (REQ-0002)
func TestFixHelpFlag(t *testing.T) {
	image := getImageName()
	stdout, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", image, "fix", "--help")

	assert.Equal(t, 0, exitCode, "fix --help must exit with code 0")
	assert.Contains(t, stdout+stderr, "cm-runner fix")
}

// Scenario 19: Host Workspace Immutability Validation (REQ-0007, ADR-0003)
func TestHostWorkspaceImmutability(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	findingPath := filepath.Join(ws, "test-finding.json")
	require.NoError(t, os.WriteFile(findingPath, []byte(sampleFindingJSON), 0666))

	targetFile := filepath.Join(ws, "src", "auth", "auth.go")
	initialChecksum := calculateSHA256(t, targetFile)

	// Execute fix against workspace
	runDocker(t, 15*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "fix", "/workspace/test-finding.json")

	finalChecksum := calculateSHA256(t, targetFile)
	assert.Equal(t, initialChecksum, finalChecksum, "host workspace files must remain 100%% byte-for-byte identical after fix execution")
}
