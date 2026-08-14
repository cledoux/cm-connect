package cmrunner

import (
	"reflect"
	"testing"
)

func TestNewFindCommand(t *testing.T) {
	// Empty target path defaults to "."
	cmd1 := NewFindCommand("")
	if cmd1.TargetPath != "." {
		t.Errorf("expected target '.', got %q", cmd1.TargetPath)
	}

	// Explicit target path preserved
	cmd2 := NewFindCommand("src/auth", "--model", "vertex:gemini")
	if cmd2.TargetPath != "src/auth" {
		t.Errorf("expected target 'src/auth', got %q", cmd2.TargetPath)
	}
	if len(cmd2.Flags) != 2 {
		t.Errorf("expected 2 flags, got %v", cmd2.Flags)
	}
}

func TestFindCommand_HasFormatFlag(t *testing.T) {
	tests := []struct {
		name     string
		flags    []string
		expected bool
	}{
		{"no flags", []string{}, false},
		{"unrelated flags", []string{"--verbose", "--model", "gemini"}, false},
		{"long format flag", []string{"--format", "json"}, true},
		{"long format flag equal", []string{"--format=sarif"}, true},
		{"short format flag", []string{"-f", "text"}, true},
		{"short format flag equal", []string{"-f=json"}, true},
		{"long help flag", []string{"--help"}, true},
		{"short help flag", []string{"-h"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewFindCommand("src/auth", tc.flags...)
			if cmd.HasFormatFlag() != tc.expected {
				t.Errorf("expected HasFormatFlag() == %v, got %v for flags %v", tc.expected, cmd.HasFormatFlag(), tc.flags)
			}
		})
	}
}

func TestFindCommand_Args(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		flags    []string
		expected []string
	}{
		{
			name:     "defaults to '.' and injects '--format json'",
			target:   "",
			flags:    []string{},
			expected: []string{"find", ".", "--format", "json"},
		},
		{
			name:     "preserves target path and injects '--format json'",
			target:   "src/auth",
			flags:    []string{"--model", "vertex:gemini"},
			expected: []string{"find", "src/auth", "--model", "vertex:gemini", "--format", "json"},
		},
		{
			name:     "does not inject format when --format=sarif is present",
			target:   "pkg/api",
			flags:    []string{"--format=sarif"},
			expected: []string{"find", "pkg/api", "--format=sarif"},
		},
		{
			name:     "does not inject format when -f is present",
			target:   ".",
			flags:    []string{"-f", "text"},
			expected: []string{"find", ".", "-f", "text"},
		},
		{
			name:     "does not inject format when --help is present",
			target:   ".",
			flags:    []string{"--help"},
			expected: []string{"find", ".", "--help"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewFindCommand(tc.target, tc.flags...)
			args := cmd.Args()
			if !reflect.DeepEqual(args, tc.expected) {
				t.Errorf("expected args %v, got %v", tc.expected, args)
			}
		})
	}
}
