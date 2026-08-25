package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cm-connect/pkg/cmrunner"
)

const sampleValidConfigForMainTest = `# CodeMender Default Configuration
scan:
  extensions:
    include:
      - ".go"
      - ".py"
output:
  format: "table"
tools:
  confirm_commands: true
  confirm_writes: true
vcs:
  type: git
  commands:
    reset: "git checkout HEAD -- ."
`

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
	if !strings.Contains(out, "cm-runner init") {
		t.Errorf("expected usage to contain 'cm-runner init', got %q", out)
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
    echo "find progress on stderr" >&2
    exit 0
elif [ "$cmd" = "report" ]; then
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

func TestRun_FindExecution_GlobalFlagsForwarded(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src", "auth")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to make test dir: %v", err)
	}

	argsLog := filepath.Join(tmpDir, "args.log")
	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
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
`, argsLog)
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"find", "src/auth"}, strings.NewReader(""), &stdout, &stderr, tmpDir, mockCM)
	if code != cmrunner.ExitClean {
		t.Fatalf("expected ExitClean (0), got %d (stderr: %q)", code, stderr.String())
	}

	logBytes, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("failed to read args log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(logBytes)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 invocations, got %d: %v", len(lines), lines)
	}
	if lines[0] != "--sandbox=false find src/auth -y" {
		t.Errorf("expected find invocation '--sandbox=false find src/auth -y', got %q", lines[0])
	}
	if lines[1] != "--sandbox=false report --format=json" {
		t.Errorf("expected report invocation '--sandbox=false report --format=json', got %q", lines[1])
	}
}

func TestRun_Init_DefaultPath_Success(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpHome)

	cmDir := filepath.Join(tmpHome, ".codemender")
	if err := os.MkdirAll(cmDir, 0755); err != nil {
		t.Fatalf("failed to make .codemender dir: %v", err)
	}
	configPath := filepath.Join(cmDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(sampleValidConfigForMainTest), 0600); err != nil {
		t.Fatalf("failed to write config.yaml: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr, tmpHome, "/bin/true")
	if code != cmrunner.ExitClean {
		t.Fatalf("expected ExitClean (0), got %d (stderr: %s)", code, stderr.String())
	}

	mutatedContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read mutated config: %v", err)
	}
	if !strings.Contains(string(mutatedContent), ".rs") {
		t.Errorf("expected .rs in mutated config, got:\n%s", string(mutatedContent))
	}
	if !strings.Contains(string(mutatedContent), `format: "json"`) && !strings.Contains(string(mutatedContent), `format: json`) {
		t.Errorf("expected format: json in mutated config, got:\n%s", string(mutatedContent))
	}
}

func TestRun_Init_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	code := run([]string{"init", "--help"}, strings.NewReader(""), &stdout, &stderr, tmpDir, "/bin/true")
	if code != cmrunner.ExitClean {
		t.Fatalf("expected ExitClean (0) for init --help, got %d", code)
	}
	if !strings.Contains(stdout.String(), "cm-runner init") {
		t.Errorf("expected init usage on stdout, got:\n%s", stdout.String())
	}
}

func TestRun_Init_MissingConfigFile(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpHome)

	var stdout, stderr bytes.Buffer
	code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr, tmpHome, "/bin/true")
	if code != cmrunner.ExitError {
		t.Fatalf("expected ExitError (>2), got %d", code)
	}
	if !strings.Contains(stderr.String(), "failed to initialize configuration") {
		t.Errorf("expected error on stderr, got:\n%s", stderr.String())
	}
}

func TestRun_Init_HomeUnset(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Unsetenv("HOME")

	var stdout, stderr bytes.Buffer
	code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr, t.TempDir(), "/bin/true")
	if code != cmrunner.ExitError {
		t.Fatalf("expected ExitError when HOME is unset, got %d", code)
	}
	if !strings.Contains(stderr.String(), "failed to initialize configuration") || !strings.Contains(stderr.String(), "HOME environment variable is not set") {
		t.Errorf("expected HOME unset error on stderr, got:\n%s", stderr.String())
	}
}

func TestRun_Init_MissingCriticalKey_FailsFast(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpHome)

	cmDir := filepath.Join(tmpHome, ".codemender")
	if err := os.MkdirAll(cmDir, 0755); err != nil {
		t.Fatalf("failed to make .codemender dir: %v", err)
	}

	badConfig := `
scan:
  extensions:
    include:
      - ".go"
output:
  format: "table"
`
	configPath := filepath.Join(cmDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(badConfig), 0600); err != nil {
		t.Fatalf("failed to write bad config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr, tmpHome, "/bin/true")
	if code != cmrunner.ExitError {
		t.Fatalf("expected ExitError (>2), got %d", code)
	}
	if !strings.Contains(stderr.String(), "tools.confirm_commands") {
		t.Errorf("expected missing critical key in stderr, got:\n%s", stderr.String())
	}
}

func TestRun_Init_CMInitPrefix_Rejected(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	code := run([]string{"cm", "init"}, strings.NewReader(""), &stdout, &stderr, tmpDir, "/bin/true")
	if code != cmrunner.ExitUsage {
		t.Fatalf("expected ExitUsage (2) for cm init prefix, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unrecognized subcommand 'cm'") {
		t.Errorf("expected unrecognized subcommand error on stderr, got:\n%s", stderr.String())
	}
}

func TestRun_Fix_Success_Fixed(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatalf("failed to make ws dir: %v", err)
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

	findingFile := filepath.Join(tmpDir, "finding.json")
	sampleFinding := `{"FilePath": "store.go", "StartLine": 2, "Title": "SQLi", "Analysis": "fix it"}`
	if err := os.WriteFile(findingFile, []byte(sampleFinding), 0644); err != nil {
		t.Fatalf("failed to write finding.json: %v", err)
	}

	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "report" ] && [ "$2" = "import" ]; then
    exit 0
elif [ "$1" = "report" ] && [ "$2" = "--format=json" ]; then
    echo '[{"FindingID":"uuid-fixed-main","Title":"SQLi"}]'
    exit 0
elif [ "$1" = "fix" ]; then
    echo "package store\nfunc New() {}\n" > store.go
    exit 0
fi
exit 2
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock cm script: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"fix", findingFile, "--", "-c", "custom-context"}, strings.NewReader(""), &stdout, &stderr, wsDir, mockCM)
	if code != cmrunner.ExitClean {
		t.Fatalf("expected ExitClean (0) for successful fix, got %d (stderr: %s)", code, stderr.String())
	}

	outStr := stdout.String()
	if !strings.Contains(outStr, `"status": "FIXED"`) {
		t.Errorf("expected FIXED in stdout change envelope, got: %s", outStr)
	}
	if !strings.Contains(outStr, "uuid-fixed-main") {
		t.Errorf("expected finding ID in stdout change envelope, got: %s", outStr)
	}
}

func TestRun_Fix_Unresolved(t *testing.T) {
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

	findingFile := filepath.Join(tmpDir, "finding.json")
	sampleFinding := `{"FilePath": "store.go", "Title": "Hard Problem"}`
	_ = os.WriteFile(findingFile, []byte(sampleFinding), 0644)

	mockCM := filepath.Join(tmpDir, "mock-cm.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "report" ] && [ "$2" = "import" ]; then
    exit 0
elif [ "$1" = "report" ] && [ "$2" = "--format=json" ]; then
    echo '[{"FindingID":"uuid-unres-main","Title":"Hard Problem"}]'
    exit 0
elif [ "$1" = "fix" ]; then
    exit 0
fi
exit 2
`
	_ = os.WriteFile(mockCM, []byte(scriptContent), 0755)

	var stdout, stderr bytes.Buffer
	code := run([]string{"fix", findingFile}, strings.NewReader(""), &stdout, &stderr, wsDir, mockCM)
	if code != cmrunner.ExitFindings {
		t.Fatalf("expected ExitFindings (1) on unresolved fix, got %d (stderr: %s)", code, stderr.String())
	}

	outStr := stdout.String()
	if !strings.Contains(outStr, `"status": "UNRESOLVED"`) {
		t.Errorf("expected UNRESOLVED in stdout change envelope, got: %s", outStr)
	}
}

func TestRun_Fix_UsageError_MissingTarget(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"fix"}, strings.NewReader(""), &stdout, &stderr, t.TempDir(), "/bin/true")
	if code != cmrunner.ExitUsage {
		t.Fatalf("expected ExitUsage (2) for fix without args, got %d", code)
	}
	if !strings.Contains(stderr.String(), "missing target finding argument") {
		t.Errorf("expected missing target argument error on stderr, got: %s", stderr.String())
	}
}

func TestRun_Fix_NonExistentFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"fix", "/non/existent/finding.json"}, strings.NewReader(""), &stdout, &stderr, t.TempDir(), "/bin/true")
	if code != cmrunner.ExitUsage {
		t.Fatalf("expected ExitUsage (2) for non-existent finding file, got %d", code)
	}
	if !strings.Contains(stderr.String(), "finding file not found") {
		t.Errorf("expected finding file not found on stderr, got: %s", stderr.String())
	}
}

func TestRun_FindDiff_EmptyDiff_FastPath(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpHome)

	cmDir := filepath.Join(tmpHome, ".codemender")
	_ = os.MkdirAll(cmDir, 0755)
	configPath := filepath.Join(cmDir, "config.yaml")
	_ = os.WriteFile(configPath, []byte(sampleValidConfigForMainTest), 0600)

	oldGitDiff := execGitDiffFn
	defer func() { execGitDiffFn = oldGitDiff }()

	execGitDiffFn = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return []byte(""), nil // 0 bytes empty diff
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"find-diff", "HEAD", "HEAD"}, strings.NewReader(""), &stdout, &stderr, t.TempDir(), "/bin/true")
	if code != cmrunner.ExitClean {
		t.Fatalf("expected ExitClean (0) for empty diff fast-path, got %d (stderr: %s)", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "[]" {
		t.Errorf("expected '[]' on stdout for empty diff, got: %q", stdout.String())
	}
}

func TestRun_FindDiff_GitError_ExitUsage(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpHome)

	cmDir := filepath.Join(tmpHome, ".codemender")
	_ = os.MkdirAll(cmDir, 0755)
	configPath := filepath.Join(cmDir, "config.yaml")
	_ = os.WriteFile(configPath, []byte(sampleValidConfigForMainTest), 0600)

	oldGitDiff := execGitDiffFn
	defer func() { execGitDiffFn = oldGitDiff }()

	execGitDiffFn = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return nil, errors.New("fatal: bad revision 'non-existent-sha'")
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"find-diff", "non-existent-sha"}, strings.NewReader(""), &stdout, &stderr, t.TempDir(), "/bin/true")
	if code != cmrunner.ExitUsage {
		t.Fatalf("expected ExitUsage (2) for git diff error, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "git diff failed") || !strings.Contains(stderr.String(), "fetch-depth: 0") {
		t.Errorf("expected git diff error and fetch-depth hint on stderr, got: %s", stderr.String())
	}
}

func TestRun_FindDiff_Success_TwoPhase(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpHome)

	cmDir := filepath.Join(tmpHome, ".codemender")
	_ = os.MkdirAll(cmDir, 0755)
	configPath := filepath.Join(cmDir, "config.yaml")
	_ = os.WriteFile(configPath, []byte(sampleValidConfigForMainTest), 0600)

	oldGitDiff := execGitDiffFn
	defer func() { execGitDiffFn = oldGitDiff }()

	sampleDiff := "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"
	execGitDiffFn = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return []byte(sampleDiff), nil
	}

	mockCM := filepath.Join(tmpHome, "mock-cm.sh")
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
    echo "find diff progress on stderr" >&2
    exit 0
elif [ "$cmd" = "report" ]; then
    echo '[{"FindingID":"diff-vuln-1","Title":"Diff Vulnerability"}]'
    exit 0
fi
exit 2
`
	if err := os.WriteFile(mockCM, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock cm script: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"find-diff", "origin/main", "HEAD", "--", "-c", "Extra diff check"}, strings.NewReader(""), &stdout, &stderr, tmpHome, mockCM)
	if code != cmrunner.ExitFindings {
		t.Fatalf("expected ExitFindings (1) for findings in diff, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Diff Vulnerability") {
		t.Errorf("expected findings on stdout, got: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "find diff progress on stderr") {
		t.Errorf("expected progress on stderr, got: %s", stderr.String())
	}

	// Verify .diff was registered in config.yaml
	mutatedContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read mutated config: %v", err)
	}
	if !strings.Contains(string(mutatedContent), ".diff") {
		t.Errorf("expected .diff in config.yaml, got:\n%s", string(mutatedContent))
	}
}

func TestRun_FindDiff_ConfigMutationError(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Unsetenv("HOME")

	var stdout, stderr bytes.Buffer
	code := run([]string{"find-diff", "HEAD"}, strings.NewReader(""), &stdout, &stderr, t.TempDir(), "/bin/true")
	if code != cmrunner.ExitError {
		t.Fatalf("expected ExitError (>2) when HOME is unset, got %d", code)
	}
	if !strings.Contains(stderr.String(), "failed to register .diff extension") {
		t.Errorf("expected config mutation error on stderr, got: %s", stderr.String())
	}
}

func TestRun_FindDiff_PermissionFallback(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpHome)

	cmDir := filepath.Join(tmpHome, ".codemender")
	_ = os.MkdirAll(cmDir, 0755)
	configPath := filepath.Join(cmDir, "config.yaml")
	_ = os.WriteFile(configPath, []byte(sampleValidConfigForMainTest), 0600)

	oldGitDiff := execGitDiffFn
	defer func() { execGitDiffFn = oldGitDiff }()

	sampleDiff := "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"
	execGitDiffFn = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return []byte(sampleDiff), nil
	}

	mockCM := filepath.Join(tmpHome, "mock-cm.sh")
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
		t.Fatalf("failed to write mock cm script: %v", err)
	}

	// Create a read-only workspace directory (mode 0555)
	roWorkspace := filepath.Join(tmpHome, "ro-workspace")
	if err := os.MkdirAll(roWorkspace, 0555); err != nil {
		t.Fatalf("failed to create ro workspace: %v", err)
	}
	defer os.Chmod(roWorkspace, 0755) // allow cleanup

	var stdout, stderr bytes.Buffer
	code := run([]string{"find-diff", "origin/main", "HEAD"}, strings.NewReader(""), &stdout, &stderr, roWorkspace, mockCM)
	if code != cmrunner.ExitClean {
		t.Fatalf("expected ExitClean (0) with permission fallback, got %d (stderr: %s)", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "[]" {
		t.Errorf("expected empty array [] on stdout, got: %s", stdout.String())
	}
}
