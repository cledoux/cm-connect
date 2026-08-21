//go:build integration

package integration_test

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// getWorkflowReadmePath returns the absolute path to github-actions/README.md.
func getWorkflowReadmePath(t *testing.T) string {
	t.Helper()
	repoRoot := getRepoRoot(t)
	return filepath.Join(repoRoot, "github-actions", "README.md")
}

// loadWorkflowReadme reads the content of github-actions/README.md or fails the test if missing.
func loadWorkflowReadme(t *testing.T) (string, []string) {
	t.Helper()
	readmePath := getWorkflowReadmePath(t)
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", readmePath, err)
	}

	content := string(data)
	if strings.TrimSpace(content) == "" {
		t.Fatalf("README.md at %s is empty", readmePath)
	}

	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return content, lines
}

// TestWorkflowREADME verifies that github-actions/README.md satisfies all acceptance criteria (REQ-0008, REQ-TEST.6).
func TestWorkflowREADME(t *testing.T) {
	// Subtest 1: Existence and basic readability (REQ-0008)
	t.Run("FileExistsAndReadable", func(t *testing.T) {
		readmePath := getWorkflowReadmePath(t)
		info, err := os.Stat(readmePath)
		if err != nil {
			t.Fatalf("github-actions/README.md does not exist at %s: %v", readmePath, err)
		}
		if info.IsDir() {
			t.Fatalf("github-actions/README.md is a directory, want file")
		}
		if info.Size() == 0 {
			t.Fatalf("github-actions/README.md is empty")
		}
	})

	// Subtest 2: Two-stage review pipeline explanation (REQ-0008.14)
	t.Run("TwoStageReviewPipeline", func(t *testing.T) {
		content, _ := loadWorkflowReadme(t)

		requiredPhrases := []string{
			"scan",
			"dynamic matrix",
			"fix",
			"review comment",
		}

		for _, phrase := range requiredPhrases {
			if !strings.Contains(strings.ToLower(content), strings.ToLower(phrase)) {
				t.Errorf("README missing expected pipeline stage/concept: %q", phrase)
			}
		}
	})

	// Subtest 3: Required GitHub secrets and IAM permissions (REQ-0008.15)
	t.Run("SecretsAndPermissions", func(t *testing.T) {
		content, _ := loadWorkflowReadme(t)

		requiredSecrets := []string{
			"GCP_WIF_PROVIDER",
			"GCP_SERVICE_ACCOUNT",
		}
		for _, secret := range requiredSecrets {
			if !strings.Contains(content, secret) {
				t.Errorf("README missing required repository secret: %q", secret)
			}
		}

		requiredPermissions := []string{
			"id-token: write",
			"pull-requests: write",
			"contents: read",
		}
		for _, perm := range requiredPermissions {
			if !strings.Contains(content, perm) {
				t.Errorf("README missing required workflow permission: %q", perm)
			}
		}

		requiredIAMRoles := []string{
			"roles/aiplatform.user",
			"roles/iam.workloadIdentityUser",
		}
		for _, role := range requiredIAMRoles {
			if !strings.Contains(content, role) {
				t.Errorf("README missing required GCP IAM role: %q", role)
			}
		}
	})

	// Subtest 4: Step-by-step instructions for install.sh and setup-wif.sh (REQ-0008.16)
	t.Run("ScriptInstructions", func(t *testing.T) {
		content, _ := loadWorkflowReadme(t)

		if !strings.Contains(content, "install.sh") {
			t.Errorf("README missing install.sh instructions")
		}
		if !strings.Contains(content, "setup-wif.sh") {
			t.Errorf("README missing setup-wif.sh instructions")
		}
	})

	// Subtest 5: Diff scoping, 1-click apply review suggestions, and fallback mechanisms (REQ-0008.17)
	t.Run("DiffScopingAndReviewSuggestions", func(t *testing.T) {
		content, _ := loadWorkflowReadme(t)

		if !strings.Contains(content, "commit.diff") {
			t.Errorf("README missing commit.diff diff-scoping explanation")
		}
		if !strings.Contains(content, "```suggestion") {
			t.Errorf("README missing 1-click apply suggestion example (```suggestion)")
		}
		if !strings.Contains(content, "422") {
			t.Errorf("README missing HTTP 422 out-of-diff error fallback explanation")
		}
		if !strings.Contains(content, "GITHUB_STEP_SUMMARY") && !strings.Contains(content, "issues") {
			t.Errorf("README missing fallback target description (issue comments or step summary)")
		}
	})

	// Subtest 6: Mermaid diagrams for WIF sequence and PR review workflow DAG (REQ-0008)
	t.Run("MermaidDiagrams", func(t *testing.T) {
		content, _ := loadWorkflowReadme(t)

		mermaidBlockRegex := regexp.MustCompile("(?s)```mermaid\\s*\\n(.*?)```")
		matches := mermaidBlockRegex.FindAllStringSubmatch(content, -1)

		if len(matches) < 2 {
			t.Fatalf("README must contain at least 2 mermaid diagrams, found %d", len(matches))
		}

		hasWorkflowDAG := false
		hasWIFSequence := false

		for _, m := range matches {
			diag := strings.TrimSpace(m[1])
			if strings.HasPrefix(diag, "flowchart") || strings.HasPrefix(diag, "graph") {
				hasWorkflowDAG = true
			}
			if strings.HasPrefix(diag, "sequenceDiagram") {
				hasWIFSequence = true
			}
		}

		if !hasWorkflowDAG {
			t.Errorf("README missing PR review workflow DAG mermaid diagram (flowchart / graph)")
		}
		if !hasWIFSequence {
			t.Errorf("README missing WIF authentication sequence mermaid diagram (sequenceDiagram)")
		}
	})

	// Subtest 7: Link integrity validation (REQ-TEST.6)
	t.Run("LinkIntegrity", func(t *testing.T) {
		content, _ := loadWorkflowReadme(t)
		repoRoot := getRepoRoot(t)
		readmeDir := filepath.Join(repoRoot, "github-actions")

		// Match markdown links [text](target)
		linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
		matches := linkRegex.FindAllStringSubmatch(content, -1)

		for _, m := range matches {
			linkTarget := m[2]
			// Skip external links, mailto, and internal page anchors
			if strings.HasPrefix(linkTarget, "http://") ||
				strings.HasPrefix(linkTarget, "https://") ||
				strings.HasPrefix(linkTarget, "mailto:") ||
				strings.HasPrefix(linkTarget, "#") {
				continue
			}

			// Strip query params or hash fragments
			cleanPath := strings.Split(strings.Split(linkTarget, "#")[0], "?")[0]
			if cleanPath == "" {
				continue
			}

			targetAbsPath := filepath.Join(readmeDir, cleanPath)
			if _, err := os.Stat(targetAbsPath); err != nil {
				// Also check if relative to repo root
				targetRepoPath := filepath.Join(repoRoot, cleanPath)
				if _, err2 := os.Stat(targetRepoPath); err2 != nil {
					t.Errorf("broken relative link in README: %s -> %s (checked %s and %s)", m[0], linkTarget, targetAbsPath, targetRepoPath)
				}
			}
		}
	})

	// Subtest 8: mdformat compliance and 80-character line wrapping (REQ-0008.18)
	t.Run("MdformatCompliance", func(t *testing.T) {
		readmePath := getWorkflowReadmePath(t)

		// Check mdformat CLI compliance
		if mdformatPath, err := exec.LookPath("mdformat"); err == nil {
			cmd := exec.Command(mdformatPath, "--check", "--wrap", "80", readmePath)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("mdformat check failed: %v\nOutput: %s", err, string(output))
			}
		} else {
			t.Logf("mdformat executable not found in PATH; falling back to line length check")
		}

		_, lines := loadWorkflowReadme(t)
		inCodeBlock := false
		for lineNum, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inCodeBlock = !inCodeBlock
				continue
			}

			// In markdown, code blocks, tables, URLs, uninterrupted links, and long code literals cannot be wrapped
			if inCodeBlock ||
				strings.HasPrefix(trimmed, "|") ||
				strings.Contains(line, "http://") ||
				strings.Contains(line, "https://") ||
				strings.Contains(line, "principalSet://") ||
				(strings.Contains(line, "[") && strings.Contains(line, "](")) ||
				strings.Contains(line, "`strategy: matrix:") {
				continue
			}

			if len(line) > 80 {
				t.Errorf("line %d exceeds 80 characters (%d chars): %q", lineNum+1, len(line), line)
			}
		}
	})
}
