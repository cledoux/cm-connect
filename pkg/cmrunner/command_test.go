package cmrunner

import (
	"reflect"
	"testing"
)

func TestNewFindCommand_Classification(t *testing.T) {
	// Empty target path defaults to "." and format defaults to "json"
	cmd1 := NewFindCommand("")
	if cmd1.TargetPath != "." {
		t.Errorf("expected target '.', got %q", cmd1.TargetPath)
	}
	if cmd1.ReportFormat != "json" {
		t.Errorf("expected default report format 'json', got %q", cmd1.ReportFormat)
	}

	// Extracts format flag and forwards scanner flags
	cmd2 := NewFindCommand("src/auth", "-y", "--format", "sarif", "-c", "auth focus", "--unrestricted")
	if cmd2.TargetPath != "src/auth" {
		t.Errorf("expected target 'src/auth', got %q", cmd2.TargetPath)
	}
	if cmd2.ReportFormat != "sarif" {
		t.Errorf("expected format 'sarif', got %q", cmd2.ReportFormat)
	}

	expectedScanFlags := []string{"-y", "-c", "auth focus", "--unrestricted"}
	if !reflect.DeepEqual(cmd2.ScanFlags, expectedScanFlags) {
		t.Errorf("expected scan flags %v, got %v", expectedScanFlags, cmd2.ScanFlags)
	}
}

func TestFindCommand_FormatFlags(t *testing.T) {
	tests := []struct {
		name           string
		flags          []string
		expectedFormat string
	}{
		{"default format", []string{}, "json"},
		{"long format with space", []string{"--format", "sarif"}, "sarif"},
		{"long format with equal", []string{"--format=html"}, "html"},
		{"short format with space", []string{"-f", "table"}, "table"},
		{"short format with equal", []string{"-f=md"}, "md"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewFindCommand("src/auth", tc.flags...)
			if cmd.ReportFormat != tc.expectedFormat {
				t.Errorf("expected format %q, got %q", tc.expectedFormat, cmd.ReportFormat)
			}
		})
	}
}

func TestFindCommand_FindArgs(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		flags    []string
		expected []string
	}{
		{
			name:     "defaults to '.' with no scan flags",
			target:   "",
			flags:    []string{"--format", "json"},
			expected: []string{"find", "."},
		},
		{
			name:     "forwards scanner flags and preserves target",
			target:   "src/auth",
			flags:    []string{"-y", "--unrestricted", "-c", "check sqli", "-f", "sarif"},
			expected: []string{"find", "src/auth", "-y", "--unrestricted", "-c", "check sqli"},
		},
		{
			name:     "handles help flag",
			target:   ".",
			flags:    []string{"--help"},
			expected: []string{"find", "--help"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewFindCommand(tc.target, tc.flags...)
			args := cmd.FindArgs()
			if !reflect.DeepEqual(args, tc.expected) {
				t.Errorf("expected FindArgs %v, got %v", tc.expected, args)
			}
		})
	}
}

func TestFindCommand_ReportArgs(t *testing.T) {
	tests := []struct {
		name     string
		flags    []string
		expected []string
	}{
		{
			name:     "defaults to --format=json",
			flags:    []string{},
			expected: []string{"report", "--format=json"},
		},
		{
			name:     "custom format flag",
			flags:    []string{"-f", "sarif"},
			expected: []string{"report", "--format=sarif"},
		},
		{
			name:     "custom format flag with equals",
			flags:    []string{"--format=html"},
			expected: []string{"report", "--format=html"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewFindCommand(".", tc.flags...)
			args := cmd.ReportArgs()
			if !reflect.DeepEqual(args, tc.expected) {
				t.Errorf("expected ReportArgs %v, got %v", tc.expected, args)
			}
		})
	}
}
