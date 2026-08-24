package cmrunner

import (
	"reflect"
	"testing"
)

func TestNewFindCommand(t *testing.T) {
	tests := []struct {
		name         string
		target       string
		flags        []string
		expectedCmd  []string
		expectedPath string
	}{
		{
			name:         "defaults to dot and injects -y when empty",
			target:       "",
			flags:        nil,
			expectedCmd:  []string{"find", ".", "-y"},
			expectedPath: ".",
		},
		{
			name:         "preserves target path and does not duplicate -y flag",
			target:       "src/auth",
			flags:        []string{"-y"},
			expectedCmd:  []string{"find", "src/auth", "-y"},
			expectedPath: "src/auth",
		},
		{
			name:         "preserves target path and does not duplicate --yes flag",
			target:       "src/auth",
			flags:        []string{"--yes"},
			expectedCmd:  []string{"find", "src/auth", "--yes"},
			expectedPath: "src/auth",
		},
		{
			name:         "injects -y when custom scan flags provided without it",
			target:       "src/auth",
			flags:        []string{"-c", "5"},
			expectedCmd:  []string{"find", "src/auth", "-y", "-c", "5"},
			expectedPath: "src/auth",
		},
		{
			name:         "does not inject -y when help flag is requested",
			target:       "src/auth",
			flags:        []string{"--help"},
			expectedCmd:  []string{"find", "src/auth", "--help"},
			expectedPath: "src/auth",
		},
		{
			name:         "does not inject -y when short help flag -h is requested",
			target:       "src/auth",
			flags:        []string{"-h"},
			expectedCmd:  []string{"find", "src/auth", "-h"},
			expectedPath: "src/auth",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewFindCommand(tc.target)
			if cmd.TargetPath != tc.expectedPath {
				t.Errorf("expected TargetPath %q, got %q", tc.expectedPath, cmd.TargetPath)
			}
			if len(tc.flags) > 0 {
				leftover, err := cmd.SetArgs(tc.flags...)
				if err != nil {
					t.Fatalf("unexpected error from SetArgs: %v", err)
				}
				if len(leftover) != 0 {
					t.Errorf("expected 0 leftover args, got %v", leftover)
				}
			}
			if !reflect.DeepEqual(cmd.Cmd(), tc.expectedCmd) {
				t.Errorf("expected Cmd() %v, got %v", tc.expectedCmd, cmd.Cmd())
			}
		})
	}
}

func TestFindCommand_EmptyTargetPath_DefaultsToDot(t *testing.T) {
	cmd := &FindCommand{TargetPath: ""}
	expected := []string{"find", ".", "-y"}
	if !reflect.DeepEqual(cmd.Cmd(), expected) {
		t.Errorf("expected %v, got %v", expected, cmd.Cmd())
	}
}

func TestNewReportCommand(t *testing.T) {
	tests := []struct {
		name           string
		formatArg      string
		flags          []string
		expectedFormat string
		expectedCmd    []string
	}{
		{
			name:           "defaults to json when empty",
			formatArg:      "",
			expectedFormat: "json",
			expectedCmd:    []string{"report", "--format=json"},
		},
		{
			name:           "custom format sarif via constructor",
			formatArg:      "sarif",
			expectedFormat: "sarif",
			expectedCmd:    []string{"report", "--format=sarif"},
		},
		{
			name:           "with extra flags via SetArgs",
			formatArg:      "json",
			flags:          []string{"--quiet"},
			expectedFormat: "json",
			expectedCmd:    []string{"report", "--format=json", "--quiet"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cmd *ReportCommand
			if tc.formatArg != "" {
				cmd = NewReportCommand(tc.formatArg)
			} else {
				cmd = NewReportCommand()
			}

			if len(tc.flags) > 0 {
				leftover, err := cmd.SetArgs(tc.flags...)
				if err != nil {
					t.Fatalf("unexpected error from SetArgs: %v", err)
				}
				if len(leftover) != 0 {
					t.Errorf("expected 0 leftover args, got %v", leftover)
				}
			}

			if cmd.Format != tc.expectedFormat {
				t.Errorf("expected Format %q, got %q", tc.expectedFormat, cmd.Format)
			}
			if !reflect.DeepEqual(cmd.Cmd(), tc.expectedCmd) {
				t.Errorf("expected Cmd() %v, got %v", tc.expectedCmd, cmd.Cmd())
			}
		})
	}
}

func TestReportCommand_EmptyFormat_DefaultsToJson(t *testing.T) {
	cmd := &ReportCommand{Format: ""}
	expected := []string{"report", "--format=json"}
	if !reflect.DeepEqual(cmd.Cmd(), expected) {
		t.Errorf("expected %v, got %v", expected, cmd.Cmd())
	}
}

func TestNewFixCommand(t *testing.T) {
	tests := []struct {
		name        string
		findingID   string
		flags       []string
		expectedCmd []string
	}{
		{
			name:        "defaults to inject -y and --unrestricted",
			findingID:   "uuid-1234",
			flags:       nil,
			expectedCmd: []string{"fix", "uuid-1234", "-y", "--unrestricted"},
		},
		{
			name:        "forwards passthrough flags verbatim after headless defaults",
			findingID:   "uuid-1234",
			flags:       []string{"-c", "Sanitize input", "--architecture=3-1"},
			expectedCmd: []string{"fix", "uuid-1234", "-y", "--unrestricted", "-c", "Sanitize input", "--architecture=3-1"},
		},
		{
			name:        "does not duplicate -y or --unrestricted when already present in flags",
			findingID:   "uuid-5678",
			flags:       []string{"-y", "--unrestricted", "--model=gemini"},
			expectedCmd: []string{"fix", "uuid-5678", "-y", "--unrestricted", "--model=gemini"},
		},
		{
			name:        "empty finding id defaults to unknown",
			findingID:   "",
			flags:       nil,
			expectedCmd: []string{"fix", "unknown", "-y", "--unrestricted"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewFixCommand(tc.findingID)
			if len(tc.flags) > 0 {
				leftover, err := cmd.SetArgs(tc.flags...)
				if err != nil {
					t.Fatalf("unexpected error from SetArgs: %v", err)
				}
				if len(leftover) != 0 {
					t.Errorf("expected 0 leftover args, got %v", leftover)
				}
			}
			if !reflect.DeepEqual(cmd.Cmd(), tc.expectedCmd) {
				t.Errorf("expected Cmd() %v, got %v", tc.expectedCmd, cmd.Cmd())
			}
		})
	}
}

func TestNewImportCommand(t *testing.T) {
	tests := []struct {
		name         string
		importFile   string
		workspaceDir string
		expectedCmd  []string
	}{
		{
			name:         "standard import parameters",
			importFile:   "/tmp/cm-import.json",
			workspaceDir: "/workspace",
			expectedCmd:  []string{"report", "import", "-f", "/tmp/cm-import.json", "-p", "/workspace"},
		},
		{
			name:         "empty parameters default to standard paths",
			importFile:   "",
			workspaceDir: "",
			expectedCmd:  []string{"report", "import", "-f", "/tmp/cm-import.json", "-p", "/workspace"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewImportCommand(tc.importFile, tc.workspaceDir)
			if !reflect.DeepEqual(cmd.Cmd(), tc.expectedCmd) {
				t.Errorf("expected Cmd() %v, got %v", tc.expectedCmd, cmd.Cmd())
			}
		})
	}

	t.Run("empty struct Cmd defaults", func(t *testing.T) {
		cmd := &ImportCommand{}
		expected := []string{"report", "import", "-f", "/tmp/cm-import.json", "-p", "/workspace"}
		if !reflect.DeepEqual(cmd.Cmd(), expected) {
			t.Errorf("expected %v, got %v", expected, cmd.Cmd())
		}
	})
}

func TestConsolidateContext(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "nil flags produces default base context",
			input:    nil,
			expected: []string{"--context=" + BaseDiffContext},
		},
		{
			name:     "empty flags produces default base context",
			input:    []string{},
			expected: []string{"--context=" + BaseDiffContext},
		},
		{
			name:     "flags without context preserves flags and prepends base context",
			input:    []string{"--model=gemini-1.5-pro", "--verbose"},
			expected: []string{"--context=" + BaseDiffContext, "--model=gemini-1.5-pro", "--verbose"},
		},
		{
			name:     "extracts -c with space and merges with base context",
			input:    []string{"-c", "Focus on SQL injection", "--model=gemini"},
			expected: []string{"--context=" + BaseDiffContext + " Focus on SQL injection", "--model=gemini"},
		},
		{
			name:     "extracts -c with equal sign and merges with base context",
			input:    []string{"-c=Focus on SQL injection", "--model=gemini"},
			expected: []string{"--context=" + BaseDiffContext + " Focus on SQL injection", "--model=gemini"},
		},
		{
			name:     "extracts --context with space and merges with base context",
			input:    []string{"--context", "Check auth flaws", "--architecture=3-1"},
			expected: []string{"--context=" + BaseDiffContext + " Check auth flaws", "--architecture=3-1"},
		},
		{
			name:     "extracts --context with equal sign and merges with base context",
			input:    []string{"--context=Check auth flaws", "--architecture=3-1"},
			expected: []string{"--context=" + BaseDiffContext + " Check auth flaws", "--architecture=3-1"},
		},
		{
			name:     "multiple context flags merged sequentially",
			input:    []string{"-c", "Focus on auth", "--context=Check SQL injection"},
			expected: []string{"--context=" + BaseDiffContext + " Focus on auth Check SQL injection"},
		},
		{
			name:     "trailing -c flag without value is stripped",
			input:    []string{"--model=gemini", "-c"},
			expected: []string{"--context=" + BaseDiffContext, "--model=gemini"},
		},
		{
			name:     "trailing --context flag without value is stripped",
			input:    []string{"--model=gemini", "--context"},
			expected: []string{"--context=" + BaseDiffContext, "--model=gemini"},
		},
		{
			name:     "empty user context value does not add trailing whitespace",
			input:    []string{"-c", "", "--model=gemini"},
			expected: []string{"--context=" + BaseDiffContext, "--model=gemini"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ConsolidateContext(tc.input)
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestNewFindDiffCommand(t *testing.T) {
	tests := []struct {
		name         string
		diffPath     string
		flags        []string
		expectedCmd  []string
		expectedPath string
	}{
		{
			name:         "defaults diff path and injects -y and consolidated base context when empty",
			diffPath:     "",
			flags:        nil,
			expectedCmd:  []string{"find", DefaultDiffPath, "-y", "--context=" + BaseDiffContext},
			expectedPath: DefaultDiffPath,
		},
		{
			name:         "preserves custom diff path and injects -y and base context",
			diffPath:     "/workspace/custom.diff",
			flags:        nil,
			expectedCmd:  []string{"find", "/workspace/custom.diff", "-y", "--context=" + BaseDiffContext},
			expectedPath: "/workspace/custom.diff",
		},
		{
			name:         "consolidates user context and appends scan flags",
			diffPath:     "/tmp/cm-diff.diff",
			flags:        []string{"-c", "Focus on auth", "--model=gemini"},
			expectedCmd:  []string{"find", "/tmp/cm-diff.diff", "-y", "--context=" + BaseDiffContext + " Focus on auth", "--model=gemini"},
			expectedPath: "/tmp/cm-diff.diff",
		},
		{
			name:         "does not duplicate -y when -y flag provided",
			diffPath:     "/tmp/cm-diff.diff",
			flags:        []string{"-y", "--model=gemini"},
			expectedCmd:  []string{"find", "/tmp/cm-diff.diff", "-y", "--context=" + BaseDiffContext, "--model=gemini"},
			expectedPath: "/tmp/cm-diff.diff",
		},
		{
			name:         "does not duplicate -y when --yes flag provided",
			diffPath:     "/tmp/cm-diff.diff",
			flags:        []string{"--yes", "--model=gemini"},
			expectedCmd:  []string{"find", "/tmp/cm-diff.diff", "--yes", "--context=" + BaseDiffContext, "--model=gemini"},
			expectedPath: "/tmp/cm-diff.diff",
		},
		{
			name:         "does not inject -y when help flag is requested",
			diffPath:     "/tmp/cm-diff.diff",
			flags:        []string{"--help"},
			expectedCmd:  []string{"find", "/tmp/cm-diff.diff", "--help"},
			expectedPath: "/tmp/cm-diff.diff",
		},
		{
			name:         "does not inject -y when short help flag -h is requested",
			diffPath:     "/tmp/cm-diff.diff",
			flags:        []string{"-h"},
			expectedCmd:  []string{"find", "/tmp/cm-diff.diff", "-h"},
			expectedPath: "/tmp/cm-diff.diff",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewFindDiffCommand(tc.diffPath)
			if cmd.DiffPath != tc.expectedPath {
				t.Errorf("expected DiffPath %q, got %q", tc.expectedPath, cmd.DiffPath)
			}
			if len(tc.flags) > 0 {
				leftover, err := cmd.SetArgs(tc.flags...)
				if err != nil {
					t.Fatalf("unexpected error from SetArgs: %v", err)
				}
				if len(leftover) != 0 {
					t.Errorf("expected 0 leftover args, got %v", leftover)
				}
			}
			if !reflect.DeepEqual(cmd.Cmd(), tc.expectedCmd) {
				t.Errorf("expected Cmd() %v, got %v", tc.expectedCmd, cmd.Cmd())
			}
		})
	}

	t.Run("empty struct Cmd defaults", func(t *testing.T) {
		cmd := &FindDiffCommand{}
		expected := []string{"find", DefaultDiffPath, "-y", "--context=" + BaseDiffContext}
		if !reflect.DeepEqual(cmd.Cmd(), expected) {
			t.Errorf("expected %v, got %v", expected, cmd.Cmd())
		}
	})

	t.Run("constructor with initial flags", func(t *testing.T) {
		cmd := NewFindDiffCommand("/tmp/cm-diff.diff", "--model=gemini")
		expected := []string{"find", "/tmp/cm-diff.diff", "-y", "--context=" + BaseDiffContext, "--model=gemini"}
		if !reflect.DeepEqual(cmd.Cmd(), expected) {
			t.Errorf("expected %v, got %v", expected, cmd.Cmd())
		}
	})
}

