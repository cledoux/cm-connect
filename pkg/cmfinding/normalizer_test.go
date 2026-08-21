package cmfinding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestImportFinding_SingleObject(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	targetFile := filepath.Join(ws, "pkg", "auth", "store.go")
	if err := os.MkdirAll(filepath.Dir(targetFile), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(targetFile, []byte("package auth\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	raw := []byte(`{
		"FilePath": "pkg/auth/store.go",
		"StartLine": 42,
		"Title": "SQL Injection in User Lookup",
		"Analysis": "Replaced raw concatenation with parameterization",
		"Severity": "CRITICAL",
		"VulnType": "CWE-89",
		"Snippet": "query := fmt.Sprintf(...)",
		"Status": "OPEN"
	}`)

	outBytes, finding, err := ImportFinding(raw, ws)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if finding == nil {
		t.Fatal("expected non-nil finding")
	}
	if finding.FilePath != "pkg/auth/store.go" {
		t.Errorf("expected file_path 'pkg/auth/store.go', got '%s'", finding.FilePath)
	}
	if finding.Line != 42 {
		t.Errorf("expected line 42, got %d", finding.Line)
	}
	if finding.Title != "SQL Injection in User Lookup" {
		t.Errorf("expected title 'SQL Injection in User Lookup', got '%s'", finding.Title)
	}
	if finding.Message != "Replaced raw concatenation with parameterization" {
		t.Errorf("expected message 'Replaced raw concatenation with parameterization', got '%s'", finding.Message)
	}
	if finding.Severity != "CRITICAL" {
		t.Errorf("expected severity 'CRITICAL', got '%s'", finding.Severity)
	}
	if finding.VulnType != "CWE-89" {
		t.Errorf("expected vuln_type 'CWE-89', got '%s'", finding.VulnType)
	}
	if finding.Snippet != "query := fmt.Sprintf(...)" {
		t.Errorf("expected snippet 'query := fmt.Sprintf(...)', got '%s'", finding.Snippet)
	}
	if finding.Status != "OPEN" {
		t.Errorf("expected status 'OPEN', got '%s'", finding.Status)
	}

	var imported []FindingImport
	if err := json.Unmarshal(outBytes, &imported); err != nil {
		t.Fatalf("failed to unmarshal output JSON: %v", err)
	}
	if len(imported) != 1 {
		t.Fatalf("expected 1 element in imported JSON array, got %d", len(imported))
	}
	if imported[0].FilePath != finding.FilePath || imported[0].Line != finding.Line {
		t.Errorf("serialized JSON does not match returned struct")
	}
}

func TestNewFindingFromJSON_And_ToImport(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	targetFile := filepath.Join(ws, "main.go")
	if err := os.WriteFile(targetFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	raw := []byte(`{
		"FilePath": "main.go",
		"StartLine": 10,
		"Title": "Reflected XSS",
		"Analysis": "Escape HTML output",
		"Severity": "MEDIUM",
		"VulnType": "CWE-79",
		"Snippet": "fmt.Fprintf(w, input)",
		"Status": "OPEN"
	}`)

	finding, err := NewFindingFromJSON(raw)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if finding == nil {
		t.Fatal("expected non-nil finding")
	}

	imported, err := finding.ToImport(ws)
	if err != nil {
		t.Fatalf("expected no error from ToImport, got: %v", err)
	}
	if imported.FilePath != "main.go" || imported.Line != 10 || imported.VulnType != "CWE-79" {
		t.Errorf("unexpected imported finding content: %+v", imported)
	}
}

func TestNewFindingFromJSON_RejectsArrays(t *testing.T) {
	t.Parallel()
	raw := []byte(`[{"FilePath": "main.go"}]`)
	_, err := NewFindingFromJSON(raw)
	if err == nil {
		t.Fatal("expected error for array input, got nil")
	}
}

func TestImportFinding_FullReportFinding(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	targetFile := filepath.Join(ws, "api", "handler.go")
	if err := os.MkdirAll(filepath.Dir(targetFile), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(targetFile, []byte("package api\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	raw := []byte(`{
		"FindingID": "cm-finding-12345",
		"FilePath": "api/handler.go",
		"StartLine": 15,
		"EndLine": 18,
		"Title": "Hardcoded Secret",
		"Analysis": "Do not hardcode credentials in code",
		"Severity": "high",
		"VulnType": "CWE-798",
		"Snippet": "secret = \"12345\"",
		"Status": "OPEN"
	}`)

	_, finding, err := ImportFinding(raw, ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if finding == nil {
		t.Fatal("expected non-nil finding")
	}
	if finding.FilePath != "api/handler.go" {
		t.Errorf("expected api/handler.go, got %s", finding.FilePath)
	}
	if finding.Line != 15 {
		t.Errorf("expected 15, got %d", finding.Line)
	}
	if finding.Message != "Do not hardcode credentials in code" {
		t.Errorf("expected message, got %s", finding.Message)
	}
	if finding.Severity != "HIGH" {
		t.Errorf("expected HIGH, got %s", finding.Severity)
	}
	if finding.VulnType != "CWE-798" {
		t.Errorf("expected CWE-798, got %s", finding.VulnType)
	}
}

func TestImportFinding_FilePrefixStripping(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	targetFile := filepath.Join(ws, "pkg", "db.go")
	if err := os.MkdirAll(filepath.Dir(targetFile), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(targetFile, []byte("package pkg\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	tests := []struct {
		name     string
		filePath string
		expected string
	}{
		{
			name:     "file:// relative prefix",
			filePath: "file://pkg/db.go",
			expected: "pkg/db.go",
		},
		{
			name:     "file:///workspace/ absolute prefix",
			filePath: "file:///workspace/pkg/db.go",
			expected: "pkg/db.go",
		},
		{
			name:     "/workspace/ absolute prefix without file://",
			filePath: "/workspace/pkg/db.go",
			expected: "pkg/db.go",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"FilePath": "` + tc.filePath + `", "Title": "Issue", "Snippet": "foo"}`)
			_, finding, err := ImportFinding(raw, ws)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if finding == nil {
				t.Fatal("expected non-nil finding")
			}
			if finding.FilePath != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, finding.FilePath)
			}
		})
	}
}

func TestImportFinding_DefaultsAndFallbacks(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	targetFile := filepath.Join(ws, "test.go")
	if err := os.WriteFile(targetFile, []byte("package test\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	t.Run("Line zero and negative defaults to 1", func(t *testing.T) {
		raw := []byte(`{"FilePath": "test.go", "StartLine": 0, "Title": "Title"}`)
		_, finding, err := ImportFinding(raw, ws)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if finding == nil {
			t.Fatal("expected non-nil finding")
		}
		if finding.Line != 1 {
			t.Errorf("expected line 1, got %d", finding.Line)
		}

		rawNeg := []byte(`{"FilePath": "test.go", "StartLine": -5, "Title": "Title"}`)
		_, findingNeg, err := ImportFinding(rawNeg, ws)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if findingNeg == nil {
			t.Fatal("expected non-nil finding")
		}
		if findingNeg.Line != 1 {
			t.Errorf("expected line 1, got %d", findingNeg.Line)
		}
	})

	t.Run("Empty title defaults to Security Finding", func(t *testing.T) {
		raw := []byte(`{"FilePath": "test.go", "Analysis": "Analysis details"}`)
		_, finding, err := ImportFinding(raw, ws)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if finding == nil {
			t.Fatal("expected non-nil finding")
		}
		if finding.Title != "Security Finding" {
			t.Errorf("expected 'Security Finding', got '%s'", finding.Title)
		}
	})

	t.Run("Empty message falls back to Snippet then Title", func(t *testing.T) {
		// Fallback to Snippet
		raw1 := []byte(`{"FilePath": "test.go", "Title": "My Title", "Snippet": "my snippet"}`)
		_, finding1, err := ImportFinding(raw1, ws)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if finding1 == nil {
			t.Fatal("expected non-nil finding")
		}
		if finding1.Message != "my snippet" {
			t.Errorf("expected 'my snippet', got '%s'", finding1.Message)
		}

		// Fallback to Title
		raw2 := []byte(`{"FilePath": "test.go", "Title": "My Title"}`)
		_, finding2, err := ImportFinding(raw2, ws)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if finding2 == nil {
			t.Fatal("expected non-nil finding")
		}
		if finding2.Message != "My Title" {
			t.Errorf("expected 'My Title', got '%s'", finding2.Message)
		}
	})

	t.Run("Severity defaults to HIGH and uppercases", func(t *testing.T) {
		rawEmpty := []byte(`{"FilePath": "test.go", "Title": "Issue"}`)
		_, findingEmpty, err := ImportFinding(rawEmpty, ws)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if findingEmpty == nil {
			t.Fatal("expected non-nil finding")
		}
		if findingEmpty.Severity != "HIGH" {
			t.Errorf("expected HIGH, got %s", findingEmpty.Severity)
		}

		rawLower := []byte(`{"FilePath": "test.go", "Title": "Issue", "Severity": "medium"}`)
		_, findingLower, err := ImportFinding(rawLower, ws)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if findingLower == nil {
			t.Fatal("expected non-nil finding")
		}
		if findingLower.Severity != "MEDIUM" {
			t.Errorf("expected MEDIUM, got %s", findingLower.Severity)
		}
	})

	t.Run("Status defaults to OPEN", func(t *testing.T) {
		raw := []byte(`{"FilePath": "test.go", "Title": "Issue"}`)
		_, finding, err := ImportFinding(raw, ws)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if finding == nil {
			t.Fatal("expected non-nil finding")
		}
		if finding.Status != "OPEN" {
			t.Errorf("expected OPEN, got %s", finding.Status)
		}
	})

	t.Run("VulnID used when VulnType is empty", func(t *testing.T) {
		raw := []byte(`{"FilePath": "test.go", "Title": "Issue", "VulnID": "CWE-22"}`)
		_, finding, err := ImportFinding(raw, ws)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if finding == nil {
			t.Fatal("expected non-nil finding")
		}
		if finding.VulnType != "CWE-22" {
			t.Errorf("expected CWE-22, got %s", finding.VulnType)
		}
	})
}

func TestImportFinding_CanonicalFields(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	targetFile := filepath.Join(ws, "alias.go")
	if err := os.WriteFile(targetFile, []byte("package alias\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	t.Run("StartLine and VulnType", func(t *testing.T) {
		raw := []byte(`{"FilePath": "alias.go", "StartLine": 20, "VulnType": "CWE-1", "Analysis": "custom analysis"}`)
		_, f, err := ImportFinding(raw, ws)
		if err != nil || f == nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.FilePath != "alias.go" || f.Line != 20 || f.VulnType != "CWE-1" || f.Message != "custom analysis" {
			t.Errorf("unexpected normalized finding: %+v", f)
		}
	})

	t.Run("VulnID fallback", func(t *testing.T) {
		raw := []byte(`{"FilePath": "alias.go", "StartLine": 30, "Analysis": "msg", "VulnID": "CWE-2"}`)
		_, f, err := ImportFinding(raw, ws)
		if err != nil || f == nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.Line != 30 || f.Message != "msg" || f.VulnType != "CWE-2" {
			t.Errorf("unexpected normalized finding: %+v", f)
		}
	})
}

func TestImportFinding_WorkspaceValidation(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	existingFile := filepath.Join(ws, "exists.go")
	if err := os.WriteFile(existingFile, []byte("package exists\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	subDir := filepath.Join(ws, "some_dir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	tests := []struct {
		name      string
		rawJSON   string
		expectErr bool
	}{
		{
			name:      "Existing file succeeds",
			rawJSON:   `{"FilePath": "exists.go", "Title": "Issue"}`,
			expectErr: false,
		},
		{
			name:      "Non-existent file fails",
			rawJSON:   `{"FilePath": "nonexistent.go", "Title": "Issue"}`,
			expectErr: true,
		},
		{
			name:      "Directory path fails",
			rawJSON:   `{"FilePath": "some_dir", "Title": "Issue"}`,
			expectErr: true,
		},
		{
			name:      "Path traversal outside workspace fails",
			rawJSON:   `{"FilePath": "../outside.go", "Title": "Issue"}`,
			expectErr: true,
		},
		{
			name:      "Missing FilePath fails",
			rawJSON:   `{"Title": "Issue without file"}`,
			expectErr: true,
		},
		{
			name:      "Empty FilePath string fails",
			rawJSON:   `{"FilePath": "", "Title": "Empty file"}`,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ImportFinding([]byte(tc.rawJSON), ws)
			if tc.expectErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestImportFinding_InvalidJSON(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()

	tests := []struct {
		name    string
		rawJSON string
	}{
		{
			name:    "Malformed JSON syntax",
			rawJSON: `{"FilePath": "foo.go", `,
		},
		{
			name:    "Empty input",
			rawJSON: ``,
		},
		{
			name:    "Whitespace only",
			rawJSON: `   `,
		},
		{
			name:    "Null literal",
			rawJSON: `null`,
		},
		{
			name:    "JSON string primitive",
			rawJSON: `"just a string"`,
		},
		{
			name:    "JSON number primitive",
			rawJSON: `12345`,
		},
		{
			name:    "JSON boolean primitive",
			rawJSON: `true`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ImportFinding([]byte(tc.rawJSON), ws)
			if err == nil {
				t.Errorf("expected error for invalid JSON input %q, got nil", tc.rawJSON)
			}
		})
	}
}

func TestImportFinding_ArrayCardinality(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	f1 := filepath.Join(ws, "f1.go")
	f2 := filepath.Join(ws, "f2.go")
	_ = os.WriteFile(f1, []byte("package f1\n"), 0644)
	_ = os.WriteFile(f2, []byte("package f2\n"), 0644)

	t.Run("Empty array fails", func(t *testing.T) {
		_, _, err := ImportFinding([]byte(`[]`), ws)
		if err == nil {
			t.Error("expected error for empty array, got nil")
		}
	})

	t.Run("Multi-element array fails single finding requirement", func(t *testing.T) {
		raw := `[
			{"FilePath": "f1.go", "Title": "Issue 1"},
			{"FilePath": "f2.go", "Title": "Issue 2"}
		]`
		_, _, err := ImportFinding([]byte(raw), ws)
		if err == nil {
			t.Error("expected error for multi-element array, got nil")
		}
	})
}
