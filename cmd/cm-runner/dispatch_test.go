package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"cm-connect/pkg/cmrunner"
)

func TestPartitionDash(t *testing.T) {
	tests := []struct {
		name               string
		args               []string
		expectedBeforeDash []string
		expectedAfterDash  []string
	}{
		{
			name:               "empty args",
			args:               []string{},
			expectedBeforeDash: []string{},
			expectedAfterDash:  nil,
		},
		{
			name:               "no double dash",
			args:               []string{"src/auth"},
			expectedBeforeDash: []string{"src/auth"},
			expectedAfterDash:  nil,
		},
		{
			name:               "double dash with target path and scan flags",
			args:               []string{"src/auth", "--", "-c", "5", "--unrestricted"},
			expectedBeforeDash: []string{"src/auth"},
			expectedAfterDash:  []string{"-c", "5", "--unrestricted"},
		},
		{
			name:               "double dash at start",
			args:               []string{"--", "-c", "5"},
			expectedBeforeDash: []string{},
			expectedAfterDash:  []string{"-c", "5"},
		},
		{
			name:               "trailing double dash without following flags",
			args:               []string{"src/auth", "--"},
			expectedBeforeDash: []string{"src/auth"},
			expectedAfterDash:  []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before, after := partitionDash(tc.args)
			if !reflect.DeepEqual(before, tc.expectedBeforeDash) {
				t.Errorf("expected beforeDash %v, got %v", tc.expectedBeforeDash, before)
			}
			if !reflect.DeepEqual(after, tc.expectedAfterDash) {
				t.Errorf("expected afterDash %v, got %v", tc.expectedAfterDash, after)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tmpDir := t.TempDir()

	authDir := filepath.Join(tmpDir, "src", "auth")
	if err := os.MkdirAll(authDir, 0755); err != nil {
		t.Fatalf("failed to create temp dirs: %v", err)
	}
	testFile := filepath.Join(authDir, "handler.go")
	if err := os.WriteFile(testFile, []byte("package auth"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	tests := []struct {
		name          string
		targetPath    string
		expected      string
		expectError   bool
		expectedError error
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
			name:        "valid absolute path inside workspace",
			targetPath:  filepath.Join(tmpDir, "src", "auth"),
			expected:    "src/auth",
			expectError: false,
		},
		{
			name:          "absolute path outside workspace",
			targetPath:    "/etc/passwd",
			expectError:   true,
			expectedError: errPathTraversal,
		},
		{
			name:          "path traversal escaping workspace",
			targetPath:    "../../etc/passwd",
			expectError:   true,
			expectedError: errPathTraversal,
		},
		{
			name:          "non-existent directory",
			targetPath:    "non/existent/path",
			expectError:   true,
			expectedError: errPathNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			normalized, err := normalizePath(tmpDir, tc.targetPath)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil (normalized: %q)", normalized)
				}
				if tc.expectedError != nil && !errors.Is(err, tc.expectedError) {
					t.Errorf("expected sentinel error %v, got %v", tc.expectedError, err)
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

func TestParseArgs(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "src", "auth")
	if err := os.MkdirAll(authDir, 0755); err != nil {
		t.Fatalf("failed to create temp dirs: %v", err)
	}

	tests := []struct {
		name              string
		args              []string
		expectedCmdCount  int
		expectedTarget    string
		expectedFormat    string
		expectedScanFlags []string
		expectedIsShell   bool
		expectedTargetDir string
		expectError       bool
		expectedError     error
	}{
		{
			name:          "empty args returns missing subcommand error",
			args:          []string{},
			expectError:   true,
			expectedError: errMissingSubcommand,
		},
		{
			name:          "only cm token returns invalid subcommand error",
			args:          []string{"cm"},
			expectError:   true,
			expectedError: errInvalidSubcommand,
		},
		{
			name:          "cm prefix with find returns invalid subcommand error",
			args:          []string{"cm", "find"},
			expectError:   true,
			expectedError: errInvalidSubcommand,
		},
		{
			name:          "cm prefix with shell returns invalid subcommand error",
			args:          []string{"cm", "shell", "src/auth"},
			expectError:   true,
			expectedError: errInvalidSubcommand,
		},
		{
			name:          "unknown subcommand returns invalid subcommand error",
			args:          []string{"invalid-cmd"},
			expectError:   true,
			expectedError: errInvalidSubcommand,
		},
		{
			name:              "shell subcommand defaults to workspace root",
			args:              []string{"shell"},
			expectedIsShell:   true,
			expectedTargetDir: tmpDir,
			expectError:       false,
		},
		{
			name:              "shell subcommand with scoped path",
			args:              []string{"shell", "src/auth"},
			expectedIsShell:   true,
			expectedTargetDir: filepath.Join(tmpDir, "src/auth"),
			expectError:       false,
		},
		{
			name:          "shell subcommand with non-existent path returns path not found error",
			args:          []string{"shell", "non/existent/path"},
			expectError:   true,
			expectedError: errPathNotFound,
		},
		{
			name:             "find with no target path defaults to dot and json",
			args:             []string{"find"},
			expectedCmdCount: 2,
			expectedTarget:   ".",
			expectedFormat:   "json",
			expectError:      false,
		},
		{
			name:             "find with scoped sub-path",
			args:             []string{"find", "src/auth"},
			expectedCmdCount: 2,
			expectedTarget:   "src/auth",
			expectedFormat:   "json",
			expectError:      false,
		},
		{
			name:              "find with double-dash separating forwarded flags",
			args:              []string{"find", "src/auth", "--", "-c", "5", "--unrestricted"},
			expectedCmdCount:  2,
			expectedTarget:    "src/auth",
			expectedFormat:    "json",
			expectedScanFlags: []string{"-c", "5", "--unrestricted"},
			expectError:       false,
		},
		{
			name:              "find with double-dash and no explicit path defaults to dot",
			args:              []string{"find", "--", "-c", "5"},
			expectedCmdCount:  2,
			expectedTarget:    ".",
			expectedFormat:    "json",
			expectedScanFlags: []string{"-c", "5"},
			expectError:       false,
		},
		{
			name:             "find with trailing double-dash",
			args:             []string{"find", "src/auth", "--"},
			expectedCmdCount: 2,
			expectedTarget:   "src/auth",
			expectedFormat:   "json",
			expectError:      false,
		},
		{
			name:              "find with help flag returns only find command",
			args:              []string{"find", "--", "--help"},
			expectedCmdCount:  1,
			expectedTarget:    ".",
			expectedScanFlags: []string{"--help"},
			expectError:       false,
		},
		{
			name:          "find with non-existent sub-path returns path not found error",
			args:          []string{"find", "non/existent/path"},
			expectError:   true,
			expectedError: errPathNotFound,
		},
		{
			name:          "find with path traversal returns path traversal error",
			args:          []string{"find", "../../etc/passwd"},
			expectError:   true,
			expectedError: errPathTraversal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmds, targetDir, isShell, err := parseArgs(tmpDir, tc.args)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error, got commands: %+v", cmds)
				}
				if tc.expectedError != nil && !errors.Is(err, tc.expectedError) {
					t.Errorf("expected sentinel error %v, got %v", tc.expectedError, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if isShell != tc.expectedIsShell {
					t.Errorf("expected isShell %v, got %v", tc.expectedIsShell, isShell)
				}
				if tc.expectedIsShell {
					if targetDir != tc.expectedTargetDir {
						t.Errorf("expected targetDir %q, got %q", tc.expectedTargetDir, targetDir)
					}
					return
				}

				if len(cmds) != tc.expectedCmdCount {
					t.Fatalf("expected %d commands, got %d", tc.expectedCmdCount, len(cmds))
				}
				findCmd, ok := cmds[0].(*cmrunner.FindCommand)
				if !ok {
					t.Fatalf("expected cmds[0] to be *cmrunner.FindCommand, got %T", cmds[0])
				}
				if findCmd.TargetPath != tc.expectedTarget {
					t.Errorf("expected target %q, got %q", tc.expectedTarget, findCmd.TargetPath)
				}
				if tc.expectedScanFlags != nil && !reflect.DeepEqual(findCmd.Flags, tc.expectedScanFlags) {
					t.Errorf("expected scan flags %v, got %v", tc.expectedScanFlags, findCmd.Flags)
				}

				if tc.expectedCmdCount > 1 {
					reportCmd, ok := cmds[1].(*cmrunner.ReportCommand)
					if !ok {
						t.Fatalf("expected cmds[1] to be *cmrunner.ReportCommand, got %T", cmds[1])
					}
					if reportCmd.Format != tc.expectedFormat {
						t.Errorf("expected format %q, got %q", tc.expectedFormat, reportCmd.Format)
					}
				}
			}
		})
	}
}
