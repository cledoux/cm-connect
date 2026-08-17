package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cm-connect/pkg/cmrunner"
)

func TestPrintUsage(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()

	if !strings.Contains(out, "cm-runner find") {
		t.Errorf("expected usage to contain 'cm-runner find', got %q", out)
	}
	if !strings.Contains(out, "Exit Codes:") {
		t.Errorf("expected usage to contain 'Exit Codes:', got %q", out)
	}
}

func TestRun_EmptyArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	code := run([]string{}, strings.NewReader(""), &stdout, &stderr, tmpDir, "/bin/true")
	if code != cmrunner.ExitUsage {
		t.Errorf("expected exit code %d, got %d", cmrunner.ExitUsage, code)
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

	code := run([]string{"unknown-cmd"}, strings.NewReader(""), &stdout, &stderr, tmpDir, "/bin/true")
	if code != cmrunner.ExitUsage {
		t.Errorf("expected exit code %d, got %d", cmrunner.ExitUsage, code)
	}
	if !strings.Contains(stderr.String(), "unrecognized subcommand") {
		t.Errorf("expected unrecognized subcommand error on stderr, got %q", stderr.String())
	}
}

func TestRun_FindNonExistentPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	code := run([]string{"find", "missing/path"}, strings.NewReader(""), &stdout, &stderr, tmpDir, "/bin/true")
	if code != cmrunner.ExitUsage {
		t.Errorf("expected exit code %d, got %d", cmrunner.ExitUsage, code)
	}
	if !strings.Contains(stderr.String(), "scan target path does not exist in workspace") {
		t.Errorf("expected path error on stderr, got %q", stderr.String())
	}
}

func TestRun_FindExecution_TwoPhase(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src", "auth")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to make test dir: %v", err)
	}

	// Create a mock cm script that handles find and report
	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "find" ]; then
    echo "find progress on stderr" >&2
    exit 0
elif [ "$1" = "report" ]; then
    echo '[{"FindingID":"vuln1","Title":"SQL Injection"}]'
    echo "report log on stderr" >&2
    exit 0
fi
exit 2
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"find", "src/auth"}, strings.NewReader(""), &stdout, &stderr, tmpDir, mockCM)
	if code != cmrunner.ExitFindings {
		t.Fatalf("expected exit code 1 (ExitFindings), got %d (stderr: %q)", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "SQL Injection") {
		t.Errorf("expected findings JSON on stdout, got %q", out)
	}
	if !strings.Contains(stderr.String(), "find progress on stderr") {
		t.Errorf("expected find progress on stderr, got %q", stderr.String())
	}
}
