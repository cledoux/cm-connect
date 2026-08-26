//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// reviewCommentCall records parameters passed to pulls.createReviewComment (POST /repos/{owner}/{repo}/pulls/{number}/comments).
type reviewCommentCall struct {
	CommitID  string  `json:"commit_id"`
	Path      string  `json:"path"`
	StartLine *int    `json:"start_line"`
	Line      int     `json:"line"`
	StartSide *string `json:"start_side"`
	Side      string  `json:"side"`
	Body      string  `json:"body"`
}

// issueCommentCall records parameters passed to issues.createComment (POST /repos/{owner}/{repo}/issues/{number}/comments).
type issueCommentCall struct {
	Body string `json:"body"`
}

// mockExecutionResult captures all mock API calls and outputs from running publish_comments.py.
type mockExecutionResult struct {
	ReviewComments []reviewCommentCall
	IssueComments  []issueCommentCall
	Stdout         string
	Stderr         string
	ExitCode       int
	StepSummary    string
}

// getPublishScriptPath returns the expected location of publish_comments.py.
func getPublishScriptPath(t *testing.T) string {
	t.Helper()
	repoRoot := getRepoRoot(t)
	return filepath.Join(repoRoot, "github-actions", "scripts", "publish_comments.py")
}

// runPublishScript runs publish_comments.py against a hermetic Go httptest mock GitHub API server.
func runPublishScript(t *testing.T, envelopePath string, simulate422 bool) mockExecutionResult {
	t.Helper()
	scriptPath := getPublishScriptPath(t)

	// If script doesn't exist, fail immediately with clear TDD message
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("Target script does not exist at %s: %v (TDD expectation: implement github-actions/scripts/publish_comments.py)", scriptPath, err)
	}

	tempDir := t.TempDir()
	summaryFile := filepath.Join(tempDir, "step_summary.md")

	var mu sync.Mutex
	var reviewCalls []reviewCommentCall
	var issueCalls []issueCommentCall

	// Create hermetic Go HTTP mock server for GitHub REST API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if strings.Contains(r.URL.Path, "/pulls/100/comments") {
			if simulate422 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"message": "Validation Failed", "errors": ["line is not part of diff"]}`))
				return
			}

			var call reviewCommentCall
			if err := json.Unmarshal(bodyBytes, &call); err != nil {
				http.Error(w, "invalid json body", http.StatusBadRequest)
				return
			}
			reviewCalls = append(reviewCalls, call)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 1, "status": "created"}`))
			return
		}

		if strings.Contains(r.URL.Path, "/issues/100/comments") {
			var call issueCommentCall
			if err := json.Unmarshal(bodyBytes, &call); err != nil {
				http.Error(w, "invalid json body", http.StatusBadRequest)
				return
			}
			issueCalls = append(issueCalls, call)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 2, "status": "created"}`))
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	// Execute python3 publish_comments.py with mock environment variables
	env := os.Environ()
	env = append(env,
		"GITHUB_API_URL="+server.URL,
		"GITHUB_REPOSITORY=octocat/hello-world",
		"PR_NUMBER=100",
		"COMMIT_SHA=abc1234567890abcdef",
		"GITHUB_TOKEN=mock-gh-token",
		"GITHUB_STEP_SUMMARY="+summaryFile,
		"ENVELOPE_PATH="+envelopePath,
	)

	stdout, stderr, exitCode := runCommandWithEnv(t, 5*time.Second, "", nil, env, "python3", scriptPath, envelopePath)

	var summaryContent string
	if data, err := os.ReadFile(summaryFile); err == nil {
		summaryContent = string(data)
	}

	return mockExecutionResult{
		ReviewComments: reviewCalls,
		IssueComments:  issueCalls,
		Stdout:         stdout,
		Stderr:         stderr,
		ExitCode:       exitCode,
		StepSummary:    summaryContent,
	}
}

// runCommandWithEnv executes a command with custom environment variables and a strict timeout.
func runCommandWithEnv(t *testing.T, timeout time.Duration, dir string, stdin io.Reader, env []string, name string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if stdin != nil {
		cmd.Stdin = stdin
	}
	if len(env) > 0 {
		cmd.Env = env
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if err == nil {
		return stdout, stderr, 0
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Logf("command timed out after %v: %s %s", timeout, name, strings.Join(args, " "))
		return stdout, stderr, 124
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout, stderr, exitErr.ExitCode()
	}

	t.Fatalf("failed to execute %s %s: %v", name, strings.Join(args, " "), err)
	return stdout, stderr, -1
}

// TestWorkflow_PublishComments executes the complete verification suite for PR review comments & fallback publisher.
// Governing: ADR-0004, ADR-0005, SPEC-workflow/cm-pr-workflow, REQ-0006, REQ-0007, REQ-TEST.2
func TestWorkflow_PublishComments(t *testing.T) {
	t.Run("SingleLineSuggestion", testWorkflowPublishSingleLineSuggestion)
	t.Run("MultilineSuggestion", testWorkflowPublishMultilineSuggestion)
	t.Run("FallbackOnHTTP422", testWorkflowPublishFallbackOnHTTP422)
	t.Run("UnresolvedFinding", testWorkflowPublishUnresolvedFinding)
	t.Run("StepSummaryGeneration", testWorkflowPublishStepSummary)
	t.Run("DualExecutionMode", testWorkflowPublishDualExecutionMode)
	t.Run("AdvisoryFindings", testWorkflowPublishAdvisoryFindings)
}

// Scenario 1: Publish single-line review suggestion comment (REQ-0006, REQ-TEST.2)
// GIVEN a ChangeEnvelope with a single-line hunk replacing line 42 of pkg/auth/store.go
// WHEN publish_comments.py processes the envelope
// THEN it MUST invoke createReviewComment targeting path="pkg/auth/store.go", line=42, side="RIGHT"
// omitting start_line and start_side, with a ```suggestion block containing the replacement.
func testWorkflowPublishSingleLineSuggestion(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	envelopePath := filepath.Join(dir, "change_envelope_single_line.json")

	result := runPublishScript(t, envelopePath, false)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(result.ReviewComments) != 1 {
		t.Fatalf("expected exactly 1 review comment, got %d", len(result.ReviewComments))
	}
	if len(result.IssueComments) != 0 {
		t.Fatalf("expected 0 fallback issue comments, got %d", len(result.IssueComments))
	}

	comment := result.ReviewComments[0]
	if comment.Path != "pkg/auth/store.go" {
		t.Errorf("comment.Path = %q, want 'pkg/auth/store.go'", comment.Path)
	}
	if comment.Line != 42 {
		t.Errorf("comment.Line = %d, want 42", comment.Line)
	}
	if comment.Side != "RIGHT" {
		t.Errorf("comment.Side = %q, want 'RIGHT'", comment.Side)
	}
	if comment.StartLine != nil {
		t.Errorf("comment.StartLine = %v, want nil/undefined for single-line hunk", *comment.StartLine)
	}
	if comment.StartSide != nil {
		t.Errorf("comment.StartSide = %v, want nil/undefined for single-line hunk", *comment.StartSide)
	}

	// Verify suggestion markdown body formatting per REQ-0006
	if !strings.Contains(comment.Body, "### 🛡️ CodeMender Auto-Fix: SQL Injection in User Lookup") {
		t.Errorf("comment.Body missing expected header title, got:\n%s", comment.Body)
	}
	if !strings.Contains(comment.Body, "CWE-89") {
		t.Errorf("comment.Body missing vulnerability type CWE-89, got:\n%s", comment.Body)
	}
	if !strings.Contains(comment.Body, "```suggestion") {
		t.Errorf("comment.Body missing ```suggestion markdown block, got:\n%s", comment.Body)
	}
	if !strings.Contains(comment.Body, "query := \"SELECT id, name, role FROM users WHERE id = $1\"") {
		t.Errorf("comment.Body missing replacement code, got:\n%s", comment.Body)
	}
}

// Scenario 2: Publish multi-line review suggestion comment (REQ-0006, REQ-TEST.2)
// GIVEN a ChangeEnvelope with a multi-line hunk replacing lines 42-43 of pkg/auth/store.go
// WHEN publish_comments.py processes the envelope
// THEN it MUST invoke createReviewComment targeting path="pkg/auth/store.go", start_line=42, line=43,
// start_side="RIGHT", side="RIGHT", with a ```suggestion block containing the replacement.
func testWorkflowPublishMultilineSuggestion(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	envelopePath := filepath.Join(dir, "change_envelope_multiline.json")

	result := runPublishScript(t, envelopePath, false)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(result.ReviewComments) != 1 {
		t.Fatalf("expected exactly 1 review comment, got %d", len(result.ReviewComments))
	}
	if len(result.IssueComments) != 0 {
		t.Fatalf("expected 0 fallback issue comments, got %d", len(result.IssueComments))
	}

	comment := result.ReviewComments[0]
	if comment.Path != "pkg/auth/store.go" {
		t.Errorf("comment.Path = %q, want 'pkg/auth/store.go'", comment.Path)
	}
	if comment.StartLine == nil || *comment.StartLine != 42 {
		t.Errorf("comment.StartLine = %v, want 42", comment.StartLine)
	}
	if comment.Line != 43 {
		t.Errorf("comment.Line = %d, want 43", comment.Line)
	}
	if comment.StartSide == nil || *comment.StartSide != "RIGHT" {
		t.Errorf("comment.StartSide = %v, want 'RIGHT'", comment.StartSide)
	}
	if comment.Side != "RIGHT" {
		t.Errorf("comment.Side = %q, want 'RIGHT'", comment.Side)
	}

	// Verify suggestion markdown body formatting per REQ-0006
	if !strings.Contains(comment.Body, "```suggestion") {
		t.Errorf("comment.Body missing ```suggestion markdown block, got:\n%s", comment.Body)
	}
	if !strings.Contains(comment.Body, "query := \"SELECT id, name, role FROM users WHERE id = $1\"") {
		t.Errorf("comment.Body missing replacement line 1, got:\n%s", comment.Body)
	}
	if !strings.Contains(comment.Body, "row := db.QueryRowContext(ctx, query, id)") {
		t.Errorf("comment.Body missing replacement line 2, got:\n%s", comment.Body)
	}
}

// Scenario 3: Handle HTTP 422 error and fall back to top-level issue comment (REQ-0007, REQ-TEST.2)
// GIVEN a ChangeEnvelope where createReviewComment rejects with HTTP 422 Unprocessable Entity
// WHEN publish_comments.py catches the 422 error
// THEN it MUST NOT fail the step and MUST invoke issues.createComment with issue_number and patch in ```diff block.
func testWorkflowPublishFallbackOnHTTP422(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	envelopePath := filepath.Join(dir, "change_envelope_single_line.json")

	result := runPublishScript(t, envelopePath, true)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0 (graceful fallback), got %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(result.ReviewComments) != 0 {
		t.Fatalf("expected 0 successful review comments due to 422 rejection, got %d", len(result.ReviewComments))
	}
	if len(result.IssueComments) != 1 {
		t.Fatalf("expected exactly 1 fallback issue comment, got %d", len(result.IssueComments))
	}

	fallback := result.IssueComments[0]
	if !strings.Contains(fallback.Body, "SQL Injection in User Lookup") {
		t.Errorf("fallback.Body missing finding title, got:\n%s", fallback.Body)
	}
	if !strings.Contains(fallback.Body, "CWE-89") {
		t.Errorf("fallback.Body missing vulnerability CWE-89, got:\n%s", fallback.Body)
	}
	if !strings.Contains(fallback.Body, "```diff") {
		t.Errorf("fallback.Body missing ```diff markdown block, got:\n%s", fallback.Body)
	}
	if !strings.Contains(fallback.Body, "diff --git a/pkg/auth/store.go b/pkg/auth/store.go") {
		t.Errorf("fallback.Body missing diff patch content, got:\n%s", fallback.Body)
	}
}

// Scenario 4: Handle unresolved finding without posting review comments (REQ-0005, REQ-0006, REQ-TEST.2)
// GIVEN a ChangeEnvelope with status: "UNRESOLVED" and empty hunks
// WHEN publish_comments.py processes the unresolved envelope
// THEN it MUST NOT invoke createReviewComment or createComment, and MUST record unresolved status in summary.
func testWorkflowPublishUnresolvedFinding(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	envelopePath := filepath.Join(dir, "change_envelope_unresolved.json")

	result := runPublishScript(t, envelopePath, false)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0 for unresolved finding, got %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(result.ReviewComments) != 0 {
		t.Fatalf("expected 0 review comments for unresolved finding, got %d", len(result.ReviewComments))
	}
	if len(result.IssueComments) != 0 {
		t.Fatalf("expected 0 issue comments for unresolved finding, got %d", len(result.IssueComments))
	}

	if !strings.Contains(result.StepSummary, "UNRESOLVED") {
		t.Errorf("step summary missing UNRESOLVED indicator, got:\n%s", result.StepSummary)
	}
	if !strings.Contains(result.StepSummary, "Hardcoded API Key") {
		t.Errorf("step summary missing finding title 'Hardcoded API Key', got:\n%s", result.StepSummary)
	}
}

// Scenario 5: Generate GitHub Actions step summary (REQ-0006, REQ-TEST.2)
// GIVEN an execution with GITHUB_STEP_SUMMARY configured
// WHEN publish_comments.py finishes processing the change envelope
// THEN $GITHUB_STEP_SUMMARY MUST contain a markdown summary table or card detailing finding status, severity, title, and modified files.
func testWorkflowPublishStepSummary(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	envelopePath := filepath.Join(dir, "change_envelope_single_line.json")

	result := runPublishScript(t, envelopePath, false)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(strings.TrimSpace(result.StepSummary)) == 0 {
		t.Fatalf("expected non-empty GITHUB_STEP_SUMMARY")
	}

	if !strings.Contains(result.StepSummary, "SQL Injection in User Lookup") {
		t.Errorf("step summary missing finding title, got:\n%s", result.StepSummary)
	}
	if !strings.Contains(result.StepSummary, "FIXED") {
		t.Errorf("step summary missing status FIXED, got:\n%s", result.StepSummary)
	}
	if !strings.Contains(result.StepSummary, "pkg/auth/store.go") {
		t.Errorf("step summary missing modified file pkg/auth/store.go, got:\n%s", result.StepSummary)
	}
}

// Scenario 6: Standalone CLI invocation vs module import (REQ-TEST.2)
// GIVEN publish_comments.py
// WHEN invoked directly via python3
// THEN it executes with exit code 0 and valid JSON output without external pip packages.
func testWorkflowPublishDualExecutionMode(t *testing.T) {
	dir := getWorkflowFixturesDir(t)
	envelopePath := filepath.Join(dir, "change_envelope_single_line.json")

	result := runPublishScript(t, envelopePath, false)
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var jsonOutput map[string]interface{}
	if err := json.Unmarshal([]byte(result.Stdout), &jsonOutput); err != nil {
		t.Errorf("expected valid JSON output from CLI execution, got error: %v, stdout: %s", err, result.Stdout)
	}
	if jsonOutput["status"] != "FIXED" {
		t.Errorf("expected status FIXED, got %v", jsonOutput["status"])
	}

	scriptPath := getPublishScriptPath(t)
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read script %s: %v", scriptPath, err)
	}

	scriptStr := string(content)
	if !strings.Contains(scriptStr, "#!/usr/bin/env python3") {
		t.Errorf("publish_comments.py missing python3 shebang")
	}
	if !strings.Contains(scriptStr, "def publish_comments(") {
		t.Errorf("publish_comments.py missing publish_comments entrypoint function")
	}
}

// Scenario 7: Publish advisory non-blocking comments for preexisting findings (REQ-0004, REQ-0007)
func testWorkflowPublishAdvisoryFindings(t *testing.T) {
	scriptPath := getPublishScriptPath(t)
	tempDir := t.TempDir()
	summaryFile := filepath.Join(tempDir, "step_summary.md")

	preexistingFile := filepath.Join(tempDir, "preexisting.json")
	preexistingContent := `[
		{
			"finding_id": "123a4567-c05a-5258-99ac-bb9e932374c9",
			"file_path": "legacy/db.go",
			"start_line": 120,
			"severity": "HIGH",
			"title": "Hardcoded Database Password",
			"payload": {
				"FilePath": "legacy/db.go",
				"StartLine": 120,
				"Title": "Hardcoded Database Password",
				"Analysis": "Hardcoded database password found in untouched legacy database helper file.",
				"Severity": "HIGH",
				"VulnType": "HARDCODED_CREDENTIALS"
			}
		}
	]`
	if err := os.WriteFile(preexistingFile, []byte(preexistingContent), 0644); err != nil {
		t.Fatalf("failed to write preexisting fixture: %v", err)
	}

	var mu sync.Mutex
	var issueCalls []issueCommentCall

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if strings.Contains(r.URL.Path, "/issues/100/comments") {
			var call issueCommentCall
			if err := json.Unmarshal(bodyBytes, &call); err != nil {
				http.Error(w, "invalid json body", http.StatusBadRequest)
				return
			}
			issueCalls = append(issueCalls, call)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 2, "status": "created"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", scriptPath, "--advisory", preexistingFile)
	cmd.Env = append(os.Environ(),
		"GITHUB_TOKEN=test-token",
		"GITHUB_REPOSITORY=test-owner/test-repo",
		"PR_NUMBER=100",
		"GITHUB_API_URL="+server.URL,
		"GITHUB_STEP_SUMMARY="+summaryFile,
	)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	if err != nil {
		t.Fatalf("script execution failed: %v, stderr: %s", err, stderrBuf.String())
	}

	mu.Lock()
	defer mu.Unlock()

	if len(issueCalls) != 1 {
		t.Fatalf("expected 1 issue comment call, got %d", len(issueCalls))
	}
	if !strings.Contains(issueCalls[0].Body, "Potentially Preexisting Security Finding") {
		t.Errorf("issue comment body missing Potentially Preexisting headline: %s", issueCalls[0].Body)
	}
	if !strings.Contains(issueCalls[0].Body, "Non-Blocking") {
		t.Errorf("issue comment body missing Non-Blocking label: %s", issueCalls[0].Body)
	}

	summaryBytes, err := os.ReadFile(summaryFile)
	if err != nil {
		t.Fatalf("failed to read step summary: %v", err)
	}
	summaryStr := string(summaryBytes)
	if !strings.Contains(summaryStr, "Potentially Preexisting Security Finding") {
		t.Errorf("summary missing Potentially Preexisting headline: %s", summaryStr)
	}
}
