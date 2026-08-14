package main

import (
	"strings"
	"testing"
)

func TestHasFormatFlag(t *testing.T) {
	tests := []struct {
		name     string
		flags    []string
		expected bool
	}{
		{
			name:     "empty flags",
			flags:    []string{},
			expected: false,
		},
		{
			name:     "unrelated flags",
			flags:    []string{"--model", "gemini-1.5-pro", "--verbose"},
			expected: false,
		},
		{
			name:     "long format flag space separated",
			flags:    []string{"--format", "text"},
			expected: true,
		},
		{
			name:     "long format flag equal separated",
			flags:    []string{"--format=sarif"},
			expected: true,
		},
		{
			name:     "short format flag space separated",
			flags:    []string{"-f", "json"},
			expected: true,
		},
		{
			name:     "short format flag equal separated",
			flags:    []string{"-f=json"},
			expected: true,
		},
		{
			name:     "format flag embedded with other flags",
			flags:    []string{"--verbose", "-f", "text", "--model", "foo"},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := HasFormatFlag(tc.flags)
			if result != tc.expected {
				t.Errorf("expected %v, got %v for flags %v", tc.expected, result, tc.flags)
			}
		})
	}
}

func TestInjectFormatFlags(t *testing.T) {
	tests := []struct {
		name     string
		flags    []string
		expected []string
	}{
		{
			name:     "inject into empty flags",
			flags:    []string{},
			expected: []string{"--format", "json"},
		},
		{
			name:     "inject into flags without format",
			flags:    []string{"--model", "vertex:gemini-1.5-pro"},
			expected: []string{"--model", "vertex:gemini-1.5-pro", "--format", "json"},
		},
		{
			name:     "do not inject when --format text exists",
			flags:    []string{"--format", "text"},
			expected: []string{"--format", "text"},
		},
		{
			name:     "do not inject when --format=sarif exists",
			flags:    []string{"--format=sarif"},
			expected: []string{"--format=sarif"},
		},
		{
			name:     "do not inject when -f sarif exists",
			flags:    []string{"-f", "sarif"},
			expected: []string{"-f", "sarif"},
		},
		{
			name:     "do not inject when -f=json exists",
			flags:    []string{"-f=json"},
			expected: []string{"-f=json"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := InjectFormatFlags(tc.flags)
			if len(result) != len(tc.expected) {
				t.Fatalf("expected len %d (%v), got len %d (%v)", len(tc.expected), tc.expected, len(result), result)
			}
			for i := range result {
				if result[i] != tc.expected[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tc.expected[i], result[i])
				}
			}
		})
	}
}

func TestConfigureEnvironment(t *testing.T) {
	baseEnv := []string{
		"PATH=/usr/local/bin:/usr/bin",
		"USER=codemender",
		"CLOUDSDK_AUTH_ACCESS_TOKEN=test-token-123",
		"GOOGLE_APPLICATION_CREDENTIALS=/auth/key.json",
		"NO_COLOR=0",
		"TERM=xterm-256color",
	}

	configured := ConfigureEnvironment(baseEnv)

	envMap := make(map[string]string)
	for _, env := range configured {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Verify required defaults
	if envMap["NO_COLOR"] != "1" {
		t.Errorf("expected NO_COLOR=1, got %q", envMap["NO_COLOR"])
	}
	if envMap["TERM"] != "dumb" {
		t.Errorf("expected TERM=dumb, got %q", envMap["TERM"])
	}

	// Verify preservation of other variables
	if envMap["PATH"] != "/usr/local/bin:/usr/bin" {
		t.Errorf("expected PATH preserved, got %q", envMap["PATH"])
	}
	if envMap["USER"] != "codemender" {
		t.Errorf("expected USER=codemender preserved, got %q", envMap["USER"])
	}
	if envMap["CLOUDSDK_AUTH_ACCESS_TOKEN"] != "test-token-123" {
		t.Errorf("expected CLOUDSDK_AUTH_ACCESS_TOKEN preserved, got %q", envMap["CLOUDSDK_AUTH_ACCESS_TOKEN"])
	}
	if envMap["GOOGLE_APPLICATION_CREDENTIALS"] != "/auth/key.json" {
		t.Errorf("expected GOOGLE_APPLICATION_CREDENTIALS preserved, got %q", envMap["GOOGLE_APPLICATION_CREDENTIALS"])
	}
}
