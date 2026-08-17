package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cm-connect/pkg/cmrunner"
)

func TestIsTerminal(t *testing.T) {
	// 1. Non-file reader
	if isTerminal(strings.NewReader("hello")) {
		t.Errorf("expected isTerminal(strings.Reader) to be false")
	}

	// 2. Regular file (not a character device / TTY)
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-file")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer tmpFile.Close()

	if isTerminal(tmpFile) {
		t.Errorf("expected isTerminal(regular file) to be false")
	}
}

func TestPrintUsage(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
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

func TestRun_ShellWithoutTTY(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	code := run([]string{"shell"}, strings.NewReader(""), &stdout, &stderr, tmpDir, "/bin/true")
	if code != cmrunner.ExitUsage {
		t.Errorf("expected exit code %d for shell without TTY, got %d", cmrunner.ExitUsage, code)
	}
	if !strings.Contains(stderr.String(), "'shell' subcommand requires an interactive terminal") {
		t.Errorf("expected TTY error on stderr, got %q", stderr.String())
	}
}

func TestRun_ShellInteractive_Success(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	oldExec := execShellFn
	oldTerm := isTerminalFn
	defer func() {
		execShellFn = oldExec
		isTerminalFn = oldTerm
	}()

	isTerminalFn = func(r io.Reader) bool { return true }
	var executedBinary string
	execShellFn = func(name string, argv []string, envv []string) error {
		executedBinary = name
		return nil
	}

	code := run([]string{"shell"}, strings.NewReader(""), &stdout, &stderr, tmpDir, "/bin/true")
	if code != cmrunner.ExitClean {
		t.Errorf("expected exit code %d for successful shell exec, got %d", cmrunner.ExitClean, code)
	}
	if !strings.Contains(executedBinary, "bash") {
		t.Errorf("expected executed binary to contain bash, got %q", executedBinary)
	}
}

func TestRun_ShellInteractive_ExecError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	oldExec := execShellFn
	oldTerm := isTerminalFn
	defer func() {
		execShellFn = oldExec
		isTerminalFn = oldTerm
	}()

	isTerminalFn = func(r io.Reader) bool { return true }
	execShellFn = func(name string, argv []string, envv []string) error {
		return errors.New("simulated exec failure")
	}

	code := run([]string{"shell"}, strings.NewReader(""), &stdout, &stderr, tmpDir, "/bin/true")
	if code != cmrunner.ExitError {
		t.Errorf("expected exit code %d for exec error, got %d", cmrunner.ExitError, code)
	}
	if !strings.Contains(stderr.String(), "simulated exec failure") {
		t.Errorf("expected exec failure error on stderr, got %q", stderr.String())
	}
}

func TestRun_ShellInteractive_ChdirError(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file-not-dir")
	if err := os.WriteFile(filePath, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	oldExec := execShellFn
	oldTerm := isTerminalFn
	defer func() {
		execShellFn = oldExec
		isTerminalFn = oldTerm
	}()

	isTerminalFn = func(r io.Reader) bool { return true }
	execShellFn = func(name string, argv []string, envv []string) error { return nil }

	var stdout, stderr bytes.Buffer
	code := run([]string{"shell", "file-not-dir"}, strings.NewReader(""), &stdout, &stderr, tmpDir, "/bin/true")
	if code != cmrunner.ExitError {
		t.Errorf("expected ExitError when chdir fails on a file, got %d", code)
	}
	if !strings.Contains(stderr.String(), "failed to change directory") {
		t.Errorf("expected chdir error on stderr, got %q", stderr.String())
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
