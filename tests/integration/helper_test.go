//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	defaultTimeout = 15 * time.Second
	defaultImage   = "cm-runner:latest"
)

// getImageName returns the target Docker image name from environment or default.
func getImageName() string {
	if img := os.Getenv("CM_IMAGE_NAME"); img != "" {
		return img
	}
	return defaultImage
}

// getRepoRoot returns the absolute path to the repository root directory.
func getRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if filepath.Base(wd) == "integration" {
		return filepath.Clean(filepath.Join(wd, "..", ".."))
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd
}

// runCommand executes a command with a strict timeout and captures stdout/stderr and exit code.
func runCommand(t *testing.T, timeout time.Duration, dir string, stdin io.Reader, name string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if stdin != nil {
		cmd.Stdin = stdin
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if err == nil {
		return stdout, stderr, 0
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Logf("command timed out after %v: %s %s", timeout, name, strings.Join(args, " "))
		return stdout, stderr, 124
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout, stderr, exitErr.ExitCode()
	}

	t.Fatalf("failed to execute command %s %v: %v", name, args, err)
	return stdout, stderr, -1
}

// runDocker executes a `docker run` command with the default timeout.
func runDocker(t *testing.T, timeout time.Duration, stdin io.Reader, dockerArgs ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	args := append([]string{"run"}, dockerArgs...)
	return runCommand(t, timeout, "", stdin, "docker", args...)
}

// createTestWorkspace initializes a temporary directory structured for testing.
func createTestWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatalf("failed to chmod test workspace: %v", err)
	}

	authDir := filepath.Join(dir, "src", "auth")
	if err := os.MkdirAll(authDir, 0777); err != nil {
		t.Fatalf("failed to create src/auth dir: %v", err)
	}

	apiDir := filepath.Join(dir, "pkg", "api")
	if err := os.MkdirAll(apiDir, 0777); err != nil {
		t.Fatalf("failed to create pkg/api dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(authDir, "auth.go"), []byte("package auth\n"), 0666); err != nil {
		t.Fatalf("failed to write auth.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "api.go"), []byte("package api\n"), 0666); err != nil {
		t.Fatalf("failed to write api.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test Project\n"), 0666); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}

	return dir
}

// calculateSHA256 returns the hex-encoded SHA-256 checksum of a file.
func calculateSHA256(t *testing.T, filePath string) string {
	t.Helper()
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file for checksum %s: %v", filePath, err)
	}
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
