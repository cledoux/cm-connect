//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// createTestGitRepo initializes a temporary Git repository with a base commit and a head commit.
// Returns the directory path, base commit SHA, and head commit SHA.
func createTestGitRepo(t *testing.T) (repoDir string, baseSHA string, headSHA string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatalf("failed to chmod test git repo: %v", err)
	}

	// Initialize Git repository
	runCommand(t, 5*time.Second, dir, nil, "git", "init")
	runCommand(t, 5*time.Second, dir, nil, "git", "config", "user.name", "CodeMender Test")
	runCommand(t, 5*time.Second, dir, nil, "git", "config", "user.email", "test@codemender.dev")

	// Base commit (clean)
	baseFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(baseFile, []byte("package main\n\nfunc main() {}\n"), 0666); err != nil {
		t.Fatalf("failed to write base file: %v", err)
	}
	runCommand(t, 5*time.Second, dir, nil, "git", "add", "main.go")
	runCommand(t, 5*time.Second, dir, nil, "git", "commit", "-m", "initial clean base commit")

	baseOut, _, exitBase := runCommand(t, 5*time.Second, dir, nil, "git", "rev-parse", "HEAD")
	if exitBase != 0 {
		t.Fatalf("failed to get base commit SHA")
	}
	baseSHA = strings.TrimSpace(baseOut)

	// Head commit (introducing code changes)
	vulnContent := `package main

import (
	"database/sql"
	"fmt"
)

func queryUser(db *sql.DB, username string) {
	query := fmt.Sprintf("SELECT * FROM users WHERE username = '%s'", username)
	db.Query(query)
}
`
	if err := os.WriteFile(baseFile, []byte(vulnContent), 0666); err != nil {
		t.Fatalf("failed to write modified file: %v", err)
	}
	runCommand(t, 5*time.Second, dir, nil, "git", "add", "main.go")
	runCommand(t, 5*time.Second, dir, nil, "git", "commit", "-m", "introduce changes")

	headOut, _, exitHead := runCommand(t, 5*time.Second, dir, nil, "git", "rev-parse", "HEAD")
	if exitHead != 0 {
		t.Fatalf("failed to get head commit SHA")
	}
	headSHA = strings.TrimSpace(headOut)

	return dir, baseSHA, headSHA
}

// createMockCM creates an executable mock 'cm' script in a temp directory that simulates
// CodeMender scanning and reporting phases.
func createMockCM(t *testing.T, findingsJSON string) string {
	t.Helper()
	dir := t.TempDir()
	mockPath := filepath.Join(dir, "cm")

	script := fmt.Sprintf(`#!/bin/sh
cmd=""
for arg in "$@"; do
    case "$arg" in
        find|report|init|fix)
            cmd="$arg"
            break
            ;;
    esac
done

if [ "$cmd" = "find" ]; then
    echo "cm find scanning diff target on stderr" >&2
    exit 0
elif [ "$cmd" = "report" ]; then
    echo '%s'
    echo "cm report summary log on stderr" >&2
    exit 0
fi
exit 0
`, findingsJSON)

	if err := os.WriteFile(mockPath, []byte(script), 0777); err != nil {
		t.Fatalf("failed to write mock cm script: %v", err)
	}
	return mockPath
}

// TestWorkflowFindDiff_Scenarios executes the end-to-end integration test suite validating
// all 5 normative scenarios specified in ADR-0007 and REQ-0014.
// Governing: ADR-0007, SPEC-cm-batch-runner (REQ-0014), SPEC-cm-pr-workflow (REQ-0003)
func TestWorkflowFindDiff_Scenarios(t *testing.T) {
	image := getImageName()

	// Scenario 1: Execute against commits with known vulnerabilities and assert structured JSON findings (REQ-FINDDIFF-INT.1)
	t.Run("REQ-FINDDIFF-INT.1_VulnerabilitiesDetected_ExitCode1_JSONFindings", func(t *testing.T) {
		ws, baseSHA, headSHA := createTestGitRepo(t)

		mockFindings := `[
  {
    "FindingID": "diff-vuln-sqli-001",
    "Title": "SQL Injection in queryUser",
    "FilePath": "main.go",
    "StartLine": 8,
    "Severity": "HIGH"
  }
]`
		mockCM := createMockCM(t, mockFindings)

		stdout, stderr, exitCode := runDocker(
			t,
			15*time.Second,
			nil,
			"--rm",
			"-v", ws+":/workspace",
			"-v", mockCM+":/usr/local/bin/cm:ro",
			image,
			"find-diff",
			baseSHA,
			headSHA,
		)

		if exitCode != 1 {
			t.Fatalf("find-diff exit code = %d, want 1 (findings detected)\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
		}

		if !strings.Contains(stderr, "cm find scanning diff target on stderr") {
			t.Errorf("expected scanner progress on stderr, got:\n%s", stderr)
		}

		// Verify structured JSON findings on stdout
		var parsed []map[string]interface{}
		if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
			t.Fatalf("failed to parse stdout as JSON array: %v\nstdout was:\n%s", err, stdout)
		}
		if len(parsed) != 1 {
			t.Fatalf("expected 1 finding in JSON array, got %d", len(parsed))
		}
		if parsed[0]["FindingID"] != "diff-vuln-sqli-001" {
			t.Errorf("expected FindingID 'diff-vuln-sqli-001', got %v", parsed[0]["FindingID"])
		}
		if parsed[0]["Title"] != "SQL Injection in queryUser" {
			t.Errorf("expected Title 'SQL Injection in queryUser', got %v", parsed[0]["Title"])
		}
	})

	// Scenario 2: Execute on clean / empty diffs and assert immediate emission of [] on stdout with exit code 0 (REQ-FINDDIFF-INT.2)
	t.Run("REQ-FINDDIFF-INT.2_EmptyDiff_FastPath_ExitCode0_EmptyArray", func(t *testing.T) {
		ws, _, headSHA := createTestGitRepo(t)

		// Passing headSHA headSHA produces 0 bytes diff output from git diff
		stdout, stderr, exitCode := runDocker(
			t,
			10*time.Second,
			nil,
			"--rm",
			"-v", ws+":/workspace",
			image,
			"find-diff",
			headSHA,
			headSHA,
		)

		if exitCode != 0 {
			t.Fatalf("empty diff find-diff exit code = %d, want 0\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
		}

		trimmedOut := strings.TrimSpace(stdout)
		if trimmedOut != "[]" {
			t.Errorf("expected exact '[]' on stdout for empty diff, got: %q", stdout)
		}
	})

	// Scenario 3: Execute with appended user context and verify proper context flag merging (REQ-FINDDIFF-INT.3)
	t.Run("REQ-FINDDIFF-INT.3_UserContextMerging_ContextFlagForwarded", func(t *testing.T) {
		ws, baseSHA, headSHA := createTestGitRepo(t)

		// Create a mock cm that records received arguments to an output file
		recordDir := t.TempDir()
		if err := os.Chmod(recordDir, 0777); err != nil {
			t.Fatalf("failed to chmod record dir: %v", err)
		}
		recordFile := filepath.Join(recordDir, "args.log")
		mockPath := filepath.Join(recordDir, "cm")

		script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> /record/args.log
cmd=""
for arg in "$@"; do
    case "$arg" in
        find|report)
            cmd="$arg"
            break
            ;;
    esac
done
if [ "$cmd" = "find" ]; then
    exit 0
elif [ "$cmd" = "report" ]; then
    echo '[]'
    exit 0
fi
exit 0
`)
		if err := os.WriteFile(mockPath, []byte(script), 0777); err != nil {
			t.Fatalf("failed to write record cm script: %v", err)
		}

		stdout, stderr, exitCode := runDocker(
			t,
			15*time.Second,
			nil,
			"--rm",
			"-v", ws+":/workspace",
			"-v", recordDir+":/record",
			"-v", mockPath+":/usr/local/bin/cm:ro",
			image,
			"find-diff",
			baseSHA,
			headSHA,
			"--",
			"-c",
			"Check specifically for SQL injection and auth flaws",
			"--model=gemini-1.5-pro",
		)

		if exitCode != 0 {
			t.Fatalf("find-diff with user context exit code = %d, want 0\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
		}

		logBytes, err := os.ReadFile(recordFile)
		if err != nil {
			t.Fatalf("failed to read recorded args from %s: %v", recordFile, err)
		}
		recordedArgs := string(logBytes)

		// Verify consolidated --context argument containing base prompt + user prompt
		expectedBaseContext := "The target is a Git unified diff for this repository."
		expectedUserPrompt := "Check specifically for SQL injection and auth flaws"

		if !strings.Contains(recordedArgs, expectedBaseContext) {
			t.Errorf("recorded args %q do not contain base diff context prompt %q", recordedArgs, expectedBaseContext)
		}
		if !strings.Contains(recordedArgs, expectedUserPrompt) {
			t.Errorf("recorded args %q do not contain user context prompt %q", recordedArgs, expectedUserPrompt)
		}
		if !strings.Contains(recordedArgs, "--model=gemini-1.5-pro") {
			t.Errorf("recorded args %q do not contain passthrough flag --model=gemini-1.5-pro", recordedArgs)
		}
	})

	// Scenario 4: Execute against non-existent Git revisions and assert exit code 2 with fetch-depth: 0 diagnostic (REQ-FINDDIFF-INT.4)
	t.Run("REQ-FINDDIFF-INT.4_InvalidGitRevision_ExitCode2_FetchDepthHint", func(t *testing.T) {
		ws, _, _ := createTestGitRepo(t)

		stdout, stderr, exitCode := runDocker(
			t,
			10*time.Second,
			nil,
			"--rm",
			"-v", ws+":/workspace",
			image,
			"find-diff",
			"non-existent-base-sha-12345",
			"non-existent-head-sha-67890",
		)

		if exitCode != 2 {
			t.Fatalf("find-diff with invalid revisions exit code = %d, want 2\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
		}

		if !strings.Contains(stderr, "git diff failed") {
			t.Errorf("stderr %q does not contain 'git diff failed'", stderr)
		}
		if !strings.Contains(stderr, "fetch-depth: 0") {
			t.Errorf("stderr %q does not contain actionable diagnostic 'fetch-depth: 0'", stderr)
		}
	})

	// Scenario 5: Verify workspace immutability (zero untracked or modified files in /workspace) (REQ-FINDDIFF-INT.5)
	t.Run("REQ-FINDDIFF-INT.5_WorkspaceImmutability_ZeroUntrackedFiles", func(t *testing.T) {
		ws, baseSHA, headSHA := createTestGitRepo(t)

		mockFindings := `[]`
		mockCM := createMockCM(t, mockFindings)

		// Record initial status
		statusBefore, _, _ := runCommand(t, 5*time.Second, ws, nil, "git", "status", "--porcelain")
		if strings.TrimSpace(statusBefore) != "" {
			t.Fatalf("test repo has dirty working tree before test: %q", statusBefore)
		}

		// Run find-diff inside container
		stdout, stderr, exitCode := runDocker(
			t,
			15*time.Second,
			nil,
			"--rm",
			"-v", ws+":/workspace",
			"-v", mockCM+":/usr/local/bin/cm:ro",
			image,
			"find-diff",
			baseSHA,
			headSHA,
		)

		if exitCode != 0 {
			t.Fatalf("find-diff execution failed with exit code %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
		}

		// Verify working tree remains completely clean and immutable
		statusAfter, _, _ := runCommand(t, 5*time.Second, ws, nil, "git", "status", "--porcelain")
		if strings.TrimSpace(statusAfter) != "" {
			t.Errorf("workspace immutability violated! git status --porcelain was not empty after find-diff:\n%s", statusAfter)
		}

		// Verify /tmp/cm-diff.diff was NOT left in workspace
		wsDiffPath := filepath.Join(ws, "cm-diff.diff")
		if _, err := os.Stat(wsDiffPath); err == nil {
			t.Errorf("found staged diff file in workspace directory: %s", wsDiffPath)
		}
	})
}
