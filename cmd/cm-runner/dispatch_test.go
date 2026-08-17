package main

import (
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"cm-connect/pkg/cmrunner"
)

// generateRandomStrings produces a slice of n random non-empty alphanumeric strings without leading 'cm'.
func generateRandomStrings(r *rand.Rand, n int) []string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789-_/."
	res := make([]string, n)
	for i := 0; i < n; i++ {
		length := r.Intn(10) + 1
		b := make([]byte, length)
		for j := 0; j < length; j++ {
			b[j] = chars[r.Intn(len(chars))]
		}
		str := string(b)
		if str == "cm" {
			str = "custom-token"
		}
		res[i] = str
	}
	return res
}

func TestStripCMPrefix_EdgeCases(t *testing.T) {
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
			name:     "only whitespace strings",
			input:    []string{"   ", "\t"},
			expected: []string{},
		},
		{
			name:     "only cm token",
			input:    []string{"cm"},
			expected: []string{},
		},
		{
			name:     "cm with whitespace",
			input:    []string{"  cm  ", "find"},
			expected: []string{"find"},
		},
		{
			name:     "cm find without path",
			input:    []string{"cm", "find"},
			expected: []string{"find"},
		},
		{
			name:     "find without cm prefix",
			input:    []string{"find", "src/auth"},
			expected: []string{"find", "src/auth"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := stripCMPrefix(tc.input)
			if len(result) != len(tc.expected) {
				t.Fatalf("for input %v: expected len %d, got %d: %v", tc.input, len(tc.expected), len(result), result)
			}
			for i := range result {
				if result[i] != tc.expected[i] {
					t.Errorf("for input %v at index %d: expected %q, got %q", tc.input, i, tc.expected[i], result[i])
				}
			}
		})
	}
}

func TestStripCMPrefix_Randomized(t *testing.T) {
	seed := time.Now().UnixNano()
	r := rand.New(rand.NewSource(seed))

	for i := 0; i < 100; i++ {
		numTokens := r.Intn(20) + 1
		randomTokens := generateRandomStrings(r, numTokens)
		input := append([]string{"cm"}, randomTokens...)
		result := stripCMPrefix(input)

		if len(result) != len(randomTokens) {
			t.Fatalf("FAILED with seed %d\nInput was: %v\nExpected len %d, got len %d: %v",
				seed, input, len(randomTokens), len(result), result)
		}

		for j := range result {
			if result[j] != randomTokens[j] {
				t.Fatalf("FAILED with seed %d\nInput was: %v\nAt index %d: expected %q, got %q",
					seed, input, j, randomTokens[j], result[j])
			}
		}
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
			name:          "only cm token returns missing subcommand error",
			args:          []string{"cm"},
			expectError:   true,
			expectedError: errMissingSubcommand,
		},
		{
			name:          "whitespace token returns missing subcommand error",
			args:          []string{"   "},
			expectError:   true,
			expectedError: errMissingSubcommand,
		},
		{
			name:          "unknown subcommand returns invalid subcommand error",
			args:          []string{"invalid-cmd"},
			expectError:   true,
			expectedError: errInvalidSubcommand,
		},
		{
			name:          "unknown subcommand with cm prefix and whitespace",
			args:          []string{" cm ", "invalid-cmd"},
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
			args:              []string{"cm", "shell", "src/auth"},
			expectedIsShell:   true,
			expectedTargetDir: filepath.Join(tmpDir, "src/auth"),
			expectError:       false,
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
			name:             "cm find with no target path defaults to dot",
			args:             []string{"cm", "find"},
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
			args:              []string{"find", "src/auth", "--", "--format=sarif", "-y"},
			expectedCmdCount:  2,
			expectedTarget:    "src/auth",
			expectedFormat:    "sarif",
			expectedScanFlags: []string{"-y"},
			expectError:       false,
		},
		{
			name:             "find with double-dash and no explicit path defaults to dot",
			args:             []string{"find", "--", "--format=json"},
			expectedCmdCount: 2,
			expectedTarget:   ".",
			expectedFormat:   "json",
			expectError:      false,
		},
		{
			name:             "find with flags without double dash",
			args:             []string{"find", "--format=sarif", "src/auth"},
			expectedCmdCount: 2,
			expectedTarget:   "src/auth",
			expectedFormat:   "sarif",
			expectError:      false,
		},
		{
			name:             "find with help flag returns only find command",
			args:             []string{"find", "--help"},
			expectedCmdCount: 1,
			expectedTarget:   ".",
			expectError:      false,
		},
		{
			name:          "find with non-existent sub-path returns path not found error",
			args:          []string{"find", "non/existent/path"},
			expectError:   true,
			expectedError: errPathNotFound,
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
