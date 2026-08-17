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
		rawArgs        []string
		expectedFormat string
		expectedLeft   []string
		expectedCmd    []string
	}{
		{
			name:           "defaults to json when empty",
			formatArg:      "",
			rawArgs:        nil,
			expectedFormat: "json",
			expectedLeft:   nil,
			expectedCmd:    []string{"report", "--format=json"},
		},
		{
			name:           "custom format sarif via constructor",
			formatArg:      "sarif",
			rawArgs:        nil,
			expectedFormat: "sarif",
			expectedLeft:   nil,
			expectedCmd:    []string{"report", "--format=sarif"},
		},
		{
			name:           "parses --format from raw args and returns unconsumed flags",
			formatArg:      "",
			rawArgs:        []string{"-y", "--format=sarif", "--unrestricted"},
			expectedFormat: "sarif",
			expectedLeft:   []string{"-y", "--unrestricted"},
			expectedCmd:    []string{"report", "--format=sarif"},
		},
		{
			name:           "parses -f with space separation and returns unconsumed flags",
			formatArg:      "",
			rawArgs:        []string{"-y", "-f", "table", "--confidence=80"},
			expectedFormat: "table",
			expectedLeft:   []string{"-y", "--confidence=80"},
			expectedCmd:    []string{"report", "--format=table"},
		},
		{
			name:           "parses -format= and -f= prefixes",
			formatArg:      "",
			rawArgs:        []string{"-format=html", "-f=md"},
			expectedFormat: "md",
			expectedLeft:   nil,
			expectedCmd:    []string{"report", "--format=md"},
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

			if len(tc.rawArgs) > 0 {
				leftover, err := cmd.SetArgs(tc.rawArgs...)
				if err != nil {
					t.Fatalf("unexpected error from SetArgs: %v", err)
				}
				if !reflect.DeepEqual(leftover, tc.expectedLeft) {
					t.Errorf("expected leftover %v, got %v", tc.expectedLeft, leftover)
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

func TestReportCommand_SetArgs_MissingArg(t *testing.T) {
	cmd := NewReportCommand()
	_, err := cmd.SetArgs("--format")
	if err == nil {
		t.Errorf("expected error for trailing --format without value, got nil")
	}
}
