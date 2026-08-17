package cmrunner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewRunner_DefaultsAndOptions(t *testing.T) {
	r := NewRunner(
		WithExecutable("/custom/cm"),
		WithWorkspace("/custom/workspace"),
		WithEnv([]string{"TEST_ENV=1"}),
	)

	if r.Executable != "/custom/cm" {
		t.Errorf("expected executable /custom/cm, got %q", r.Executable)
	}
	if r.Workspace != "/custom/workspace" {
		t.Errorf("expected workspace /custom/workspace, got %q", r.Workspace)
	}
	if len(r.Env) != 1 || r.Env[0] != "TEST_ENV=1" {
		t.Errorf("expected custom env, got %v", r.Env)
	}
}

func TestResolveExecutable(t *testing.T) {
	// 1. Existing binary
	resolved := resolveExecutable("/bin/sh", "sh")
	if resolved != "/bin/sh" {
		t.Errorf("expected /bin/sh, got %q", resolved)
	}

	// 2. Non-existent preferred binary falling back to PATH
	resolved = resolveExecutable("/non/existent/path/sh", "sh")
	if !strings.HasSuffix(resolved, "sh") {
		t.Errorf("expected resolved binary name to end with sh, got %q", resolved)
	}

	// 3. Fallback fails completely
	resolved = resolveExecutable("/non/existent/path/cm", "non_existent_binary_xyz")
	if resolved != "/non/existent/path/cm" {
		t.Errorf("expected original path fallback, got %q", resolved)
	}
}

func TestRunner_Run_NilCommand(t *testing.T) {
	runner := NewRunner()
	ctx := context.Background()

	code, err := runner.Run(ctx, nil, nil, nil, nil)
	if code != ExitUsage {
		t.Errorf("expected ExitUsage (2), got %d", code)
	}
	if err == nil {
		t.Errorf("expected error for nil command, got nil")
	}
}

func TestRunner_Run_SingleCommand(t *testing.T) {
	tmpDir := t.TempDir()
	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
echo "single-cmd-output"
exit 0
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(tmpDir),
	)

	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	cmd := NewFindCommand(".")

	code, err := runner.Run(ctx, cmd, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != ExitClean {
		t.Errorf("expected ExitClean (0), got %d", code)
	}
	if !strings.Contains(stdout.String(), "single-cmd-output") {
		t.Errorf("expected output on stdout, got %q", stdout.String())
	}
}

func TestRunner_Run_EmptyArgs(t *testing.T) {
	runner := NewRunner()
	ctx := context.Background()

	// Direct call to execSubprocess with empty args
	code, err := runner.execSubprocess(ctx, nil, nil, nil, nil)
	if code != ExitUsage {
		t.Errorf("expected ExitUsage for empty args, got %d", code)
	}
	if err == nil {
		t.Errorf("expected error for empty args, got nil")
	}
}

func TestRunner_Run_InvalidExecutablePath(t *testing.T) {
	runner := NewRunner(
		WithExecutable("/non/existent/binary/path"),
		WithWorkspace(t.TempDir()),
	)
	ctx := context.Background()

	var stdout, stderr bytes.Buffer
	cmd := NewFindCommand(".")

	code, err := runner.Run(ctx, cmd, strings.NewReader(""), &stdout, &stderr)
	if code != ExitError {
		t.Errorf("expected ExitError for invalid executable, got %d", code)
	}
	if err == nil {
		t.Errorf("expected error for invalid executable, got nil")
	}
}

func TestRunner_RunSequence_Empty(t *testing.T) {
	runner := NewRunner()
	ctx := context.Background()

	code, err := runner.RunSequence(ctx, nil, nil, nil, nil)
	if code != ExitClean {
		t.Errorf("expected ExitClean (0), got %d", code)
	}
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestRunner_RunSequence_FindAndReport_WithFindings(t *testing.T) {
	tmpDir := t.TempDir()
	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "find" ]; then
    echo "find progress log" >&2
    exit 0
elif [ "$1" = "report" ]; then
    echo '[{"FindingID":"vuln1","Title":"SQL Injection"}]'
    echo "report log notice" >&2
    exit 0
fi
exit 2
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(tmpDir),
	)

	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	findCmd := NewFindCommand("src/auth")
	reportCmd := NewReportCommand("json")

	cmds := []Command{findCmd, reportCmd}

	code, err := runner.RunSequence(ctx, cmds, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != ExitFindings {
		t.Errorf("expected ExitFindings (1), got %d", code)
	}

	out := stdout.String()
	if !strings.Contains(out, "SQL Injection") {
		t.Errorf("expected findings JSON on stdout, got %q", out)
	}

	errOut := stderr.String()
	if !strings.Contains(errOut, "find progress log") {
		t.Errorf("expected find progress on stderr, got %q", errOut)
	}
}

func TestRunner_RunSequence_FindAndReport_CleanCodebase(t *testing.T) {
	tmpDir := t.TempDir()
	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "find" ]; then
    exit 0
elif [ "$1" = "report" ]; then
    echo '[]'
    exit 0
fi
exit 2
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(tmpDir),
	)

	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	cmds := []Command{
		NewFindCommand("."),
		NewReportCommand("json"),
	}

	code, err := runner.RunSequence(ctx, cmds, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != ExitClean {
		t.Errorf("expected ExitClean (0), got %d", code)
	}

	out := strings.TrimSpace(stdout.String())
	if out != "[]" {
		t.Errorf("expected empty JSON array '[]' on stdout, got %q", out)
	}
}

func TestRunner_RunSequence_NilStdoutOnReport(t *testing.T) {
	tmpDir := t.TempDir()
	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "report" ]; then
    echo '[{"FindingID":"vuln1"}]'
    exit 0
fi
exit 0
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(tmpDir),
	)

	ctx := context.Background()
	cmds := []Command{NewReportCommand("json")}

	code, err := runner.RunSequence(ctx, cmds, strings.NewReader(""), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != ExitFindings {
		t.Errorf("expected ExitFindings (1) with nil stdout, got %d", code)
	}
}

func TestRunner_RunSequence_Phase1FailureAbortsPhase2(t *testing.T) {
	tmpDir := t.TempDir()
	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "find" ]; then
    echo "scan engine crashed" >&2
    exit 3
elif [ "$1" = "report" ]; then
    echo "SHOULD_NOT_EXECUTE"
    exit 0
fi
exit 2
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(tmpDir),
	)

	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	cmds := []Command{
		NewFindCommand("."),
		NewReportCommand("json"),
	}

	code, err := runner.RunSequence(ctx, cmds, strings.NewReader(""), &stdout, &stderr)
	if code != ExitError {
		t.Errorf("expected ExitError (3), got %d", code)
	}
	if err == nil {
		t.Errorf("expected error from failed find command, got nil")
	}

	out := stdout.String()
	if strings.Contains(out, "SHOULD_NOT_EXECUTE") {
		t.Errorf("report command ran despite find failure: stdout=%q", out)
	}
}

func TestRunner_RunSequence_SignalForwarding(t *testing.T) {
	tmpDir := t.TempDir()
	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
sleep 10
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(tmpDir),
	)

	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer

	cmds := []Command{
		NewFindCommand("."),
	}

	done := make(chan int)
	go func() {
		code, _ := runner.RunSequence(ctx, cmds, strings.NewReader(""), &stdout, &stderr)
		done <- code
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case code := <-done:
		if code == ExitClean {
			t.Errorf("expected non-zero exit code on cancelled context, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("command failed to terminate within timeout after cancellation")
	}
}

func TestEvaluateReportExitCode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "empty string",
			input:    "",
			expected: ExitClean,
		},
		{
			name:     "null string",
			input:    "null",
			expected: ExitClean,
		},
		{
			name:     "empty json array",
			input:    "[]",
			expected: ExitClean,
		},
		{
			name:     "json array with findings",
			input:    `[{"FindingID":"vuln1"}]`,
			expected: ExitFindings,
		},
		{
			name:     "malformed json array",
			input:    `[{"FindingID": invalid`,
			expected: ExitClean,
		},
		{
			name:     "malformed sarif json",
			input:    `{"version": invalid`,
			expected: ExitClean,
		},
		{
			name:     "sarif empty results",
			input:    `{"version":"2.1.0","runs":[{"results":[]}]}`,
			expected: ExitClean,
		},
		{
			name:     "sarif empty runs",
			input:    `{"version":"2.1.0","runs":[]}`,
			expected: ExitClean,
		},
		{
			name:     "sarif with results",
			input:    `{"version":"2.1.0","runs":[{"results":[{"ruleId":"CWE-89"}]}]}`,
			expected: ExitFindings,
		},
		{
			name:     "plain text string",
			input:    `some plain text output`,
			expected: ExitClean,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := EvaluateReportExitCode(tc.input)
			if result != tc.expected {
				t.Errorf("for input %q: expected exit code %d, got %d", tc.input, tc.expected, result)
			}
		})
	}
}
