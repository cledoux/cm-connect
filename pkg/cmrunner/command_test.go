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
			name:         "defaults to dot and injects --sandbox=false and -y when empty",
			target:       "",
			flags:        nil,
			expectedCmd:  []string{"--sandbox=false", "find", ".", "-y"},
			expectedPath: ".",
		},
		{
			name:         "preserves target path and does not duplicate -y flag or --sandbox flag",
			target:       "src/auth",
			flags:        []string{"-y", "--sandbox=false"},
			expectedCmd:  []string{"find", "src/auth", "-y", "--sandbox=false"},
			expectedPath: "src/auth",
		},
		{
			name:         "injects --sandbox=false and -y when custom scan flags provided without them",
			target:       "src/auth",
			flags:        []string{"-c", "5"},
			expectedCmd:  []string{"--sandbox=false", "find", "src/auth", "-y", "-c", "5"},
			expectedPath: "src/auth",
		},
		{
			name:         "injects -y when --sandbox=false is already provided",
			target:       "src/auth",
			flags:        []string{"--sandbox=false", "-c", "5"},
			expectedCmd:  []string{"find", "src/auth", "-y", "--sandbox=false", "-c", "5"},
			expectedPath: "src/auth",
		},
		{
			name:         "injects -y when --sandbox=true is already provided",
			target:       "src/auth",
			flags:        []string{"--sandbox=true", "-c", "5"},
			expectedCmd:  []string{"find", "src/auth", "-y", "--sandbox=true", "-c", "5"},
			expectedPath: "src/auth",
		},
		{
			name:         "injects --sandbox=false when -y is already provided",
			target:       "src/auth",
			flags:        []string{"-y", "-c", "5"},
			expectedCmd:  []string{"--sandbox=false", "find", "src/auth", "-y", "-c", "5"},
			expectedPath: "src/auth",
		},
		{
			name:         "injects --sandbox=false when --yes is already provided",
			target:       "src/auth",
			flags:        []string{"--yes", "-c", "5"},
			expectedCmd:  []string{"--sandbox=false", "find", "src/auth", "--yes", "-c", "5"},
			expectedPath: "src/auth",
		},
		{
			name:         "does not inject --sandbox=false or -y when help flag is requested",
			target:       "src/auth",
			flags:        []string{"--help"},
			expectedCmd:  []string{"find", "src/auth", "--help"},
			expectedPath: "src/auth",
		},
		{
			name:         "does not inject --sandbox=false or -y when short help flag -h is requested",
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
	expected := []string{"--sandbox=false", "find", ".", "-y"}
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
