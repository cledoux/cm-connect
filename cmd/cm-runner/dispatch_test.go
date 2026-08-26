package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

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

func TestParseInitArgs(t *testing.T) {
	t.Run("default init when no args provided", func(t *testing.T) {
		plan, err := parseInitArgs([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.Action != ActionInit {
			t.Errorf("expected plan.Action ActionInit, got %v", plan.Action)
		}
	})

	t.Run("help flag --help", func(t *testing.T) {
		plan, err := parseInitArgs([]string{"--help"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.Action != ActionHelp {
			t.Errorf("expected plan.Action ActionHelp, got %v", plan.Action)
		}
	})

	t.Run("help flag -h", func(t *testing.T) {
		plan, err := parseInitArgs([]string{"-h"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.Action != ActionHelp {
			t.Errorf("expected plan.Action ActionHelp, got %v", plan.Action)
		}
	})

	t.Run("error on unexpected positional arguments", func(t *testing.T) {
		_, err := parseInitArgs([]string{"/custom/path/config.yaml"})
		if err == nil {
			t.Errorf("expected error for unexpected arguments, got nil")
		}
	})
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
		expectedAction    ActionType
		expectedCmdCount  int
		expectedTarget    string
		expectedFormat    string
		expectedScanFlags []string
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
			name:          "cm prefix with init returns invalid subcommand error",
			args:          []string{"cm", "init"},
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
			expectedAction:    ActionShell,
			expectedTargetDir: tmpDir,
			expectError:       false,
		},
		{
			name:              "shell subcommand with scoped path",
			args:              []string{"shell", "src/auth"},
			expectedAction:    ActionShell,
			expectedTargetDir: filepath.Join(tmpDir, "src/auth"),
			expectError:       false,
		},
		{
			name:           "shell subcommand with help flag",
			args:           []string{"shell", "--help"},
			expectedAction: ActionHelp,
			expectError:    false,
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
			expectedAction:   ActionRunSequence,
			expectedCmdCount: 2,
			expectedTarget:   ".",
			expectedFormat:   "json",
			expectError:      false,
		},
		{
			name:             "find with scoped sub-path",
			args:             []string{"find", "src/auth"},
			expectedAction:   ActionRunSequence,
			expectedCmdCount: 2,
			expectedTarget:   "src/auth",
			expectedFormat:   "json",
			expectError:      false,
		},
		{
			name:              "find with double-dash separating forwarded flags",
			args:              []string{"find", "src/auth", "--", "-c", "5", "--unrestricted"},
			expectedAction:    ActionRunSequence,
			expectedCmdCount:  2,
			expectedTarget:    "src/auth",
			expectedFormat:    "json",
			expectedScanFlags: []string{"-c", "5", "--unrestricted"},
			expectError:       false,
		},
		{
			name:              "find with double-dash and no explicit path defaults to dot",
			args:              []string{"find", "--", "-c", "5"},
			expectedAction:    ActionRunSequence,
			expectedCmdCount:  2,
			expectedTarget:    ".",
			expectedFormat:    "json",
			expectedScanFlags: []string{"-c", "5"},
			expectError:       false,
		},
		{
			name:             "find with trailing double-dash",
			args:             []string{"find", "src/auth", "--"},
			expectedAction:   ActionRunSequence,
			expectedCmdCount: 2,
			expectedTarget:   "src/auth",
			expectedFormat:   "json",
			expectError:      false,
		},
		{
			name:           "find with help flag returns ActionHelp",
			args:           []string{"find", "--", "--help"},
			expectedAction: ActionHelp,
			expectError:    false,
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
		{
			name:           "init subcommand returns ActionInit",
			args:           []string{"init"},
			expectedAction: ActionInit,
			expectError:    false,
		},
		{
			name:           "init subcommand with help flag",
			args:           []string{"init", "--help"},
			expectedAction: ActionHelp,
			expectError:    false,
		},
		{
			name:        "init subcommand with unexpected positional argument returns error",
			args:        []string{"init", "/custom/path.yaml"},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := parseArgs(tmpDir, tc.args)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error, got plan: %+v", plan)
				}
				if tc.expectedError != nil && !errors.Is(err, tc.expectedError) {
					t.Errorf("expected sentinel error %v, got %v", tc.expectedError, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if plan.Action != tc.expectedAction {
					t.Errorf("expected action %v, got %v", tc.expectedAction, plan.Action)
				}
				if plan.Action == ActionHelp || plan.Action == ActionInit {
					return
				}
				if plan.Action == ActionShell {
					if plan.TargetDir != tc.expectedTargetDir {
						t.Errorf("expected targetDir %q, got %q", tc.expectedTargetDir, plan.TargetDir)
					}
					return
				}

				if len(plan.Commands) != tc.expectedCmdCount {
					t.Fatalf("expected %d commands, got %d", tc.expectedCmdCount, len(plan.Commands))
				}
				findCmd, ok := plan.Commands[0].(*cmrunner.FindCommand)
				if !ok {
					t.Fatalf("expected cmds[0] to be *cmrunner.FindCommand, got %T", plan.Commands[0])
				}
				if findCmd.TargetPath != tc.expectedTarget {
					t.Errorf("expected target %q, got %q", tc.expectedTarget, findCmd.TargetPath)
				}
				if tc.expectedScanFlags != nil && !reflect.DeepEqual(findCmd.Flags, tc.expectedScanFlags) {
					t.Errorf("expected scan flags %v, got %v", tc.expectedScanFlags, findCmd.Flags)
				}

				if tc.expectedCmdCount > 1 {
					reportCmd, ok := plan.Commands[1].(*cmrunner.ReportCommand)
					if !ok {
						t.Fatalf("expected cmds[1] to be *cmrunner.ReportCommand, got %T", plan.Commands[1])
					}
					if reportCmd.Format != tc.expectedFormat {
						t.Errorf("expected format %q, got %q", tc.expectedFormat, reportCmd.Format)
					}
				}
			}
		})
	}
}

func TestParseFindArgs_CmdIncludesYes(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "src", "auth")
	if err := os.MkdirAll(authDir, 0755); err != nil {
		t.Fatalf("failed to create temp dirs: %v", err)
	}

	plan, err := parseArgs(tmpDir, []string{"find", "src/auth"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Commands) < 1 {
		t.Fatalf("expected at least 1 command, got %d", len(plan.Commands))
	}
	findCmd, ok := plan.Commands[0].(*cmrunner.FindCommand)
	if !ok {
		t.Fatalf("expected plan.Commands[0] to be *cmrunner.FindCommand, got %T", plan.Commands[0])
	}
	expected := []string{"find", "src/auth", "-y"}
	if !reflect.DeepEqual(findCmd.Cmd(), expected) {
		t.Errorf("expected Cmd() %v, got %v", expected, findCmd.Cmd())
	}
}

func TestParseArgs_Fix(t *testing.T) {
	tmpDir := t.TempDir()
	findingFile := filepath.Join(tmpDir, "finding.json")
	sampleJSON := `{"FilePath": "main.go", "Title": "XSS"}`
	if err := os.WriteFile(findingFile, []byte(sampleJSON), 0644); err != nil {
		t.Fatalf("failed to write test finding file: %v", err)
	}

	t.Run("valid file path target", func(t *testing.T) {
		plan, err := parseArgs(tmpDir, []string{"fix", findingFile}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.Action != ActionFix {
			t.Errorf("expected ActionFix, got %v", plan.Action)
		}
		if string(plan.RawFinding) != sampleJSON {
			t.Errorf("expected raw finding %q, got %q", sampleJSON, string(plan.RawFinding))
		}
		if len(plan.PassthroughFlags) != 0 {
			t.Errorf("expected 0 passthrough flags, got %v", plan.PassthroughFlags)
		}
	})

	t.Run("stdin target with data", func(t *testing.T) {
		stdinReader := strings.NewReader(sampleJSON)
		plan, err := parseArgs(tmpDir, []string{"fix", "-"}, stdinReader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.Action != ActionFix {
			t.Errorf("expected ActionFix, got %v", plan.Action)
		}
		if string(plan.RawFinding) != sampleJSON {
			t.Errorf("expected raw finding from stdin %q, got %q", sampleJSON, string(plan.RawFinding))
		}
	})

	t.Run("stdin target with empty stream returns error", func(t *testing.T) {
		stdinReader := strings.NewReader("   ")
		_, err := parseArgs(tmpDir, []string{"fix", "-"}, stdinReader)
		if err == nil {
			t.Error("expected error on empty stdin, got nil")
		}
	})

	t.Run("missing target argument returns error", func(t *testing.T) {
		_, err := parseArgs(tmpDir, []string{"fix"}, nil)
		if err == nil {
			t.Error("expected error on missing fix target argument, got nil")
		}
	})

	t.Run("non-existent file target returns error", func(t *testing.T) {
		_, err := parseArgs(tmpDir, []string{"fix", "nonexistent.json"}, nil)
		if err == nil {
			t.Error("expected error on non-existent file path, got nil")
		}
	})

	t.Run("double dash forwards passthrough flags verbatim", func(t *testing.T) {
		plan, err := parseArgs(tmpDir, []string{"fix", findingFile, "--", "-c", "Sanitize input", "--architecture=3-1"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.Action != ActionFix {
			t.Errorf("expected ActionFix, got %v", plan.Action)
		}
		expectedFlags := []string{"-c", "Sanitize input", "--architecture=3-1"}
		if !reflect.DeepEqual(plan.PassthroughFlags, expectedFlags) {
			t.Errorf("expected passthrough flags %v, got %v", expectedFlags, plan.PassthroughFlags)
		}
	})

	t.Run("help flag returns ActionHelp", func(t *testing.T) {
		plan, err := parseArgs(tmpDir, []string{"fix", "--help"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.Action != ActionHelp {
			t.Errorf("expected ActionHelp, got %v", plan.Action)
		}
	})

	t.Run("redundant cm fix prefix returns error", func(t *testing.T) {
		_, err := parseArgs(tmpDir, []string{"cm", "fix", findingFile}, nil)
		if err == nil {
			t.Error("expected error for 'cm fix', got nil")
		}
	})

	t.Run("nil stdin returns error", func(t *testing.T) {
		_, err := parseArgs(tmpDir, []string{"fix", "-"}, nil)
		if err == nil {
			t.Error("expected error when stdin is nil, got nil")
		}
	})

	t.Run("empty finding file returns error", func(t *testing.T) {
		emptyFinding := filepath.Join(tmpDir, "empty_finding.json")
		_ = os.WriteFile(emptyFinding, []byte("   \n"), 0644)
		_, err := parseArgs(tmpDir, []string{"fix", emptyFinding}, nil)
		if err == nil {
			t.Error("expected error on empty finding file, got nil")
		}
	})

	t.Run("relative path inside workspace resolves correctly", func(t *testing.T) {
		subDir := filepath.Join(tmpDir, "reports")
		_ = os.MkdirAll(subDir, 0755)
		relFile := filepath.Join(subDir, "rel_finding.json")
		_ = os.WriteFile(relFile, []byte(sampleJSON), 0644)

		plan, err := parseArgs(tmpDir, []string{"fix", "reports/rel_finding.json"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(plan.RawFinding) != sampleJSON {
			t.Errorf("expected %q, got %q", sampleJSON, string(plan.RawFinding))
		}
	})

	t.Run("help flag after double dash returns ActionHelp", func(t *testing.T) {
		plan, err := parseArgs(tmpDir, []string{"fix", findingFile, "--", "--help"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.Action != ActionHelp {
			t.Errorf("expected ActionHelp, got %v", plan.Action)
		}
	})
}

func TestParseArgs_FindDiff(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		args         []string
		expectedPlan DispatchPlan
		expectError  bool
	}{
		{
			name: "defaults to HEAD and consolidates base context when empty",
			args: []string{"find-diff"},
			expectedPlan: DispatchPlan{
				Action:           ActionFindDiff,
				GitDiffArgs:      []string{"HEAD"},
				PassthroughFlags: []string{"--context=The target is a Git unified diff for this repository."},
			},
		},
		{
			name: "positional git diff revisions before double-dash",
			args: []string{"find-diff", "origin/main", "HEAD"},
			expectedPlan: DispatchPlan{
				Action:           ActionFindDiff,
				GitDiffArgs:      []string{"origin/main", "HEAD"},
				PassthroughFlags: []string{"--context=The target is a Git unified diff for this repository."},
			},
		},
		{
			name: "triple-dot git revision range",
			args: []string{"find-diff", "origin/main...HEAD"},
			expectedPlan: DispatchPlan{
				Action:           ActionFindDiff,
				GitDiffArgs:      []string{"origin/main...HEAD"},
				PassthroughFlags: []string{"--context=The target is a Git unified diff for this repository."},
			},
		},
		{
			name: "consolidates separated user context flag -c <val>",
			args: []string{"find-diff", "main", "HEAD", "--", "-c", "Check SQL injection", "--model=gemini-1.5-pro"},
			expectedPlan: DispatchPlan{
				Action:      ActionFindDiff,
				GitDiffArgs: []string{"main", "HEAD"},
				PassthroughFlags: []string{
					"--context=The target is a Git unified diff for this repository. Check SQL injection",
					"--model=gemini-1.5-pro",
				},
			},
		},
		{
			name: "consolidates equals user context flag -c=<val>",
			args: []string{"find-diff", "main", "HEAD", "--", "-c=Check SQL injection"},
			expectedPlan: DispatchPlan{
				Action:      ActionFindDiff,
				GitDiffArgs: []string{"main", "HEAD"},
				PassthroughFlags: []string{
					"--context=The target is a Git unified diff for this repository. Check SQL injection",
				},
			},
		},
		{
			name: "consolidates separated user context flag --context <val>",
			args: []string{"find-diff", "--", "--context", "Check XSS", "--unrestricted"},
			expectedPlan: DispatchPlan{
				Action:      ActionFindDiff,
				GitDiffArgs: []string{"HEAD"},
				PassthroughFlags: []string{
					"--context=The target is a Git unified diff for this repository. Check XSS",
					"--unrestricted",
				},
			},
		},
		{
			name: "consolidates equals user context flag --context=<val>",
			args: []string{"find-diff", "--", "--context=Check XSS"},
			expectedPlan: DispatchPlan{
				Action:      ActionFindDiff,
				GitDiffArgs: []string{"HEAD"},
				PassthroughFlags: []string{
					"--context=The target is a Git unified diff for this repository. Check XSS",
				},
			},
		},
		{
			name: "consolidates empty user context flag -c=''",
			args: []string{"find-diff", "--", "-c="},
			expectedPlan: DispatchPlan{
				Action:      ActionFindDiff,
				GitDiffArgs: []string{"HEAD"},
				PassthroughFlags: []string{
					"--context=The target is a Git unified diff for this repository.",
				},
			},
		},
		{
			name: "help flag --help",
			args: []string{"find-diff", "--help"},
			expectedPlan: DispatchPlan{
				Action: ActionHelp,
			},
		},
		{
			name: "help flag -h",
			args: []string{"find-diff", "-h"},
			expectedPlan: DispatchPlan{
				Action: ActionHelp,
			},
		},
		{
			name: "help flag in passthrough flags",
			args: []string{"find-diff", "main", "HEAD", "--", "--help"},
			expectedPlan: DispatchPlan{
				Action: ActionHelp,
			},
		},
		{
			name: "short help flag -h in passthrough flags",
			args: []string{"find-diff", "main", "HEAD", "--", "-h"},
			expectedPlan: DispatchPlan{
				Action: ActionHelp,
			},
		},
		{
			name:        "redundant cm prefix rejected",
			args:        []string{"cm", "find-diff", "origin/main"},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := parseArgs(tmpDir, tc.args, nil)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error for args %v, got plan: %+v", tc.args, plan)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for args %v: %v", tc.args, err)
			}
			if diff := cmp.Diff(tc.expectedPlan, plan); diff != "" {
				t.Errorf("parseArgs(%v) mismatch (-want +got):\n%s", tc.args, diff)
			}
		})
	}
}
