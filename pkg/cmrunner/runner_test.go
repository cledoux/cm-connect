package cmrunner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
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
		WithGlobalFlags("--sandbox=false"),
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
	if len(r.GlobalFlags) != 1 || r.GlobalFlags[0] != "--sandbox=false" {
		t.Errorf("expected custom global flags, got %v", r.GlobalFlags)
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

func TestRunner_WithGlobalFlags(t *testing.T) {
	tmpDir := t.TempDir()
	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
echo "args: $@"
exit 0
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(tmpDir),
		WithGlobalFlags("--sandbox=false"),
	)

	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	cmd := NewFindCommand("src/auth")

	code, err := runner.Run(ctx, cmd, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != ExitClean {
		t.Errorf("expected ExitClean (0), got %d", code)
	}
	expectedOutput := "args: --sandbox=false find src/auth -y\n"
	if stdout.String() != expectedOutput {
		t.Errorf("expected stdout %q, got %q", expectedOutput, stdout.String())
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
cmd=""
for arg in "$@"; do
    case "$arg" in
        find|report)
            cmd="$arg"
            break
            ;;
    esac
done
if [ "$cmd" = "find" ]; then
    echo "find progress log" >&2
    exit 0
elif [ "$cmd" = "report" ]; then
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
cmd=""
for arg in "$@"; do
    case "$arg" in
        find|report)
            cmd="$arg"
            break
            ;;
    esac
done
if [ "$cmd" = "find" ]; then
    exit 0
elif [ "$cmd" = "report" ]; then
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
cmd=""
for arg in "$@"; do
    case "$arg" in
        report)
            cmd="$arg"
            break
            ;;
    esac
done
if [ "$cmd" = "report" ]; then
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
cmd=""
for arg in "$@"; do
    case "$arg" in
        find|report)
            cmd="$arg"
            break
            ;;
    esac
done
if [ "$cmd" = "find" ]; then
    echo "scan engine crashed" >&2
    exit 3
elif [ "$cmd" = "report" ]; then
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

func TestRunner_RunFixPipeline_Success_Fixed(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatalf("failed to create ws dir: %v", err)
	}

	// Initialize git repo in workspace
	cmdInit := exec.Command("git", "init")
	cmdInit.Dir = wsDir
	_ = cmdInit.Run()
	_ = exec.Command("git", "config", "user.name", "Test").Run()
	_ = exec.Command("git", "config", "user.email", "test@example.com").Run()

	storeFile := filepath.Join(wsDir, "store.go")
	_ = os.WriteFile(storeFile, []byte("package store\nfunc Old() {}\n"), 0644)
	cmdAdd := exec.Command("git", "add", "store.go")
	cmdAdd.Dir = wsDir
	_ = cmdAdd.Run()
	cmdCommit := exec.Command("git", "commit", "-m", "initial")
	cmdCommit.Dir = wsDir
	_ = cmdCommit.Run()

	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "report" ] && [ "$2" = "import" ]; then
    echo "import progress log" >&2
    exit 0
elif [ "$1" = "report" ] && [ "$2" = "--format=json" ]; then
    echo '[{"FindingID":"478a8868-uuid","Title":"SQLi","VulnType":"CWE-89","Analysis":"parameterize"}]'
    exit 0
elif [ "$1" = "fix" ]; then
    echo "fix reasoning telemetry" >&2
    # Modify store.go in workspace
    echo "package store\nfunc New() {}\n" > store.go
    exit 0
fi
echo "unknown subcommand: $*" >&2
exit 2
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(wsDir),
	)

	rawFinding := []byte(`{
		"FilePath": "store.go",
		"StartLine": 2,
		"Title": "SQLi",
		"Analysis": "parameterize",
		"Severity": "HIGH",
		"VulnType": "CWE-89",
		"Snippet": "func Old() {}"
	}`)

	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	code, err := runner.RunFixPipeline(ctx, rawFinding, []string{"-c", "Sanitize"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("RunFixPipeline failed: %v", err)
	}
	if code != ExitClean {
		t.Errorf("expected ExitClean (0), got %d", code)
	}

	outStr := stdout.String()
	if !strings.Contains(outStr, `"status": "FIXED"`) {
		t.Errorf("expected FIXED status in stdout envelope, got: %s", outStr)
	}
	if !strings.Contains(outStr, "478a8868-uuid") {
		t.Errorf("expected finding ID in stdout envelope, got: %s", outStr)
	}

	errStr := stderr.String()
	if !strings.Contains(errStr, "import progress log") || !strings.Contains(errStr, "fix reasoning telemetry") {
		t.Errorf("expected stderr to contain progress logs, got: %s", errStr)
	}
}

func TestRunner_RunFixPipeline_Unresolved_EmptyDiff(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(wsDir, 0755)

	cmdInit := exec.Command("git", "init")
	cmdInit.Dir = wsDir
	_ = cmdInit.Run()
	_ = exec.Command("git", "config", "user.name", "Test").Run()
	_ = exec.Command("git", "config", "user.email", "test@example.com").Run()

	storeFile := filepath.Join(wsDir, "store.go")
	_ = os.WriteFile(storeFile, []byte("package store\n"), 0644)
	cmdAdd := exec.Command("git", "add", "store.go")
	cmdAdd.Dir = wsDir
	_ = cmdAdd.Run()
	cmdCommit := exec.Command("git", "commit", "-m", "initial")
	cmdCommit.Dir = wsDir
	_ = cmdCommit.Run()

	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "report" ] && [ "$2" = "import" ]; then
    exit 0
elif [ "$1" = "report" ] && [ "$2" = "--format=json" ]; then
    echo '[{"FindingID":"unresolved-uuid","Title":"Complex Issue"}]'
    exit 0
elif [ "$1" = "fix" ]; then
    # No modifications
    exit 0
fi
exit 2
`
	_ = os.WriteFile(mockCM, []byte(scriptContent), 0755)

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(wsDir),
	)

	rawFinding := []byte(`{"FilePath": "store.go", "Title": "Complex Issue"}`)

	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	code, err := runner.RunFixPipeline(ctx, rawFinding, nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("RunFixPipeline failed: %v", err)
	}
	if code != ExitFindings {
		t.Errorf("expected ExitFindings (1) on unresolved fix, got %d", code)
	}

	outStr := stdout.String()
	if !strings.Contains(outStr, `"status": "UNRESOLVED"`) {
		t.Errorf("expected UNRESOLVED status in stdout envelope, got: %s", outStr)
	}
}

func TestRunner_RunFixPipeline_InvalidFindingJSON(t *testing.T) {
	runner := NewRunner(WithWorkspace(t.TempDir()))
	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	code, err := runner.RunFixPipeline(ctx, []byte(`{invalid`), nil, &stdout, &stderr)
	if code != ExitUsage {
		t.Errorf("expected ExitUsage (2), got %d", code)
	}
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestRunner_RunFixPipeline_ImportFailure(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(wsDir, 0755)
	_ = os.WriteFile(filepath.Join(wsDir, "foo.go"), []byte("package foo\n"), 0644)

	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "report" ] && [ "$2" = "import" ]; then
    echo "fatal import crash" >&2
    exit 3
fi
exit 0
`
	_ = os.WriteFile(mockCM, []byte(scriptContent), 0755)

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(wsDir),
	)

	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	code, err := runner.RunFixPipeline(ctx, []byte(`{"FilePath": "foo.go", "Title": "Issue"}`), nil, &stdout, &stderr)
	if code != ExitError {
		t.Errorf("expected ExitError (3), got %d", code)
	}
	if err == nil {
		t.Error("expected error from failed import, got nil")
	}
}

func TestRunner_RunFixPipeline_ReportFailure(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(wsDir, 0755)
	_ = os.WriteFile(filepath.Join(wsDir, "foo.go"), []byte("package foo\n"), 0644)

	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "report" ] && [ "$2" = "import" ]; then
    exit 0
elif [ "$1" = "report" ] && [ "$2" = "--format=json" ]; then
    echo "database query error" >&2
    exit 3
fi
exit 0
`
	_ = os.WriteFile(mockCM, []byte(scriptContent), 0755)

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(wsDir),
	)

	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	code, err := runner.RunFixPipeline(ctx, []byte(`{"FilePath": "foo.go", "Title": "Issue"}`), nil, &stdout, &stderr)
	if code != ExitError {
		t.Errorf("expected ExitError (3), got %d", code)
	}
	if err == nil {
		t.Error("expected error from failed report, got nil")
	}
}

func TestExtractFindingID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedID  string
		expectError bool
	}{
		{
			name:       "array with FindingID",
			input:      `[{"FindingID": "f-123"}]`,
			expectedID: "f-123",
		},
		{
			name:       "array with finding_id",
			input:      `[{"finding_id": "f-456"}]`,
			expectedID: "f-456",
		},
		{
			name:       "array with ID",
			input:      `[{"ID": "f-789"}]`,
			expectedID: "f-789",
		},
		{
			name:       "array with id",
			input:      `[{"id": "f-abc"}]`,
			expectedID: "f-abc",
		},
		{
			name:       "single object with FindingID",
			input:      `{"FindingID": "f-obj"}`,
			expectedID: "f-obj",
		},
		{
			name:       "single object with finding_id",
			input:      `{"finding_id": "f-obj-alt"}`,
			expectedID: "f-obj-alt",
		},
		{
			name:       "single object with ID",
			input:      `{"ID": "f-obj-id"}`,
			expectedID: "f-obj-id",
		},
		{
			name:       "single object with id",
			input:      `{"id": "f-obj-id-lower"}`,
			expectedID: "f-obj-id-lower",
		},
		{
			name:        "empty array",
			input:       `[]`,
			expectError: true,
		},
		{
			name:        "empty string",
			input:       ``,
			expectError: true,
		},
		{
			name:        "invalid JSON array",
			input:       `[invalid`,
			expectError: true,
		},
		{
			name:        "invalid JSON object",
			input:       `{invalid`,
			expectError: true,
		},
		{
			name:        "primitive JSON",
			input:       `12345`,
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, err := extractFindingID([]byte(tc.input))
			if tc.expectError && err == nil {
				t.Errorf("expected error, got nil (id=%q)", id)
			}
			if !tc.expectError {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if id != tc.expectedID {
					t.Errorf("expected id %q, got %q", tc.expectedID, id)
				}
			}
		})
	}
}

func TestRunner_RunFixPipeline_NilStreams(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(wsDir, 0755)
	_ = os.WriteFile(filepath.Join(wsDir, "foo.go"), []byte("package foo\n"), 0644)

	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "report" ] && [ "$2" = "import" ]; then
    exit 0
elif [ "$1" = "report" ] && [ "$2" = "--format=json" ]; then
    echo '[{"FindingID":"uuid-nil-streams"}]'
    exit 0
elif [ "$1" = "fix" ]; then
    exit 0
fi
exit 0
`
	_ = os.WriteFile(mockCM, []byte(scriptContent), 0755)

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(wsDir),
	)

	ctx := context.Background()
	code, err := runner.RunFixPipeline(ctx, []byte(`{"FilePath": "foo.go", "Title": "Issue"}`), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != ExitFindings { // ExitFindings (1) because no diff produced
		t.Errorf("expected ExitFindings (1), got %d", code)
	}
}

func TestRunner_RunFixPipeline_FindingIDUnknownFailsFast(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(wsDir, 0755)
	_ = os.WriteFile(filepath.Join(wsDir, "foo.go"), []byte("package foo\n"), 0644)

	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "report" ] && [ "$2" = "import" ]; then
    exit 0
elif [ "$1" = "report" ] && [ "$2" = "--format=json" ]; then
    echo '[{"FindingID":"unknown"}]'
    exit 0
elif [ "$1" = "fix" ]; then
    echo "SHOULD_NOT_BE_CALLED" >&2
    exit 0
fi
exit 0
`
	_ = os.WriteFile(mockCM, []byte(scriptContent), 0755)

	runner := NewRunner(
		WithExecutable(mockCM),
		WithWorkspace(wsDir),
	)

	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	code, err := runner.RunFixPipeline(ctx, []byte(`{"FilePath": "foo.go", "Title": "Issue"}`), nil, &stdout, &stderr)
	if code != ExitError {
		t.Errorf("expected ExitError (3) for unknown finding ID, got %d", code)
	}
	if err == nil {
		t.Errorf("expected error for unknown finding ID, got nil")
	}
	if strings.Contains(stderr.String(), "SHOULD_NOT_BE_CALLED") {
		t.Errorf("Stage 4 executed despite unknown FindingID failure: stderr=%q", stderr.String())
	}
}

func TestRunner_RunFixPipeline_SubprocessCrash(t *testing.T) {
	crashCodes := []struct {
		name     string
		exitCode int
	}{
		{name: "OOM / SIGKILL exit 137", exitCode: 137},
		{name: "Command not found exit 127", exitCode: 127},
		{name: "CLI usage error exit 2", exitCode: 2},
	}

	for _, tc := range crashCodes {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			wsDir := filepath.Join(tmpDir, "workspace")
			_ = os.MkdirAll(wsDir, 0755)
			_ = os.WriteFile(filepath.Join(wsDir, "foo.go"), []byte("package foo\n"), 0644)

			mockCM := filepath.Join(tmpDir, "mock-cm.sh")
			scriptContent := `#!/bin/sh
if [ "$1" = "report" ] && [ "$2" = "import" ]; then
    exit 0
elif [ "$1" = "report" ] && [ "$2" = "--format=json" ]; then
    echo '[{"FindingID":"crash-test-uuid"}]'
    exit 0
elif [ "$1" = "fix" ]; then
    echo "Subprocess crashing with code ` + string(rune('0'+tc.exitCode/100)) + `..." >&2
    exit ` + string(rune('0'+tc.exitCode/100)) + string(rune('0'+(tc.exitCode/10)%10)) + string(rune('0'+tc.exitCode%10)) + `
fi
exit 0
`
			// Use fmt.Sprintf for reliable script writing
			scriptContent = "#!/bin/sh\n" +
				"if [ \"$1\" = \"report\" ] && [ \"$2\" = \"import\" ]; then\n" +
				"    exit 0\n" +
				"elif [ \"$1\" = \"report\" ] && [ \"$2\" = \"--format=json\" ]; then\n" +
				"    echo '[{\"FindingID\":\"crash-test-uuid\"}]'\n" +
				"    exit 0\n" +
				"elif [ \"$1\" = \"fix\" ]; then\n" +
				"    echo \"Subprocess crash output\" >&2\n" +
				"    exit " + string([]byte{byte('0' + tc.exitCode/100), byte('0' + (tc.exitCode/10)%10), byte('0' + tc.exitCode%10)}) + "\n" +
				"fi\n" +
				"exit 0\n"
			if tc.exitCode < 10 {
				scriptContent = "#!/bin/sh\n" +
					"if [ \"$1\" = \"report\" ] && [ \"$2\" = \"import\" ]; then\n" +
					"    exit 0\n" +
					"elif [ \"$1\" = \"report\" ] && [ \"$2\" = \"--format=json\" ]; then\n" +
					"    echo '[{\"FindingID\":\"crash-test-uuid\"}]'\n" +
					"    exit 0\n" +
					"elif [ \"$1\" = \"fix\" ]; then\n" +
					"    echo \"Subprocess crash output\" >&2\n" +
					"    exit " + string([]byte{byte('0' + tc.exitCode)}) + "\n" +
					"fi\n" +
					"exit 0\n"
			}
			_ = os.WriteFile(mockCM, []byte(scriptContent), 0755)

			runner := NewRunner(
				WithExecutable(mockCM),
				WithWorkspace(wsDir),
			)

			var stdout, stderr bytes.Buffer
			ctx := context.Background()
			code, err := runner.RunFixPipeline(ctx, []byte(`{"FilePath": "foo.go", "Title": "Issue"}`), nil, &stdout, &stderr)
			if code != ExitError {
				t.Errorf("expected ExitError (3) on fix crash with exit %d, got %d", tc.exitCode, code)
			}
			if err == nil {
				t.Errorf("expected non-nil error on fix crash with exit %d, got nil", tc.exitCode)
			}
			if stdout.Len() > 0 {
				t.Errorf("expected no envelope on stdout on fix crash, got: %s", stdout.String())
			}
		})
	}
}
