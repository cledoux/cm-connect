//go:build integration

package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Scenario 1: Mandatory Subcommand Requirement (REQ-0003, REQ-0012)
func TestSubcommandMandatory(t *testing.T) {
	image := getImageName()
	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", image)

	if exitCode != 2 {
		t.Errorf("empty invocation exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr, "missing subcommand") {
		t.Errorf("stderr %q does not contain %q", stderr, "missing subcommand")
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr %q does not contain %q", stderr, "Usage:")
	}
}

// Scenario 2: Error on Unrecognized Subcommand (REQ-0003)
func TestSubcommandUnrecognized(t *testing.T) {
	image := getImageName()
	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", image, "invalid-subcommand")

	if exitCode != 2 {
		t.Errorf("unrecognized subcommand exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr, "unrecognized subcommand") {
		t.Errorf("stderr %q does not contain %q", stderr, "unrecognized subcommand")
	}
}

// Scenario 3: Strict Subcommand Dispatch & cm Prefix Rejection (REQ-0008)
func TestSubcommandRejectCMPrefix(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "cm", "find", "non/existent/path")

	if exitCode != 2 {
		t.Errorf("cm prefix invocation exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr, "unrecognized subcommand 'cm'") {
		t.Errorf("stderr %q does not contain %q", stderr, "unrecognized subcommand 'cm'")
	}
}

// Scenario 4: Default Full Repository Scan and Flag Forwarding (REQ-0004, REQ-0005)
func TestFindDefaultWorkspace(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	stdout, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "find", "--", "--help")

	if exitCode != 0 {
		t.Errorf("find --help exit code = %d, want 0", exitCode)
	}
	combined := stdout + "\n" + stderr
	if !strings.Contains(combined, "Usage:") {
		t.Errorf("output %q does not contain %q", combined, "Usage:")
	}
}

// Scenario 5: Scoped Sub-Path Scan Target Resolution (REQ-0004)
func TestFindScopedSubPath(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	stdout, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "find", "src/auth", "--", "--help")

	if exitCode != 0 {
		t.Errorf("find src/auth --help exit code = %d, want 0", exitCode)
	}
	combined := stdout + "\n" + stderr
	if !strings.Contains(combined, "Usage:") {
		t.Errorf("output %q does not contain %q", combined, "Usage:")
	}
}

// Scenario 6: Non-Existent Sub-Path Error Handling (REQ-0004)
func TestFindNonExistentPath(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	_, stderr, exitCode := runDocker(t, 10*time.Second, nil, "--rm", "-v", ws+":/workspace", image, "find", "non/existent/path")

	if exitCode != 2 {
		t.Errorf("non-existent path exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr, "scan target path does not exist in workspace") {
		t.Errorf("stderr %q does not contain %q", stderr, "scan target path does not exist in workspace")
	}
}

// Scenario 7: Build-Time Configuration Pre-Initialization & Headless Defaults (REQ-0002)
func TestPreinitializedConfigAndHeadlessDefaults(t *testing.T) {
	image := getImageName()

	stdoutList, stderrList, exitList := runDocker(t, 10*time.Second, nil, "--rm", "--entrypoint", "ls", image, "-la", "/home/codemender/.codemender")
	if exitList != 0 {
		t.Fatalf("ls /home/codemender/.codemender failed with exit %d: %s", exitList, stderrList)
	}
	if !strings.Contains(stdoutList, "config.yaml") {
		t.Errorf("ls output %q does not contain config.yaml", stdoutList)
	}

	stdoutCat, stderrCat, exitCat := runDocker(t, 10*time.Second, nil, "--rm", "--entrypoint", "cat", image, "/home/codemender/.codemender/config.yaml")
	if exitCat != 0 {
		t.Fatalf("cat config.yaml failed with exit %d: %s", exitCat, stderrCat)
	}

	if !strings.Contains(stdoutCat, ".rs") {
		t.Errorf("config.yaml %q missing .rs extension", stdoutCat)
	}
	if !strings.Contains(stdoutCat, `format: "json"`) && !strings.Contains(stdoutCat, "format: json") {
		t.Errorf("config.yaml %q missing json format", stdoutCat)
	}
	if !strings.Contains(stdoutCat, "confirm_commands: false") {
		t.Errorf("config.yaml %q missing confirm_commands: false", stdoutCat)
	}
	if !strings.Contains(stdoutCat, "confirm_writes: false") {
		t.Errorf("config.yaml %q missing confirm_writes: false", stdoutCat)
	}
}

// Scenario 8: Strict Unprivileged Userspace Execution (REQ-0010)
func TestUnprivilegedUserspaceExecution(t *testing.T) {
	image := getImageName()

	uidOut, _, exitUID := runDocker(t, 10*time.Second, nil, "--rm", "--entrypoint", "id", image, "-u")
	gidOut, _, exitGID := runDocker(t, 10*time.Second, nil, "--rm", "--entrypoint", "id", image, "-g")

	if exitUID != 0 || exitGID != 0 {
		t.Fatalf("failed to query container UID/GID (exit UID=%d, GID=%d)", exitUID, exitGID)
	}

	uid := strings.TrimSpace(uidOut)
	gid := strings.TrimSpace(gidOut)

	if uid == "0" || uid == "" {
		t.Errorf("container UID = %q, want non-root (> 0)", uid)
	}
	if gid == "0" || gid == "" {
		t.Errorf("container GID = %q, want non-root (> 0)", gid)
	}
}

// Scenario 9: Host Runner Script Verification (REQ-0001, REQ-0011)
func TestHostRunnerScript(t *testing.T) {
	repoRoot := getRepoRoot(t)
	runnerPath := filepath.Join(repoRoot, "bin", "cm-runner")
	ws := createTestWorkspace(t)

	_, stderr, exitCode := runCommand(t, 10*time.Second, ws, nil, runnerPath, "find", "non/existent/path")

	if exitCode != 2 {
		t.Errorf("./bin/cm-runner exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr, "scan target path does not exist in workspace") {
		t.Errorf("stderr %q does not contain %q", stderr, "scan target path does not exist in workspace")
	}
}

// Scenario 10: Signal Forwarding and Clean Termination (REQ-0012)
func TestSignalForwardingSIGTERM(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	stdout, stderr, exitCode := runCommand(t, 10*time.Second, "", nil, "docker", "run", "-d", "--rm", "-v", ws+":/workspace", image, "find")
	if exitCode != 0 {
		t.Fatalf("failed to start detached container: %s", stderr)
	}
	containerID := strings.TrimSpace(stdout)
	defer func() {
		_ = exec.Command("docker", "rm", "-f", containerID).Run()
	}()

	time.Sleep(500 * time.Millisecond)

	startTime := time.Now()
	killOut, killErr, killExit := runCommand(t, 5*time.Second, "", nil, "docker", "kill", "--signal=SIGTERM", containerID)
	t.Logf("kill output: %s %s (exit %d)", killOut, killErr, killExit)

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
	if !stopped {
		t.Errorf("container failed to terminate after SIGTERM")
	}
	if duration > 4*time.Second {
		t.Errorf("termination duration = %v, want <= 4s", duration)
	}
}

// Scenario 11: Shell Subcommand TTY Enforcement (REQ-0009)
func TestShellSubcommandTTYEnforcement(t *testing.T) {
	image := getImageName()
	repoRoot := getRepoRoot(t)
	shellScriptPath := filepath.Join(repoRoot, "bin", "cm-shell")
	ws := createTestWorkspace(t)

	_, stderrDocker, exitDocker := runDocker(t, 10*time.Second, nil, "--rm", image, "shell")
	if exitDocker != 2 {
		t.Errorf("shell without TTY exit code = %d, want 2", exitDocker)
	}
	if !strings.Contains(stderrDocker, "requires an interactive terminal") {
		t.Errorf("stderr %q does not contain %q", stderrDocker, "requires an interactive terminal")
	}

	_, stderrScript, exitScript := runCommand(t, 10*time.Second, ws, strings.NewReader(""), shellScriptPath)
	if exitScript != 2 {
		t.Errorf("./bin/cm-shell without TTY exit code = %d, want 2", exitScript)
	}
	if !strings.Contains(stderrScript, "requires an interactive terminal") {
		t.Errorf("stderr %q does not contain %q", stderrScript, "requires an interactive terminal")
	}
}

// Scenario 12: Init Subcommand Execution & In-Place Mutation (REQ-0002)
func TestInitSubcommandAndMutation(t *testing.T) {
	image := getImageName()
	ws := createTestWorkspace(t)

	// Test 1: Help flag
	stdoutHelp, stderrHelp, exitHelp := runDocker(t, 10*time.Second, nil, "--rm", image, "init", "--help")
	if exitHelp != 0 {
		t.Fatalf("init --help failed with exit %d: %s", exitHelp, stderrHelp)
	}
	if !strings.Contains(stdoutHelp+stderrHelp, "cm-runner init") {
		t.Errorf("init --help output %q missing 'cm-runner init'", stdoutHelp+stderrHelp)
	}

	// Test 2: In-place mutation of $HOME/.codemender/config.yaml
	mockHome := filepath.Join(ws, "mock-home")
	dotCM := filepath.Join(mockHome, ".codemender")
	if err := os.MkdirAll(dotCM, 0777); err != nil {
		t.Fatalf("failed to create mock .codemender dir: %v", err)
	}

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
	if err := os.WriteFile(configPath, []byte(sampleConfig), 0666); err != nil {
		t.Fatalf("failed to write mock config.yaml: %v", err)
	}

	_, stderrMutate, exitMutate := runDocker(t, 10*time.Second, nil, "--rm", "-e", "HOME=/mock-home", "-v", mockHome+":/mock-home", image, "init")
	if exitMutate != 0 {
		t.Fatalf("init execution failed with exit %d: %s", exitMutate, stderrMutate)
	}

	mutatedContentBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read mutated config.yaml: %v", err)
	}
	mutatedContent := string(mutatedContentBytes)

	if !strings.Contains(mutatedContent, ".rs") {
		t.Errorf("mutated config %q missing .rs extension", mutatedContent)
	}
	if !strings.Contains(mutatedContent, "confirm_commands: false") {
		t.Errorf("mutated config %q missing confirm_commands: false", mutatedContent)
	}
	if !strings.Contains(mutatedContent, "confirm_writes: false") {
		t.Errorf("mutated config %q missing confirm_writes: false", mutatedContent)
	}
}
