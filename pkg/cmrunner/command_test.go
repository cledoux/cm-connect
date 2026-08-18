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
			name:         "defaults to dot when empty",
			target:       "",
			flags:        nil,
			expectedCmd:  []string{"find", "."},
			expectedPath: ".",
		},
		{
			name:         "preserves target path and sets flags via SetArgs",
			target:       "src/auth",
			flags:        []string{"-y", "--unrestricted"},
			expectedCmd:  []string{"find", "src/auth", "-y", "--unrestricted"},
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
	expected := []string{"find", "."}
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
