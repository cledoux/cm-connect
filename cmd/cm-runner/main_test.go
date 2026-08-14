package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintUsage(t *testing.T) {
	var buf bytes.Buffer
	PrintUsage(&buf)
	out := buf.String()

	if !strings.Contains(out, "cm-runner find") {
		t.Errorf("expected usage to contain 'cm-runner find', got %q", out)
	}
	if !strings.Contains(out, "cm-runner shell") {
		t.Errorf("expected usage to contain 'cm-runner shell', got %q", out)
	}
	if !strings.Contains(out, "Exit Codes:") {
		t.Errorf("expected usage to contain 'Exit Codes:', got %q", out)
	}
}

func TestRun_EmptyArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	code := Run([]string{}, os.Stdin, &stdout, &stderr, tmpDir, "/bin/true", "/bin/bash")
	if code != ExitUsage {
		t.Errorf("expected exit code %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr.String(), "missing subcommand") {
		t.Errorf("expected missing subcommand error on stderr, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("expected usage on stderr, got %q", stderr.String())
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	code := Run([]string{"unknown-cmd"}, os.Stdin, &stdout, &stderr, tmpDir, "/bin/true", "/bin/bash")
	if code != ExitUsage {
		t.Errorf("expected exit code %d, got %d", ExitUsage, code)
	}
	if !strings.Contains(stderr.String(), "unrecognized subcommand 'unknown-cmd'") {
		t.Errorf("expected unrecognized subcommand error on stderr, got %q", stderr.String())
	}
}

func TestRun_FindNonExistentPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	code := Run([]string{"find", "missing/path"}, os.Stdin, &stdout, &stderr, tmpDir, "/bin/true", "/bin/bash")
	if code != ExitUsage {
		t.Errorf("expected exit code %d, got %d", ExitUsage, code)
	}
	expectedErr := "scan target path 'missing/path' does not exist in " + tmpDir
	if !strings.Contains(stderr.String(), expectedErr) {
		t.Errorf("expected stderr to contain %q, got %q", expectedErr, stderr.String())
	}
}

func TestRun_FindExecution(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src", "auth")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to make test dir: %v", err)
	}

	// Create a mock cm script that echoes received arguments
	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
echo "args: $@"
exit 0
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"find", "src/auth"}, os.Stdin, &stdout, &stderr, tmpDir, mockCM, "/bin/bash")
	if code != ExitClean {
		t.Fatalf("expected exit code 0, got %d (stderr: %q)", code, stderr.String())
	}

	out := stdout.String()
	// Should have executed "find src/auth --format json"
	if !strings.Contains(out, "find src/auth --format json") {
		t.Errorf("expected output to contain forwarded args 'find src/auth --format json', got %q", out)
	}
}

func TestRun_ShellWithoutTTY(t *testing.T) {
	tmpDir := t.TempDir()
	var stdout, stderr bytes.Buffer

	// os.Stdin in test runner is not an interactive terminal
	code := Run([]string{"shell"}, os.Stdin, &stdout, &stderr, tmpDir, "/bin/true", "/bin/bash")
	if code != ExitUsage {
		t.Errorf("expected exit code %d, got %d", ExitUsage, code)
	}
	expectedErr := "Error: 'shell' subcommand requires an interactive terminal. Please run with 'docker run -it <image> shell'"
	if !strings.Contains(stderr.String(), expectedErr) {
		t.Errorf("expected stderr to contain %q, got %q", expectedErr, stderr.String())
	}
}
