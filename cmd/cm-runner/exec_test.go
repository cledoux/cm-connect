package main

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestExecuteProcess_Success(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code, err := ExecuteProcess(
		context.Background(),
		"/bin/sh",
		[]string{"-c", "echo 'findings payload'; echo 'log line' >&2; exit 0"},
		[]string{"PATH=/bin:/usr/bin"},
		t.TempDir(),
		nil,
		&stdout,
		&stderr,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if strings.TrimSpace(stdout.String()) != "findings payload" {
		t.Errorf("expected stdout %q, got %q", "findings payload", stdout.String())
	}
	if strings.TrimSpace(stderr.String()) != "log line" {
		t.Errorf("expected stderr %q, got %q", "log line", stderr.String())
	}
}

func TestExecuteProcess_ExitCodes(t *testing.T) {
	testCases := []struct {
		script   string
		expected int
	}{
		{"exit 1", 1},
		{"exit 2", 2},
		{"exit 7", 7},
	}

	for _, tc := range testCases {
		t.Run(tc.script, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, err := ExecuteProcess(
				context.Background(),
				"/bin/sh",
				[]string{"-c", tc.script},
				[]string{"PATH=/bin:/usr/bin"},
				t.TempDir(),
				nil,
				&stdout,
				&stderr,
			)

			if code != tc.expected {
				t.Errorf("expected exit code %d, got %d (err: %v)", tc.expected, code, err)
			}
		})
	}
}

func TestExecuteProcess_EnvAndWorkingDir(t *testing.T) {
	tmpDir := t.TempDir()
	var stdout, stderr bytes.Buffer

	env := []string{
		"CUSTOM_KEY=CUSTOM_VAL",
		"NO_COLOR=1",
		"TERM=dumb",
	}

	code, err := ExecuteProcess(
		context.Background(),
		"/bin/sh",
		[]string{"-c", "echo $CUSTOM_KEY; echo $NO_COLOR; pwd"},
		env,
		tmpDir,
		nil,
		&stdout,
		&stderr,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	out := stdout.String()
	if !strings.Contains(out, "CUSTOM_VAL") {
		t.Errorf("expected stdout to contain CUSTOM_VAL, got %q", out)
	}
	if !strings.Contains(out, "1") {
		t.Errorf("expected stdout to contain NO_COLOR 1, got %q", out)
	}
	if !strings.Contains(out, tmpDir) {
		t.Errorf("expected stdout to contain working dir %q, got %q", tmpDir, out)
	}
}

func TestExecuteProcess_NonExistentBinary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code, err := ExecuteProcess(
		context.Background(),
		"/path/to/non/existent/binary",
		[]string{},
		[]string{},
		t.TempDir(),
		nil,
		&stdout,
		&stderr,
	)

	if err == nil {
		t.Fatalf("expected error for non-existent binary, got nil")
	}
	if code <= 2 {
		t.Errorf("expected exit code > 2 for fatal launch failure, got %d", code)
	}
	if !strings.Contains(stderr.String(), "failed to execute") {
		t.Errorf("expected stderr to contain error trace, got %q", stderr.String())
	}
}

func TestExecuteProcess_SignalForwarding(t *testing.T) {
	script := `
trap "echo 'trapped sigterm'; exit 143" TERM
echo 'ready'
while true; do
  sleep 0.1
done
`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start cmd: %v", err)
	}

	readyChan := make(chan struct{})
	var mu sync.Mutex
	var outputLines []string

	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			text := scanner.Text()
			mu.Lock()
			outputLines = append(outputLines, text)
			mu.Unlock()
			if text == "ready" {
				close(readyChan)
			}
		}
	}()

	select {
	case <-readyChan:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for process to become ready")
	}

	// Send SIGTERM to process group
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("failed to get pgid: %v", err)
	}
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		t.Fatalf("failed to send SIGTERM: %v", err)
	}

	_ = cmd.Wait()

	mu.Lock()
	combined := strings.Join(outputLines, "\n")
	mu.Unlock()

	if !strings.Contains(combined, "trapped sigterm") {
		t.Errorf("expected process output to contain 'trapped sigterm', got: %q", combined)
	}
}
