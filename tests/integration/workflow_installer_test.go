//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestInstallerScriptPermissions verifies that github-actions/scripts/install.sh exists
// and has executable permissions (REQ-0008.6).
func TestInstallerScriptPermissions(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := filepath.Join(repoRoot, "github-actions", "scripts", "install.sh")

	info, err := os.Stat(installScript)
	if err != nil {
		t.Fatalf("installer script not found at %s: %v", installScript, err)
	}

	mode := info.Mode()
	if mode.Perm()&0111 == 0 {
		t.Errorf("installer script %s is not executable (mode: %v)", installScript, mode)
	}
}

// TestInstallerScriptSuccess verifies that install.sh copies the workflow template and
// companion scripts into isolated target repositories, creates missing directories,
// preserves checksums, and emits next-step instructions (REQ-0008.1, REQ-0008.3, REQ-0008.4, REQ-0008.5, REQ-TEST.4).
func TestInstallerScriptSuccess(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := filepath.Join(repoRoot, "github-actions", "scripts", "install.sh")
	targetDir := t.TempDir()

	stdout, stderr, exitCode := runCommand(t, 10*time.Second, repoRoot, nil, installScript, targetDir)
	if exitCode != 0 {
		t.Fatalf("installer script failed with exit code %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	// Verify required target directories exist (REQ-0008.3)
	workflowsDir := filepath.Join(targetDir, ".github", "workflows")
	if info, err := os.Stat(workflowsDir); err != nil || !info.IsDir() {
		t.Errorf("expected directory %s to exist, err: %v", workflowsDir, err)
	}

	scriptsDir := filepath.Join(targetDir, ".github", "scripts")
	if info, err := os.Stat(scriptsDir); err != nil || !info.IsDir() {
		t.Errorf("expected directory %s to exist, err: %v", scriptsDir, err)
	}

	// Verify copied files exist, are non-empty, and match source checksums (REQ-0008.4)
	expectedFiles := []struct {
		sourceRel string
		targetRel string
	}{
		{
			sourceRel: filepath.Join("github-actions", "workflows", "codemender.yml"),
			targetRel: filepath.Join(".github", "workflows", "codemender.yml"),
		},
		{
			sourceRel: filepath.Join("github-actions", "scripts", "filter_findings.jq"),
			targetRel: filepath.Join(".github", "scripts", "filter_findings.jq"),
		},
		{
			sourceRel: filepath.Join("github-actions", "scripts", "publish_comments.py"),
			targetRel: filepath.Join(".github", "scripts", "publish_comments.py"),
		},
	}

	for _, ef := range expectedFiles {
		srcPath := filepath.Join(repoRoot, ef.sourceRel)
		dstPath := filepath.Join(targetDir, ef.targetRel)

		srcChecksum := calculateSHA256(t, srcPath)
		dstChecksum := calculateSHA256(t, dstPath)

		if srcChecksum != dstChecksum {
			t.Errorf("checksum mismatch for %s: source=%s, installed=%s", ef.targetRel, srcChecksum, dstChecksum)
		}

		dstContent, err := os.ReadFile(dstPath)
		if err != nil {
			t.Errorf("failed to read installed file %s: %v", dstPath, err)
		} else if len(dstContent) == 0 {
			t.Errorf("installed file %s is empty", dstPath)
		}
	}

	// Verify next-step instructions in stdout (REQ-0008.5)
	if !strings.Contains(stdout, "setup-wif.sh") {
		t.Errorf("expected stdout to mention setup-wif.sh instructions, got: %s", stdout)
	}
	if !strings.Contains(stdout, "GCP_WIF_PROVIDER") && !strings.Contains(stdout, "secrets") {
		t.Errorf("expected stdout to mention repository secrets instructions, got: %s", stdout)
	}
}

// TestInstallerScriptMissingArgument verifies that running install.sh without arguments
// emits usage to stderr and exits with status code 1 (REQ-0008.2).
func TestInstallerScriptMissingArgument(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := filepath.Join(repoRoot, "github-actions", "scripts", "install.sh")

	stdout, stderr, exitCode := runCommand(t, 5*time.Second, repoRoot, nil, installScript)
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1 for missing argument\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	combinedOutput := stdout + stderr
	if !strings.Contains(strings.ToLower(combinedOutput), "usage") {
		t.Errorf("expected usage message in output when missing argument, got stdout=%q, stderr=%q", stdout, stderr)
	}
}

// TestInstallerScriptNonExistentDirectory verifies that running install.sh with a non-existent
// target path fails with exit code 1 and prints usage/error to stderr (REQ-0008.2).
func TestInstallerScriptNonExistentDirectory(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := filepath.Join(repoRoot, "github-actions", "scripts", "install.sh")
	nonExistentPath := filepath.Join(t.TempDir(), "non-existent-directory-12345")

	stdout, stderr, exitCode := runCommand(t, 5*time.Second, repoRoot, nil, installScript, nonExistentPath)
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1 for non-existent target\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	combinedOutput := stdout + stderr
	if !strings.Contains(strings.ToLower(combinedOutput), "usage") && !strings.Contains(strings.ToLower(combinedOutput), "error") && !strings.Contains(strings.ToLower(combinedOutput), "not found") {
		t.Errorf("expected usage or error message for non-existent target, got stdout=%q, stderr=%q", stdout, stderr)
	}
}

// TestInstallerScriptTargetIsFile verifies that running install.sh with a regular file
// as target fails with exit code 1 (REQ-0008.2).
func TestInstallerScriptTargetIsFile(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := filepath.Join(repoRoot, "github-actions", "scripts", "install.sh")
	tempFile := filepath.Join(t.TempDir(), "target-is-a-file.txt")
	if err := os.WriteFile(tempFile, []byte("regular file content"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	stdout, stderr, exitCode := runCommand(t, 5*time.Second, repoRoot, nil, installScript, tempFile)
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1 when target is a file\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	combinedOutput := stdout + stderr
	if !strings.Contains(strings.ToLower(combinedOutput), "usage") && !strings.Contains(strings.ToLower(combinedOutput), "directory") {
		t.Errorf("expected usage/directory error message when target is a file, got stdout=%q, stderr=%q", stdout, stderr)
	}
}

// TestInstallerScriptOverwritesExisting verifies that install.sh is idempotent and
// cleanly updates an existing target repository that already has .github directories (REQ-0008.3, REQ-0008.4).
func TestInstallerScriptOverwritesExisting(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := filepath.Join(repoRoot, "github-actions", "scripts", "install.sh")
	targetDir := t.TempDir()

	// Pre-populate target directory with dummy files
	dummyWorkflowsDir := filepath.Join(targetDir, ".github", "workflows")
	dummyScriptsDir := filepath.Join(targetDir, ".github", "scripts")
	if err := os.MkdirAll(dummyWorkflowsDir, 0755); err != nil {
		t.Fatalf("failed to create dummy workflows dir: %v", err)
	}
	if err := os.MkdirAll(dummyScriptsDir, 0755); err != nil {
		t.Fatalf("failed to create dummy scripts dir: %v", err)
	}

	staleFile := filepath.Join(dummyWorkflowsDir, "codemender.yml")
	if err := os.WriteFile(staleFile, []byte("stale workflow content"), 0644); err != nil {
		t.Fatalf("failed to create stale file: %v", err)
	}

	stdout, stderr, exitCode := runCommand(t, 10*time.Second, repoRoot, nil, installScript, targetDir)
	if exitCode != 0 {
		t.Fatalf("installer script failed on existing target directory with exit code %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	// Verify the file was overwritten with true content
	srcWorkflow := filepath.Join(repoRoot, "github-actions", "workflows", "codemender.yml")
	srcChecksum := calculateSHA256(t, srcWorkflow)
	dstChecksum := calculateSHA256(t, staleFile)

	if srcChecksum != dstChecksum {
		t.Errorf("expected installed workflow to match source checksum after overwrite, got %s != %s", dstChecksum, srcChecksum)
	}
}
