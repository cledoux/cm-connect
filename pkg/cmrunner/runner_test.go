package cmrunner

import (
	"bytes"
	"context"
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

func TestRunner_RunFind_Success(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	// Runner executing /bin/sh which outputs arguments
	r := NewRunner(
		WithExecutable("/bin/sh"),
		WithWorkspace("."),
	)

	cmd := NewFindCommand("src/auth")
	// RunFind will execute: /bin/sh find src/auth --format json
	// /bin/sh will exit 0 without error when passed nonexistent file as first script arg,
	// or let's create a small shell script that echoes args
	tmpDir := t.TempDir()
	scriptFile := filepath.Join(tmpDir, "mock-cm.sh")
	if err := os.WriteFile(scriptFile, []byte("#!/bin/sh\necho \"args: $@\"\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	r.Executable = scriptFile
	r.Workspace = tmpDir

	code, err := r.RunFind(ctx, cmd, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if code != ExitClean {
		t.Errorf("expected exit code %d, got %d", ExitClean, code)
	}
	if !strings.Contains(stdout.String(), "find src/auth --format json") {
		t.Errorf("expected forwarded args in stdout, got %q", stdout.String())
	}
}

func TestRunner_RunFind_NilCommand(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	tmpDir := t.TempDir()

	scriptFile := filepath.Join(tmpDir, "mock-cm.sh")
	if err := os.WriteFile(scriptFile, []byte("#!/bin/sh\necho \"args: $@\"\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	r := NewRunner(WithExecutable(scriptFile), WithWorkspace(tmpDir))
	code, err := r.RunFind(ctx, nil, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if code != ExitClean {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "find . --format json") {
		t.Errorf("expected default find . args, got %q", stdout.String())
	}
}

func TestRunner_RunFind_ExitCodes(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		exitCode     int
		expectedCode int
	}{
		{"findings exit 1", 1, ExitFindings},
		{"usage exit 2", 2, ExitUsage},
		{"fatal exit 7", 7, 7},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			scriptFile := filepath.Join(tmpDir, "mock-exit.sh")
			scriptContent := "#!/bin/sh\nexit " + string(rune('0'+tc.exitCode)) + "\n"
			if tc.exitCode == 7 {
				scriptContent = "#!/bin/sh\nexit 7\n"
			}
			if err := os.WriteFile(scriptFile, []byte(scriptContent), 0755); err != nil {
				t.Fatalf("failed to write script: %v", err)
			}

			r := NewRunner(WithExecutable(scriptFile), WithWorkspace(tmpDir))
			code, err := r.RunFind(ctx, NewFindCommand("."), strings.NewReader(""), &stdout, &stderr)
			if err == nil {
				t.Fatalf("expected error for exit code %d, got nil", tc.exitCode)
			}
			if code != tc.expectedCode {
				t.Errorf("expected exit code %d, got %d", tc.expectedCode, code)
			}
		})
	}
}

func TestRunner_RunFind_NonExistentBinary(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	r := NewRunner(WithExecutable("/bin/non-existent-cm-bin-12345"))
	code, err := r.RunFind(ctx, NewFindCommand("."), strings.NewReader(""), &stdout, &stderr)
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
trap 'exit 0' TERM
sleep 5 &
wait $!
`
	if err := os.WriteFile(scriptFile, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	r := NewRunner(WithExecutable(scriptFile), WithWorkspace(tmpDir))

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	start := time.Now()
	code, _ := r.RunFind(ctx, NewFindCommand("."), strings.NewReader(""), &stdout, &stderr)
	duration := time.Since(start)

	if duration > 3*time.Second {
		t.Errorf("execution took too long to terminate on signal (%v)", duration)
	}
	if code != ExitClean {
		t.Logf("signal terminated with code %d", code)
	}
}
