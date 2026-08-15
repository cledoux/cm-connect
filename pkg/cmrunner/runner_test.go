package cmrunner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNewRunner_DefaultsAndOptions(t *testing.T) {
	// Defaults
	r1 := NewRunner()
	if r1.Workspace != DefaultWorkspace {
		t.Errorf("expected workspace %q, got %q", DefaultWorkspace, r1.Workspace)
	}

	// Options
	r2 := NewRunner(
		WithExecutable("/usr/bin/cm"),
		WithWorkspace("/custom/workspace"),
		WithEnv([]string{"TEST_ENV=1"}),
	)
	if r2.Executable != "/usr/bin/cm" {
		t.Errorf("expected executable /usr/bin/cm, got %q", r2.Executable)
	}
	if r2.Workspace != "/custom/workspace" {
		t.Errorf("expected workspace /custom/workspace, got %q", r2.Workspace)
	}
	if len(r2.Env) != 1 || r2.Env[0] != "TEST_ENV=1" {
		t.Errorf("expected env [TEST_ENV=1], got %v", r2.Env)
	}
}

func TestResolveExecutable(t *testing.T) {
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "my-binary")
	if err := os.WriteFile(existingFile, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// 1. Preferred path exists
	res1 := resolveExecutable(existingFile, "sh")
	if res1 != existingFile {
		t.Errorf("expected %q, got %q", existingFile, res1)
	}

	// 2. Preferred path does not exist, LookPath finds "sh"
	res2 := resolveExecutable(filepath.Join(tmpDir, "non-existent"), "sh")
	if !strings.Contains(res2, "sh") {
		t.Errorf("expected LookPath result for 'sh', got %q", res2)
	}

	// 3. Preferred path does not exist, fallback binary does not exist
	nonExistentPath := filepath.Join(tmpDir, "missing-bin")
	res3 := resolveExecutable(nonExistentPath, "non-existent-fallback-12345")
	if res3 != nonExistentPath {
		t.Errorf("expected %q, got %q", nonExistentPath, res3)
	}
}

func TestRunner_Run_NilCommand(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	r := NewRunner()

	code, err := r.Run(ctx, nil, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected error for nil command, got nil")
	}
	if code != ExitUsage {
		t.Errorf("expected exit code %d, got %d", ExitUsage, code)
	}
}

// mockCustomCommand implements Command interface for testing polymorphism.
type mockCustomCommand struct {
	subcommand string
	exitCode   int
	output     string
}

func (m *mockCustomCommand) Subcommand() string {
	return m.subcommand
}

func (m *mockCustomCommand) Execute(ctx context.Context, r *Runner, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	stdout.Write([]byte(m.output))
	if m.exitCode != 0 {
		return m.exitCode, errors.New("command failed")
	}
	return ExitClean, nil
}

func TestRunner_Run_PolymorphicCommand(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	r := NewRunner()

	cmd := &mockCustomCommand{
		subcommand: "custom",
		exitCode:   ExitClean,
		output:     "custom command executed",
	}

	code, err := r.Run(ctx, cmd, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != ExitClean {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if stdout.String() != "custom command executed" {
		t.Errorf("expected output 'custom command executed', got %q", stdout.String())
	}
}

func TestRunner_RunFind_TwoPhaseSuccess_WithFindings(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	// Mock cm script that handles "find" (Phase 1) and "report" (Phase 2)
	scriptFile := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "find" ]; then
    echo "Scanning target $2..." >&2
    echo "Discovered 1 finding" >&2
    exit 0
elif [ "$1" = "report" ]; then
    echo '[{"FindingID":"f1","Title":"SQL Injection","Severity":"HIGH"}]'
    echo "Session log: /tmp/session.log" >&2
    exit 0
fi
exit 2
`
	if err := os.WriteFile(scriptFile, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	r := NewRunner(WithExecutable(scriptFile), WithWorkspace(tmpDir))
	cmd := NewFindCommand("src/auth", "-y")

	code, err := r.Run(ctx, cmd, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Findings present -> exit code 1
	if code != ExitFindings {
		t.Errorf("expected exit code %d (ExitFindings), got %d", ExitFindings, code)
	}

	// Stdout must contain clean findings JSON
	if !strings.Contains(stdout.String(), "SQL Injection") {
		t.Errorf("expected findings JSON on stdout, got %q", stdout.String())
	}

	// Stderr must contain scanning progress and session logs
	if !strings.Contains(stderr.String(), "Scanning target src/auth") {
		t.Errorf("expected scanning progress on stderr, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Session log:") {
		t.Errorf("expected session log on stderr, got %q", stderr.String())
	}
}

func TestRunner_RunFind_TwoPhaseSuccess_CleanCodebase(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	scriptFile := filepath.Join(tmpDir, "mock-cm-clean.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "find" ]; then
    echo "Scanning clean target $2..." >&2
    exit 0
elif [ "$1" = "report" ]; then
    echo "[]"
    exit 0
fi
exit 2
`
	if err := os.WriteFile(scriptFile, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	r := NewRunner(WithExecutable(scriptFile), WithWorkspace(tmpDir))
	cmd := NewFindCommand(".")

	code, err := r.Run(ctx, cmd, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Zero findings -> exit code 0
	if code != ExitClean {
		t.Errorf("expected exit code %d (ExitClean), got %d", ExitClean, code)
	}
	if strings.TrimSpace(stdout.String()) != "[]" {
		t.Errorf("expected empty array on stdout, got %q", stdout.String())
	}
}

func TestRunner_RunFind_Phase1FailureAbortsPhase2(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	scriptFile := filepath.Join(tmpDir, "mock-cm-fail.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "find" ]; then
    echo "Fatal auth error" >&2
    exit 3
elif [ "$1" = "report" ]; then
    echo "Should never reach report"
    exit 0
fi
exit 2
`
	if err := os.WriteFile(scriptFile, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	r := NewRunner(WithExecutable(scriptFile), WithWorkspace(tmpDir))
	cmd := NewFindCommand(".")

	code, err := r.Run(ctx, cmd, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected error on Phase 1 failure, got nil")
	}
	if code != 3 {
		t.Errorf("expected exit code 3, got %d", code)
	}
	if stdout.Len() > 0 {
		t.Errorf("expected empty stdout on Phase 1 failure, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Fatal auth error") {
		t.Errorf("expected error message on stderr, got %q", stderr.String())
	}
}

func TestRunner_RunFind_HelpFlag(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	scriptFile := filepath.Join(tmpDir, "mock-cm-help.sh")
	scriptContent := `#!/bin/sh
echo "Usage: cm find [command]"
exit 0
`
	if err := os.WriteFile(scriptFile, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	r := NewRunner(WithExecutable(scriptFile), WithWorkspace(tmpDir))
	cmd := NewFindCommand(".", "--help")

	code, err := r.Run(ctx, cmd, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != ExitClean {
		t.Errorf("expected exit code 0 for help, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage: cm find") {
		t.Errorf("expected help usage on stdout, got %q", stdout.String())
	}
}

func TestRunner_RunFind_NonExistentBinary(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	r := NewRunner(WithExecutable("/bin/non-existent-cm-bin-12345"))
	code, err := r.Run(ctx, NewFindCommand("."), strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected error for non-existent binary, got nil")
	}
	if code != ExitError {
		t.Errorf("expected exit code %d, got %d", ExitError, code)
	}
	if !strings.Contains(stderr.String(), "failed to execute") {
		t.Errorf("expected error message on stderr, got %q", stderr.String())
	}
}

func TestRunner_RunFind_SignalForwarding(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	scriptFile := filepath.Join(tmpDir, "mock-signal.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "find" ]; then
    trap 'exit 0' TERM INT
    while true; do
        sleep 0.05
    done
elif [ "$1" = "report" ]; then
    echo "[]"
    exit 0
fi
`
	if err := os.WriteFile(scriptFile, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	r := NewRunner(WithExecutable(scriptFile), WithWorkspace(tmpDir))

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	start := time.Now()
	code, _ := r.Run(ctx, NewFindCommand("."), strings.NewReader(""), &stdout, &stderr)
	duration := time.Since(start)

	if duration > 3*time.Second {
		t.Errorf("execution took too long to terminate on signal (%v)", duration)
	}
	if code != ExitClean {
		t.Logf("signal terminated with code %d", code)
	}
}

func TestEvaluateReportExitCode(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		data     string
		expected int
	}{
		{"empty string", "json", "", ExitClean},
		{"null string", "json", "null", ExitClean},
		{"empty json array", "json", "[]", ExitClean},
		{"json array with findings", "json", `[{"FindingID":"123"}]`, ExitFindings},
		{"sarif empty results", "sarif", `{"runs":[{"results":[]}]}`, ExitClean},
		{"sarif with results", "sarif", `{"runs":[{"results":[{"ruleId":"XXE"}]}]}`, ExitFindings},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := evaluateReportExitCode(tc.format, []byte(tc.data))
			if code != tc.expected {
				t.Errorf("expected %d, got %d for data %q", tc.expected, code, tc.data)
			}
		})
	}
}
