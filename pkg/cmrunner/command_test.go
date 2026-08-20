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
			flags:        []string{"-y", "--unrestricted"},
			expectedCmd:  []string{"find", "src/auth", "-y", "--unrestricted"},
			expectedPath: "src/auth",
		},
		{
			name:         "injects -y when custom scan flags provided without -y",
			target:       "src/auth",
			flags:        []string{"-c", "5", "--unrestricted"},
			expectedCmd:  []string{"find", "src/auth", "-y", "-c", "5", "--unrestricted"},
			expectedPath: "src/auth",
		},
		{
			name:         "does not inject -y when help flag is requested",
			target:       "src/auth",
			flags:        []string{"--help"},
			expectedCmd:  []string{"find", "src/auth", "--help"},
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
