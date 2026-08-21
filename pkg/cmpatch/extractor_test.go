package cmpatch

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSynthesizeEnvelope_Fixed(t *testing.T) {
	t.Parallel()

	diff := `diff --git a/pkg/auth/store.go b/pkg/auth/store.go
index 4b825dc..a3f12bc 100644
--- a/pkg/auth/store.go
+++ b/pkg/auth/store.go
@@ -42,3 +42,3 @@
-    query := fmt.Sprintf("SELECT * FROM users WHERE id = '%s'", id)
+    query := "SELECT * FROM users WHERE id = $1"
-    row := db.QueryRow(query)
+    row := db.QueryRowContext(ctx, query, id)
`

	env, err := SynthesizeEnvelope("finding-123", "CWE-89", "SQLi", "Use parameterized query", diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env == nil {
		t.Fatal("expected non-nil ChangeEnvelope")
	}

	if env.FindingID != "finding-123" {
		t.Errorf("expected finding_id 'finding-123', got %s", env.FindingID)
	}
	if env.Status != "FIXED" {
		t.Errorf("expected status 'FIXED', got %s", env.Status)
	}
	if env.VulnType != "CWE-89" {
		t.Errorf("expected vuln_type 'CWE-89', got %s", env.VulnType)
	}
	if env.Title != "SQLi" {
		t.Errorf("expected title 'SQLi', got %s", env.Title)
	}
	if env.Summary != "Use parameterized query" {
		t.Errorf("expected summary 'Use parameterized query', got %s", env.Summary)
	}
	if len(env.FilesModified) != 1 || env.FilesModified[0] != "pkg/auth/store.go" {
		t.Errorf("unexpected files_modified: %v", env.FilesModified)
	}
	if env.Patch != diff {
		t.Errorf("patch does not match original diff")
	}
	if len(env.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(env.Hunks))
	}
	hunk := env.Hunks[0]
	if hunk.FilePath != "pkg/auth/store.go" {
		t.Errorf("expected hunk file_path 'pkg/auth/store.go', got %s", hunk.FilePath)
	}
	if hunk.StartLine != 42 {
		t.Errorf("expected start_line 42, got %d", hunk.StartLine)
	}
	if hunk.EndLine != 44 {
		t.Errorf("expected end_line 44, got %d", hunk.EndLine)
	}

	jsonBytes, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("failed to marshal JSON: %v", err)
	}
	var roundtrip ChangeEnvelope
	if err := json.Unmarshal(jsonBytes, &roundtrip); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if roundtrip.Status != "FIXED" || len(roundtrip.FilesModified) != 1 {
		t.Errorf("roundtrip unmarshal failed: %+v", roundtrip)
	}
}

func TestSynthesizeEnvelope_Unresolved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		diff string
	}{
		{name: "empty string", diff: ""},
		{name: "whitespace only", diff: "   \n\t  \n"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			env, err := SynthesizeEnvelope("finding-456", "CWE-79", "XSS", "Failed to fix", tc.diff)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if env == nil {
				t.Fatal("expected non-nil ChangeEnvelope")
			}
			if env.Status != "UNRESOLVED" {
				t.Errorf("expected status 'UNRESOLVED', got %s", env.Status)
			}
			if env.Patch != "" {
				t.Errorf("expected empty patch, got %q", env.Patch)
			}
			if len(env.FilesModified) != 0 {
				t.Errorf("expected empty files_modified, got %v", env.FilesModified)
			}
			if len(env.Hunks) != 0 {
				t.Errorf("expected empty hunks, got %v", env.Hunks)
			}
		})
	}
}

func TestParseDiff_SingleHunk(t *testing.T) {
	t.Parallel()

	diff := `diff --git a/main.go b/main.go
index 1234567..89abcdef 100644
--- a/main.go
+++ b/main.go
@@ -10,3 +10,3 @@
-func oldFunc() {
+func newFunc() {
 	fmt.Println("hello")
 }
`

	files, hunks, err := ParseDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != "main.go" {
		t.Fatalf("expected files ['main.go'], got %v", files)
	}
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	h := hunks[0]
	if h.FilePath != "main.go" {
		t.Errorf("expected FilePath main.go, got %s", h.FilePath)
	}
	if h.StartLine != 10 {
		t.Errorf("expected start_line 10, got %d", h.StartLine)
	}
	if h.EndLine != 12 {
		t.Errorf("expected end_line 12, got %d", h.EndLine)
	}
	if !strings.Contains(h.Original, "oldFunc") || !strings.Contains(h.Replacement, "newFunc") {
		t.Errorf("unexpected hunk content: original=%q replacement=%q", h.Original, h.Replacement)
	}
}

func TestParseDiff_MultipleHunks(t *testing.T) {
	t.Parallel()

	diff := `diff --git a/server.go b/server.go
--- a/server.go
+++ b/server.go
@@ -5,2 +5,2 @@
-const Port = 8080
+const Port = 8443
 var Debug = false
@@ -25,3 +25,3 @@
 func Start() {
-	http.ListenAndServe(":8080", nil)
+	http.ListenAndServeTLS(":8443", "cert.pem", "key.pem", nil)
 }
`

	files, hunks, err := ParseDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != "server.go" {
		t.Fatalf("expected files ['server.go'], got %v", files)
	}
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}
	if hunks[0].StartLine != 5 || hunks[0].EndLine != 6 {
		t.Errorf("unexpected hunk 0 lines: %d to %d", hunks[0].StartLine, hunks[0].EndLine)
	}
	if hunks[1].StartLine != 25 || hunks[1].EndLine != 27 {
		t.Errorf("unexpected hunk 1 lines: %d to %d", hunks[1].StartLine, hunks[1].EndLine)
	}
}

func TestParseDiff_MultipleFiles(t *testing.T) {
	t.Parallel()

	diff := `diff --git a/file1.go b/file1.go
--- a/file1.go
+++ b/file1.go
@@ -1,2 +1,2 @@
-package old
+package new
diff --git a/file2.go b/file2.go
--- a/file2.go
+++ b/file2.go
@@ -10,1 +10,1 @@
-var x = 1
+var x = 2
`

	files, hunks, err := ParseDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 || files[0] != "file1.go" || files[1] != "file2.go" {
		t.Fatalf("expected ['file1.go', 'file2.go'], got %v", files)
	}
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}
	if hunks[0].FilePath != "file1.go" || hunks[1].FilePath != "file2.go" {
		t.Errorf("mismatched hunk filepaths: %v, %v", hunks[0].FilePath, hunks[1].FilePath)
	}
}

func TestParseDiff_NewFile(t *testing.T) {
	t.Parallel()

	diff := `diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package new
+
+func Added() {}
`

	files, hunks, err := ParseDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != "new.go" {
		t.Fatalf("expected ['new.go'], got %v", files)
	}
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	if hunks[0].FilePath != "new.go" {
		t.Errorf("expected FilePath new.go, got %s", hunks[0].FilePath)
	}
	if !strings.Contains(hunks[0].Replacement, "func Added()") {
		t.Errorf("expected replacement to contain Added(), got %q", hunks[0].Replacement)
	}
}

func TestParseDiff_DeletedFile(t *testing.T) {
	t.Parallel()

	diff := `diff --git a/deleted.go b/deleted.go
deleted file mode 100644
--- a/deleted.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package deleted
-
-func Old() {}
`

	files, hunks, err := ParseDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != "deleted.go" {
		t.Fatalf("expected ['deleted.go'], got %v", files)
	}
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	if hunks[0].FilePath != "deleted.go" {
		t.Errorf("expected FilePath deleted.go, got %s", hunks[0].FilePath)
	}
}

func TestParseDiff_StandardDiffU(t *testing.T) {
	t.Parallel()

	diff := `--- /workspace-ro/pkg/config.go	2026-08-20 12:00:00.000000000 +0000
+++ /workspace/pkg/config.go	2026-08-20 12:05:00.000000000 +0000
@@ -1,3 +1,3 @@
 package pkg
-var Timeout = 10
+var Timeout = 30
`

	files, hunks, err := ParseDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != "pkg/config.go" {
		t.Fatalf("expected ['pkg/config.go'], got %v", files)
	}
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	if hunks[0].FilePath != "pkg/config.go" {
		t.Errorf("expected pkg/config.go, got %s", hunks[0].FilePath)
	}
}

func TestParseDiff_EmptyAndMalformed(t *testing.T) {
	t.Parallel()

	t.Run("Empty diff string", func(t *testing.T) {
		files, hunks, err := ParseDiff("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 0 || len(hunks) != 0 {
			t.Errorf("expected empty results for empty diff")
		}
	})

	t.Run("Non-diff plain text", func(t *testing.T) {
		files, hunks, err := ParseDiff("This is just some build log output\nline 2\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 0 || len(hunks) != 0 {
			t.Errorf("expected empty results for non-diff text")
		}
	})
}

func TestExtractPatch_GitRepo(t *testing.T) {
	t.Parallel()

	ws := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = ws
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, string(out))
	}
	_ = exec.Command("git", "config", "user.name", "Test").Run()
	_ = exec.Command("git", "config", "user.email", "test@example.com").Run()

	filePath := filepath.Join(ws, "sample.txt")
	if err := os.WriteFile(filePath, []byte("initial line\n"), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	cmdAdd := exec.Command("git", "add", "sample.txt")
	cmdAdd.Dir = ws
	_ = cmdAdd.Run()
	cmdCommit := exec.Command("git", "commit", "-m", "initial")
	cmdCommit.Dir = ws
	_ = cmdCommit.Run()

	// Modify file
	if err := os.WriteFile(filePath, []byte("modified line\n"), 0644); err != nil {
		t.Fatalf("failed to write modified file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	patch, err := ExtractPatch(ctx, ws, "")
	if err != nil {
		t.Fatalf("ExtractPatch failed: %v", err)
	}
	if patch == "" {
		t.Fatal("expected non-empty patch")
	}
	if !strings.Contains(patch, "modified line") {
		t.Errorf("expected patch to contain 'modified line', got %s", patch)
	}
}

func TestExtractPatch_NonGitFallback(t *testing.T) {
	t.Parallel()

	roDir := t.TempDir()
	rwDir := t.TempDir()

	fileRO := filepath.Join(roDir, "foo.go")
	fileRW := filepath.Join(rwDir, "foo.go")

	if err := os.WriteFile(fileRO, []byte("original code\n"), 0644); err != nil {
		t.Fatalf("failed to write ro file: %v", err)
	}
	if err := os.WriteFile(fileRW, []byte("remediated code\n"), 0644); err != nil {
		t.Fatalf("failed to write rw file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	patch, err := ExtractPatch(ctx, rwDir, roDir)
	if err != nil {
		t.Fatalf("ExtractPatch failed: %v", err)
	}
	if patch == "" {
		t.Fatal("expected non-empty patch from directory diff fallback")
	}
	if !strings.Contains(patch, "remediated code") {
		t.Errorf("expected patch to contain 'remediated code', got %s", patch)
	}
}

func TestExtractPatch_NoChanges(t *testing.T) {
	t.Parallel()

	ws := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = ws
	_ = cmd.Run()
	_ = exec.Command("git", "config", "user.name", "Test").Run()
	_ = exec.Command("git", "config", "user.email", "test@example.com").Run()

	filePath := filepath.Join(ws, "file.txt")
	_ = os.WriteFile(filePath, []byte("same\n"), 0644)
	cmdAdd := exec.Command("git", "add", "file.txt")
	cmdAdd.Dir = ws
	_ = cmdAdd.Run()
	cmdCommit := exec.Command("git", "commit", "-m", "initial")
	cmdCommit.Dir = ws
	_ = cmdCommit.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	patch, err := ExtractPatch(ctx, ws, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patch != "" {
		t.Errorf("expected empty patch for clean repository, got %q", patch)
	}
}

func TestExtractPatch_NonGitWithoutFallback(t *testing.T) {
	t.Parallel()

	ws := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	patch, err := ExtractPatch(ctx, ws, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patch != "" {
		t.Errorf("expected empty patch when not a git repo and no fallback dir, got %q", patch)
	}
}

func TestExtractPatch_DirectoryDiffError(t *testing.T) {
	t.Parallel()

	ws := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := ExtractPatch(ctx, ws, "/non/existent/fallback/path")
	if err == nil {
		t.Fatal("expected error when fallback dir does not exist, got nil")
	}
}

func TestParseDiff_NoNewlineMarker(t *testing.T) {
	t.Parallel()

	diff := `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-old
\ No newline at end of file
+new
\ No newline at end of file
`

	files, hunks, err := ParseDiff(diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != "a.txt" {
		t.Fatalf("expected ['a.txt'], got %v", files)
	}
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
}
