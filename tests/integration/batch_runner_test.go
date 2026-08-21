//go:build integration

package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Scenario 1: Mandatory Subcommand Requirement (REQ-0003, REQ-0012)
func TestSubcommandMandatory(t *testing.T) {
	image := getImageName()
	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", image)

	assert.Equal(t, 2, exitCode, "empty invocation must exit with code 2")
	assert.Contains(t, stderr, "missing subcommand")
	assert.Contains(t, stderr, "Usage:")
}

// Scenario 2: Error on Unrecognized Subcommand (REQ-0003)
func TestSubcommandUnrecognized(t *testing.T) {
	image := getImageName()
	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", image, "invalid-subcommand")

	assert.Equal(t, 2, exitCode, "unrecognized subcommand must exit with code 2")
	assert.Contains(t, stderr, "unrecognized subcommand")
}

// Scenario 3: Strict Subcommand Dispatch & cm Prefix Rejection (REQ-0008)
func TestSubcommandRejectCMPrefix(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "cm", "find", "non/existent/path")

	assert.Equal(t, 2, exitCode, "invocation with 'cm' prefix must exit with code 2")
	assert.Contains(t, stderr, "unrecognized subcommand 'cm'")
}

// Scenario 4: Default Full Repository Scan and Flag Forwarding (REQ-0004, REQ-0005)
func TestFindDefaultWorkspace(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	stdout, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "find", "--", "--help")

	combined := stdout + "\n" + stderr
	assert.Contains(t, combined, "Usage:")
	assert.Equal(t, 0, exitCode)
}

// Scenario 5: Scoped Sub-Path Scan Target Resolution (REQ-0004)
func TestFindScopedSubPath(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	stdout, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "find", "src/auth", "--", "--help")

	combined := stdout + "\n" + stderr
	assert.Contains(t, combined, "Usage:")
	assert.Equal(t, 0, exitCode)
}

// Scenario 6: Non-Existent Sub-Path Error Handling (REQ-0004)
func TestFindNonExistentPath(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "find", "non/existent/path")

	assert.Equal(t, 2, exitCode, "non-existent path must exit with code 2")
	assert.Contains(t, stderr, "scan target path does not exist in workspace")
}

// Scenario 7: Build-Time Configuration Pre-Initialization & Headless Defaults (REQ-0002)
func TestPreinitializedConfigAndHeadlessDefaults(t *testing.T) {
	image := getImageName()

	stdoutList, stderrList, exitList := runDocker(t, 10*time.Second, nil, "--rm", "--entrypoint", "ls", image, "-la", "/home/codemender/.codemender")
	require.Equal(t, 0, exitList, "ls /home/codemender/.codemender failed: %s", stderrList)
	assert.Contains(t, stdoutList, "config.yaml")

	stdoutCat, stderrCat, exitCat := runDocker(t, 10*time.Second, nil, "--rm", "--entrypoint", "cat", image, "/home/codemender/.codemender/config.yaml")
	require.Equal(t, 0, exitCat, "cat config.yaml failed: %s", stderrCat)

	assert.Contains(t, stdoutCat, ".rs", "config.yaml must include .rs extension")
	assert.True(t, strings.Contains(stdoutCat, `format: "json"`) || strings.Contains(stdoutCat, "format: json"), "format must be json")
	assert.Contains(t, stdoutCat, "confirm_commands: false")
	assert.Contains(t, stdoutCat, "confirm_writes: false")
}

// Scenario 8: Strict Unprivileged Userspace Execution (REQ-0010)
func TestUnprivilegedUserspaceExecution(t *testing.T) {
	image := getImageName()

	uidOut, _, exitUID := runDocker(t, 10*time.Second, nil, "--rm", "--entrypoint", "id", image, "-u")
	gidOut, _, exitGID := runDocker(t, 10*time.Second, nil, "--rm", "--entrypoint", "id", image, "-g")

	require.Equal(t, 0, exitUID)
	require.Equal(t, 0, exitGID)

	uid := strings.TrimSpace(uidOut)
	gid := strings.TrimSpace(gidOut)

	assert.NotEqual(t, "0", uid, "UID must be non-root")
	assert.NotEqual(t, "0", gid, "GID must be non-root")
}

// Scenario 9: Host Runner Script Verification (REQ-0001, REQ-0011)
func TestHostRunnerScript(t *testing.T) {
	repoRoot := getRepoRoot(t)
	runnerPath := filepath.Join(repoRoot, "bin", "cm-runner")
	ws := createTestWorkspace(t)

	_, stderr, exitCode := runCommand(t, 10*time.Second, ws, nil, runnerPath, "find", "non/existent/path")

	assert.Equal(t, 2, exitCode, "./bin/cm-runner must exit with code 2 on invalid path")
	assert.Contains(t, stderr, "scan target path does not exist in workspace")
}

// Scenario 10: Signal Forwarding and Clean Termination (REQ-0012)
func TestSignalForwardingSIGTERM(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	// Launch container in detached mode
	stdout, stderr, exitCode := runCommand(t, 10*time.Second, "", nil, "docker", "run", "-d", "--rm", "-v", ws+":/workspace", image, "find")
	require.Equal(t, 0, exitCode, "failed to start detached container: %s", stderr)
	containerID := strings.TrimSpace(stdout)
	defer func() {
		_ = exec.Command("docker", "rm", "-f", containerID).Run()
	}()

	time.Sleep(500 * time.Millisecond)

	startTime := time.Now()
	// Send SIGTERM to container
	killOut, killErr, killExit := runCommand(t, 5*time.Second, "", nil, "docker", "kill", "--signal=SIGTERM", containerID)
	t.Logf("kill output: %s %s (exit %d)", killOut, killErr, killExit)

	// Poll until container has stopped
	stopped := false
	for i := 0; i < 5; i++ {
		psOut, _, _ := runCommand(t, 2*time.Second, "", nil, "docker", "ps", "-q", "--no-trunc")
		if !strings.Contains(psOut, containerID) {
			stopped = true
			break
		}
		time.Sleep(1 * time.Second)
	}

	duration := time.Since(startTime)
	assert.True(t, stopped, "container must terminate cleanly after SIGTERM")
	assert.LessOrEqual(t, duration, 4*time.Second, "termination must complete within 4 seconds")
}

// Scenario 11: Shell Subcommand TTY Enforcement (REQ-0009)
func TestShellSubcommandTTYEnforcement(t *testing.T) {
	image := getImageName()
	repoRoot := getRepoRoot(t)
	shellScriptPath := filepath.Join(repoRoot, "bin", "cm-shell")
	ws := createTestWorkspace(t)

	_, stderrDocker, exitDocker := runDocker(t, 10*time.Second, nil, "--rm", image, "shell")
	assert.Equal(t, 2, exitDocker)
	assert.Contains(t, stderrDocker, "requires an interactive terminal")

	_, stderrScript, exitScript := runCommand(t, 10*time.Second, ws, strings.NewReader(""), shellScriptPath)
	assert.Equal(t, 2, exitScript)
	assert.Contains(t, stderrScript, "requires an interactive terminal")
}

// Scenario 12: Init Subcommand Execution & In-Place Mutation (REQ-0002)
func TestInitSubcommandAndMutation(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	// Test 1: Help flag
	stdoutHelp, stderrHelp, exitHelp := runDocker(t, 10*time.Second, nil, "--rm", image, "init", "--help")
	require.Equal(t, 0, exitHelp, "init --help failed: %s", stderrHelp)
	assert.Contains(t, stdoutHelp+stderrHelp, "cm-runner init")

	// Test 2: In-place mutation of $HOME/.codemender/config.yaml
	mockHome := filepath.Join(ws, "mock-home")
	dotCM := filepath.Join(mockHome, ".codemender")
	require.NoError(t, os.MkdirAll(dotCM, 0777))

	sampleConfig := `scan:
  extensions:
    include:
      - ".go"
      - ".py"
output:
  format: "table"
tools:
  confirm_commands: true
  confirm_writes: true
`
	configPath := filepath.Join(dotCM, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(sampleConfig), 0666))

	_, stderrMutate, exitMutate := runDocker(t, 10*time.Second, nil, "--rm", "-e", "HOME=/mock-home", "-v", mockHome+":/mock-home", image, "init")
	require.Equal(t, 0, exitMutate, "init execution failed: %s", stderrMutate)

	mutatedContentBytes, err := os.ReadFile(configPath)
	require.NoError(t, err)
	mutatedContent := string(mutatedContentBytes)

	assert.Contains(t, mutatedContent, ".rs", "mutated config must contain .rs extension")
	assert.Contains(t, mutatedContent, "confirm_commands: false")
	assert.Contains(t, mutatedContent, "confirm_writes: false")
}
