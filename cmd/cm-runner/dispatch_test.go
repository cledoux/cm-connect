package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripCMPrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty input",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "only cm token",
			input:    []string{"cm"},
			expected: []string{},
		},
		{
			name:     "cm find without path",
			input:    []string{"cm", "find"},
			expected: []string{"find"},
		},
		{
			name:     "cm find with path and flags",
			input:    []string{"cm", "find", "src/auth", "--format", "json"},
			expected: []string{"find", "src/auth", "--format", "json"},
		},
		{
			name:     "find without cm prefix",
			input:    []string{"find", "src/auth"},
			expected: []string{"find", "src/auth"},
		},
		{
			name:     "shell with cm prefix",
			input:    []string{"cm", "shell"},
			expected: []string{"shell"},
		},
		{
			name:     "shell without cm prefix",
			input:    []string{"shell"},
			expected: []string{"shell"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := StripCMPrefix(tc.input)
			if len(result) != len(tc.expected) {
				t.Fatalf("expected len %d, got %d: %v", len(tc.expected), len(result), result)
			}
			for i := range result {
				if result[i] != tc.expected[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tc.expected[i], result[i])
				}
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test directory structure
	authDir := filepath.Join(tmpDir, "src", "auth")
	if err := os.MkdirAll(authDir, 0755); err != nil {
		t.Fatalf("failed to create temp dirs: %v", err)
	}
	testFile := filepath.Join(authDir, "handler.go")
	if err := os.WriteFile(testFile, []byte("package auth"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	tests := []struct {
		name        string
		targetPath  string
		expected    string
		expectError bool
		errContains string
	}{
		{
			name:        "empty path defaults to dot",
			targetPath:  "",
			expected:    ".",
			expectError: false,
		},
		{
			name:        "dot path remains dot",
			targetPath:  ".",
			expected:    ".",
			expectError: false,
		},
		{
			name:        "slash dot path",
			targetPath:  "./",
			expected:    ".",
			expectError: false,
		},
		{
			name:        "valid relative directory",
			targetPath:  "src/auth",
			expected:    "src/auth",
			expectError: false,
		},
		{
			name:        "valid relative directory with dot-slash",
			targetPath:  "./src/auth",
			expected:    "src/auth",
			expectError: false,
		},
		{
			name:        "valid file path",
			targetPath:  "src/auth/handler.go",
			expected:    "src/auth/handler.go",
			expectError: false,
		},
		{
			name:        "non-existent directory",
			targetPath:  "non/existent/path",
			expectError: true,
			errContains: "scan target path 'non/existent/path' does not exist in " + tmpDir,
		},
		{
			name:        "path traversal escaping workspace",
			targetPath:  "../../etc/passwd",
			expectError: true,
			errContains: "scan target path '../../etc/passwd' does not exist in " + tmpDir,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			normalized, err := NormalizePath(tmpDir, tc.targetPath)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil (normalized: %q)", normalized)
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got %q", tc.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if normalized != tc.expected {
					t.Errorf("expected %q, got %q", tc.expected, normalized)
				}
			}
		})
	}
}

func TestDispatchCommand(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "src", "auth")
	if err := os.MkdirAll(authDir, 0755); err != nil {
		t.Fatalf("failed to create temp dirs: %v", err)
	}

	tests := []struct {
		name           string
		args           []string
		expectedType   CommandType
		expectedTarget string
		expectedFlags  []string
		expectError    bool
		errContains    string
	}{
		{
			name:        "empty args returns error",
			args:        []string{},
			expectError: true,
			errContains: "missing subcommand",
		},
		{
			name:        "only cm token returns error",
			args:        []string{"cm"},
			expectError: true,
			errContains: "missing subcommand",
		},
		{
			name:        "unknown subcommand returns error",
			args:        []string{"invalid-cmd"},
			expectError: true,
			errContains: "unrecognized subcommand 'invalid-cmd'",
		},
		{
			name:        "unknown subcommand with cm prefix returns error",
			args:        []string{"cm", "invalid-cmd"},
			expectError: true,
			errContains: "unrecognized subcommand 'invalid-cmd'",
		},
		{
			name:           "find with no target path defaults to dot",
			args:           []string{"find"},
			expectedType:   CmdTypeFind,
			expectedTarget: ".",
			expectedFlags:  []string{},
			expectError:    false,
		},
		{
			name:           "cm find with no target path defaults to dot",
			args:           []string{"cm", "find"},
			expectedType:   CmdTypeFind,
			expectedTarget: ".",
			expectedFlags:  []string{},
			expectError:    false,
		},
		{
			name:           "find with scoped sub-path",
			args:           []string{"find", "src/auth"},
			expectedType:   CmdTypeFind,
			expectedTarget: "src/auth",
			expectedFlags:  []string{},
			expectError:    false,
		},
		{
			name:           "cm find with scoped sub-path and custom flags",
			args:           []string{"cm", "find", "src/auth", "--model", "vertex:gemini-1.5-pro"},
			expectedType:   CmdTypeFind,
			expectedTarget: "src/auth",
			expectedFlags:  []string{"--model", "vertex:gemini-1.5-pro"},
			expectError:    false,
		},
		{
			name:           "find with flags before target path",
			args:           []string{"find", "--model", "vertex:gemini-1.5-pro", "src/auth"},
			expectedType:   CmdTypeFind,
			expectedTarget: "src/auth",
			expectedFlags:  []string{"--model", "vertex:gemini-1.5-pro"},
			expectError:    false,
		},
		{
			name:           "find with format flag and no target path",
			args:           []string{"find", "--format", "sarif"},
			expectedType:   CmdTypeFind,
			expectedTarget: ".",
			expectedFlags:  []string{"--format", "sarif"},
			expectError:    false,
		},
		{
			name:        "find with non-existent sub-path returns error",
			args:        []string{"find", "non/existent/path"},
			expectError: true,
			errContains: "scan target path 'non/existent/path' does not exist in " + tmpDir,
		},
		{
			name:         "shell subcommand",
			args:         []string{"shell"},
			expectedType: CmdTypeShell,
			expectError:  false,
		},
		{
			name:         "cm shell subcommand",
			args:         []string{"cm", "shell"},
			expectedType: CmdTypeShell,
			expectError:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := DispatchCommand(tmpDir, tc.args)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error, got plan: %+v", plan)
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got %q", tc.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if plan.Type != tc.expectedType {
					t.Errorf("expected type %v, got %v", tc.expectedType, plan.Type)
				}
				if tc.expectedType == CmdTypeFind {
					if plan.TargetPath != tc.expectedTarget {
						t.Errorf("expected target %q, got %q", tc.expectedTarget, plan.TargetPath)
					}
					if len(plan.Flags) != len(tc.expectedFlags) {
						t.Fatalf("expected flags %v, got %v", tc.expectedFlags, plan.Flags)
					}
					for i := range plan.Flags {
						if plan.Flags[i] != tc.expectedFlags[i] {
							t.Errorf("flag[%d]: expected %q, got %q", i, tc.expectedFlags[i], plan.Flags[i])
						}
					}
				}
			}
		})
	}
}
