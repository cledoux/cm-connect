package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestExecuteShell_NoTTY(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer

	isTermMock := func(fd int) bool {
		return false
	}

	procCalled := false
	runCmdMock := func(ctx context.Context, executable string, env []string, dir string, stdin io.Reader, outW, errW io.Writer) error {
		procCalled = true
		return nil
	}

	exitCode := ExecuteShell(
		context.Background(),
		"/bin/bash",
		"/workspace",
		os.Stdin,
		&stdout,
		&stderr,
		[]string{"PATH=/bin"},
		isTermMock,
		runCmdMock,
	)

	if exitCode != 2 {
		t.Errorf("expected exit code 2, got %d", exitCode)
	}

	if procCalled {
		t.Errorf("process runner should not have been called when TTY is missing")
	}

	expectedErr := "Error: 'shell' subcommand requires an interactive terminal. Please run with 'docker run -it <image> shell'"
	if !strings.Contains(stderr.String(), expectedErr) {
		t.Errorf("expected stderr to contain %q, got %q", expectedErr, stderr.String())
	}
}

func TestExecuteShell_WithTTY(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer

	isTermMock := func(fd int) bool {
		return true
	}

	procCalled := false
	var capturedExecutable string
	var capturedDir string
	var capturedEnv []string

	runCmdMock := func(ctx context.Context, executable string, env []string, dir string, stdin io.Reader, outW, errW io.Writer) error {
		procCalled = true
		capturedExecutable = executable
		capturedDir = dir
		capturedEnv = env
		return nil
	}

	customEnv := []string{"PATH=/bin", "USER=codemender"}

	exitCode := ExecuteShell(
		context.Background(),
		"/bin/bash",
		"/workspace",
		os.Stdin,
		&stdout,
		&stderr,
		customEnv,
		isTermMock,
		runCmdMock,
	)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	if !procCalled {
		t.Fatalf("process runner should have been called when TTY is present")
	}

	if capturedExecutable != "/bin/bash" {
		t.Errorf("expected executable /bin/bash, got %q", capturedExecutable)
	}

	if capturedDir != "/workspace" {
		t.Errorf("expected working directory /workspace, got %q", capturedDir)
	}

	if len(capturedEnv) != len(customEnv) {
		t.Errorf("expected env %v, got %v", customEnv, capturedEnv)
	}
}
