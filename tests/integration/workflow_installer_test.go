//go:build integration

package integration_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// getInstallScriptPath returns the absolute path to github-actions/install.sh.
func getInstallScriptPath(t *testing.T) string {
	t.Helper()
	repoRoot := getRepoRoot(t)
	return filepath.Join(repoRoot, "github-actions", "install.sh")
}

// createGitRepoWithRemote initializes a temporary git repository with a configured origin remote.
func createGitRepoWithRemote(t *testing.T, remoteURL string) string {
	t.Helper()
	dir := t.TempDir()
	stdout, stderr, exitCode := runCommand(t, 5*time.Second, dir, nil, "git", "init")
	if exitCode != 0 {
		t.Fatalf("git init failed (exit %d): stdout=%s, stderr=%s", exitCode, stdout, stderr)
	}
	if remoteURL != "" {
		stdout, stderr, exitCode = runCommand(t, 5*time.Second, dir, nil, "git", "remote", "add", "origin", remoteURL)
		if exitCode != 0 {
			t.Fatalf("git remote add failed (exit %d): stdout=%s, stderr=%s", exitCode, stdout, stderr)
		}
	}
	return dir
}

// Scenario 1: Verify github-actions/install.sh location, permissions, and repo isolation (REQ-0008, REQ-0008.6).
func TestInstallerScriptPermissionsAndIsolation(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := getInstallScriptPath(t)

	info, err := os.Stat(installScript)
	if err != nil {
		t.Fatalf("installer script not found at %s: %v", installScript, err)
	}

	mode := info.Mode()
	if mode.Perm()&0111 == 0 {
		t.Errorf("installer script %s is not executable (mode: %v)", installScript, mode)
	}

	// Verify isolation: .github/workflows/codemender.yml MUST NOT exist in cm-connect
	activeWorkflowPath := filepath.Join(repoRoot, ".github", "workflows", "codemender.yml")
	if _, err := os.Stat(activeWorkflowPath); err == nil {
		t.Errorf("isolated workflow template must NOT exist at %s in cm-connect", activeWorkflowPath)
	}
}

// Scenario 2: Verify install.sh passes bash syntax check (bash -n).
func TestInstallerScriptBashSyntax(t *testing.T) {
	installScript := getInstallScriptPath(t)
	cmd := exec.Command("bash", "-n", installScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n %s failed: %v\nOutput:\n%s", installScript, err, string(out))
	}
}

// Scenario 3: Verify help flags display usage and exit code 0.
func TestInstallerScriptHelpFlags(t *testing.T) {
	installScript := getInstallScriptPath(t)

	tests := []struct {
		name string
		flag string
	}{
		{"LongHelp", "--help"},
		{"ShortHelp", "-h"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCommand(t, 5*time.Second, "", nil, installScript, tc.flag)
			if code != 0 {
				t.Errorf("expected exit code 0 for %s, got %d. stderr: %s", tc.flag, code, stderr)
			}
			combined := stdout + "\n" + stderr
			if !strings.Contains(strings.ToLower(combined), "usage:") {
				t.Errorf("expected usage info in output for %s, got: %s", tc.flag, combined)
			}
		})
	}
}

// Scenario 4: Verify validation errors on missing or invalid arguments (REQ-0008.1).
func TestInstallerScriptValidationErrors(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := getInstallScriptPath(t)

	t.Run("MissingArguments", func(t *testing.T) {
		stdout, stderr, exitCode := runCommand(t, 5*time.Second, repoRoot, nil, installScript)
		if exitCode != 1 {
			t.Errorf("exitCode = %d, want 1 for missing argument\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
		}
		combined := stdout + stderr
		if !strings.Contains(strings.ToLower(combined), "usage") {
			t.Errorf("expected usage message when missing argument, got: %s", combined)
		}
	})

	t.Run("NonExistentDirectory", func(t *testing.T) {
		nonExistentPath := filepath.Join(t.TempDir(), "non-existent-dir-999")
		stdout, stderr, exitCode := runCommand(t, 5*time.Second, repoRoot, nil, installScript, nonExistentPath)
		if exitCode != 1 {
			t.Errorf("exitCode = %d, want 1 for non-existent target\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
		}
		combined := stdout + stderr
		if !strings.Contains(strings.ToLower(combined), "error") && !strings.Contains(strings.ToLower(combined), "usage") {
			t.Errorf("expected error or usage message, got: %s", combined)
		}
	})

	t.Run("TargetIsRegularFile", func(t *testing.T) {
		tempFile := filepath.Join(t.TempDir(), "target-is-a-file.txt")
		if err := os.WriteFile(tempFile, []byte("file content"), 0644); err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		stdout, stderr, exitCode := runCommand(t, 5*time.Second, repoRoot, nil, installScript, tempFile)
		if exitCode != 1 {
			t.Errorf("exitCode = %d, want 1 when target is a file\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
		}
		combined := stdout + stderr
		if !strings.Contains(strings.ToLower(combined), "directory") && !strings.Contains(strings.ToLower(combined), "error") {
			t.Errorf("expected directory error message, got: %s", combined)
		}
	})

	t.Run("UnknownFlag", func(t *testing.T) {
		targetDir := t.TempDir()
		stdout, stderr, exitCode := runCommand(t, 5*time.Second, repoRoot, nil, installScript, "--unknown-flag", targetDir)
		if exitCode != 1 {
			t.Errorf("exitCode = %d, want 1 for unknown flag\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
		}
	})
}

// Scenario 5: Verify Fast Installation with --skip-build and Git Remote Slug Auto-Discovery (REQ-0008.1, REQ-0008.3, REQ-0008.4, REQ-0008.5).
func TestInstallerScriptSkipBuild_TargetSlugDiscovery(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := getInstallScriptPath(t)

	tests := []struct {
		name          string
		remoteURL     string
		expectedSlug  string
		expectedImage string
	}{
		{
			name:          "SSH Remote URL",
			remoteURL:     "git@github.com:my-org/my-app.git",
			expectedSlug:  "my-org/my-app",
			expectedImage: "ghcr.io/my-org/my-app/cm-runner:latest",
		},
		{
			name:          "HTTPS Remote URL with .git",
			remoteURL:     "https://github.com/octocat/hello-world.git",
			expectedSlug:  "octocat/hello-world",
			expectedImage: "ghcr.io/octocat/hello-world/cm-runner:latest",
		},
		{
			name:          "HTTPS Remote URL without .git",
			remoteURL:     "https://github.com/acme-corp/service-backend",
			expectedSlug:  "acme-corp/service-backend",
			expectedImage: "ghcr.io/acme-corp/service-backend/cm-runner:latest",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targetDir := createGitRepoWithRemote(t, tc.remoteURL)

			stdout, stderr, exitCode := runCommand(t, 10*time.Second, repoRoot, nil, installScript, "--skip-build", targetDir)
			if exitCode != 0 {
				t.Fatalf("installer script failed with exit code %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
			}

			// Verify target directories exist
			workflowsDir := filepath.Join(targetDir, ".github", "workflows")
			if info, err := os.Stat(workflowsDir); err != nil || !info.IsDir() {
				t.Errorf("expected directory %s to exist, err: %v", workflowsDir, err)
			}

			scriptsDir := filepath.Join(targetDir, ".github", "scripts")
			if info, err := os.Stat(scriptsDir); err != nil || !info.IsDir() {
				t.Errorf("expected directory %s to exist, err: %v", scriptsDir, err)
			}

			// Verify workflow file exists and is templated with the discovered image tag
			workflowPath := filepath.Join(workflowsDir, "codemender.yml")
			workflowContent, err := os.ReadFile(workflowPath)
			if err != nil {
				t.Fatalf("failed to read installed workflow file: %v", err)
			}
			workflowStr := string(workflowContent)

			if !strings.Contains(workflowStr, tc.expectedImage) {
				t.Errorf("expected workflow to contain templated image tag %q, but got:\n%s", tc.expectedImage, workflowStr)
			}
			// Verify hardcoded source repository default was replaced
			if strings.Contains(workflowStr, "ghcr.io/cledoux/cm-runner:latest") && tc.expectedImage != "ghcr.io/cledoux/cm-runner:latest" {
				t.Errorf("expected workflow not to contain old image tag ghcr.io/cledoux/cm-runner:latest")
			}

			// Verify helper scripts exist and are executable
			helperScripts := []string{
				"filter_findings.jq",
				"publish_comments.py",
				"setup-wif.sh",
			}
			for _, scriptName := range helperScripts {
				dstPath := filepath.Join(scriptsDir, scriptName)
				info, err := os.Stat(dstPath)
				if err != nil {
					t.Errorf("expected helper script %s to exist: %v", dstPath, err)
					continue
				}
				if scriptName == "publish_comments.py" || scriptName == "setup-wif.sh" {
					if info.Mode().Perm()&0111 == 0 {
						t.Errorf("expected helper script %s to be executable, got mode %v", dstPath, info.Mode())
					}
				}
				// Verify script content matches source
				srcPath := filepath.Join(repoRoot, "github-actions", "scripts", scriptName)
				if calculateSHA256(t, srcPath) != calculateSHA256(t, dstPath) {
					t.Errorf("checksum mismatch for %s", scriptName)
				}
			}

			// Verify next-step instructions in stdout (REQ-0008.5)
			if !strings.Contains(stdout, "setup-wif.sh") {
				t.Errorf("expected stdout to mention setup-wif.sh, got: %s", stdout)
			}
			if !strings.Contains(stdout, "GCP_WIF_PROVIDER") && !strings.Contains(stdout, "secrets") {
				t.Errorf("expected stdout to mention secrets, got: %s", stdout)
			}
		})
	}
}

// Scenario 6: Verify Custom Image Override via --image flag (REQ-0008.4).
func TestInstallerScriptCustomImageFlag(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := getInstallScriptPath(t)

	tests := []struct {
		name        string
		args        []string
		customImage string
	}{
		{
			name:        "SeparateFlagValue",
			args:        []string{"--skip-build", "--image", "custom-registry.io/my-team/custom-cm:v2.1.0"},
			customImage: "custom-registry.io/my-team/custom-cm:v2.1.0",
		},
		{
			name:        "EqualsFlagValue",
			args:        []string{"--skip-build", "--image=docker.io/enterprise/cm-runner:stable"},
			customImage: "docker.io/enterprise/cm-runner:stable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targetDir := createGitRepoWithRemote(t, "git@github.com:orig-org/orig-app.git")
			allArgs := append(tc.args, targetDir)

			stdout, stderr, exitCode := runCommand(t, 10*time.Second, repoRoot, nil, installScript, allArgs...)
			if exitCode != 0 {
				t.Fatalf("installer script failed with exit code %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
			}

			workflowPath := filepath.Join(targetDir, ".github", "workflows", "codemender.yml")
			workflowContent, err := os.ReadFile(workflowPath)
			if err != nil {
				t.Fatalf("failed to read installed workflow file: %v", err)
			}
			workflowStr := string(workflowContent)

			if !strings.Contains(workflowStr, tc.customImage) {
				t.Errorf("expected workflow to contain custom image %q, got:\n%s", tc.customImage, workflowStr)
			}
		})
	}
}

// Scenario 7: Verify Explicit Repository Slug Override via --repo flag (REQ-0008.1, REQ-0008.4).
func TestInstallerScriptExplicitRepoFlag(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := getInstallScriptPath(t)

	tests := []struct {
		name         string
		args         []string
		explicitRepo string
		expectedTag  string
	}{
		{
			name:         "SeparateFlagValue",
			args:         []string{"--skip-build", "--repo", "override-org/override-repo"},
			explicitRepo: "override-org/override-repo",
			expectedTag:  "ghcr.io/override-org/override-repo/cm-runner:latest",
		},
		{
			name:         "EqualsFlagValue",
			args:         []string{"--skip-build", "--repo=flag-org/flag-repo"},
			explicitRepo: "flag-org/flag-repo",
			expectedTag:  "ghcr.io/flag-org/flag-repo/cm-runner:latest",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Even if git remote says "git@github.com:initial-org/initial-repo.git"
			targetDir := createGitRepoWithRemote(t, "git@github.com:initial-org/initial-repo.git")
			allArgs := append(tc.args, targetDir)

			stdout, stderr, exitCode := runCommand(t, 10*time.Second, repoRoot, nil, installScript, allArgs...)
			if exitCode != 0 {
				t.Fatalf("installer script failed with exit code %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
			}

			workflowPath := filepath.Join(targetDir, ".github", "workflows", "codemender.yml")
			workflowContent, err := os.ReadFile(workflowPath)
			if err != nil {
				t.Fatalf("failed to read installed workflow: %v", err)
			}
			workflowStr := string(workflowContent)

			if !strings.Contains(workflowStr, tc.expectedTag) {
				t.Errorf("expected workflow to contain %q, got:\n%s", tc.expectedTag, workflowStr)
			}
			if strings.Contains(workflowStr, "initial-org/initial-repo") {
				t.Errorf("expected workflow not to contain initial-org/initial-repo")
			}
		})
	}
}

// Scenario 8: Verify Idempotence and Overwriting Existing Files (REQ-0008.3).
func TestInstallerScriptOverwritesExisting(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := getInstallScriptPath(t)
	targetDir := createGitRepoWithRemote(t, "git@github.com:my-org/my-app.git")

	// Pre-populate target directory with dummy stale files
	dummyWorkflowsDir := filepath.Join(targetDir, ".github", "workflows")
	dummyScriptsDir := filepath.Join(targetDir, ".github", "scripts")
	if err := os.MkdirAll(dummyWorkflowsDir, 0755); err != nil {
		t.Fatalf("failed to create dummy workflows dir: %v", err)
	}
	if err := os.MkdirAll(dummyScriptsDir, 0755); err != nil {
		t.Fatalf("failed to create dummy scripts dir: %v", err)
	}

	staleFile := filepath.Join(dummyWorkflowsDir, "codemender.yml")
	if err := os.WriteFile(staleFile, []byte("stale workflow content"), 0644); err != nil {
		t.Fatalf("failed to create stale file: %v", err)
	}

	stdout, stderr, exitCode := runCommand(t, 10*time.Second, repoRoot, nil, installScript, "--skip-build", targetDir)
	if exitCode != 0 {
		t.Fatalf("installer script failed on existing target directory with exit code %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	// Verify the file was overwritten with templated content
	dstContent, err := os.ReadFile(staleFile)
	if err != nil {
		t.Fatalf("failed to read installed file: %v", err)
	}
	if string(dstContent) == "stale workflow content" {
		t.Errorf("stale file was not overwritten")
	}
	if !strings.Contains(string(dstContent), "ghcr.io/my-org/my-app/cm-runner:latest") {
		t.Errorf("expected overwritten workflow to contain templated image tag")
	}
}

// Scenario 9: Verify Full Automated GHCR Build and Push flow with mocked docker and gh binaries (REQ-0008.2, REQ-0008.3).
func TestInstallerScriptFullBuildAndPush_Mocked(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := getInstallScriptPath(t)
	targetDir := createGitRepoWithRemote(t, "git@github.com:prod-team/main-service.git")

	// Create a mock bin directory with mock `docker` and `gh` scripts
	mockBinDir := t.TempDir()
	logFile := filepath.Join(mockBinDir, "mock_calls.log")

	// Mock `gh` script
	mockGH := filepath.Join(mockBinDir, "gh")
	ghScriptContent := fmt.Sprintf(`#!/usr/bin/env bash
echo "gh $*" >> "%s"
if [ "$1" = "auth" ] && [ "$2" = "token" ]; then
  echo "mock_github_token_12345"
  exit 0
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo "Logged in to github.com account mockuser"
  exit 0
fi
exit 0
`, logFile)
	if err := os.WriteFile(mockGH, []byte(ghScriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock gh: %v", err)
	}

	// Mock `docker` script
	mockDocker := filepath.Join(mockBinDir, "docker")
	dockerScriptContent := fmt.Sprintf(`#!/usr/bin/env bash
echo "docker $*" >> "%s"
if [ "$1" = "login" ]; then
  # Read stdin if any
  cat > /dev/null
  exit 0
fi
if [ "$1" = "build" ]; then
  exit 0
fi
if [ "$1" = "push" ]; then
  exit 0
fi
if [ "$1" = "info" ]; then
  exit 0
fi
exit 0
`, logFile)
	if err := os.WriteFile(mockDocker, []byte(dockerScriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock docker: %v", err)
	}

	// Set PATH to prepend mockBinDir
	currentPath := os.Getenv("PATH")
	customEnv := append(os.Environ(), "PATH="+mockBinDir+":"+currentPath)

	stdout, stderr, exitCode := runCommandWithEnv(t, 10*time.Second, repoRoot, nil, customEnv, installScript, targetDir)
	if exitCode != 0 {
		t.Fatalf("installer script failed with exit code %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	// Read log file to verify docker and gh invocations
	logsBytes, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read mock logs at %s: %v", logFile, err)
	}
	logs := string(logsBytes)

	// 1. Verify GH auth token was queried
	if !strings.Contains(logs, "gh auth token") && !strings.Contains(logs, "gh auth") {
		t.Errorf("expected gh auth invocation in mock log, got:\n%s", logs)
	}

	// 2. Verify docker login ghcr.io was called
	if !strings.Contains(logs, "docker login") || !strings.Contains(logs, "ghcr.io") {
		t.Errorf("expected docker login ghcr.io in mock log, got:\n%s", logs)
	}

	// 3. Verify docker build was called with OCI label and tag
	expectedTag := "ghcr.io/prod-team/main-service/cm-runner:latest"
	expectedLabel := "org.opencontainers.image.source=https://github.com/prod-team/main-service"
	if !strings.Contains(logs, "docker build") {
		t.Errorf("expected docker build in mock log, got:\n%s", logs)
	}
	if !strings.Contains(logs, expectedTag) {
		t.Errorf("expected docker build tag %q in mock log, got:\n%s", expectedTag, logs)
	}
	if !strings.Contains(logs, expectedLabel) {
		t.Errorf("expected docker build label %q in mock log, got:\n%s", expectedLabel, logs)
	}

	// 4. Verify docker push was called with the target image tag
	if !strings.Contains(logs, "docker push "+expectedTag) {
		t.Errorf("expected docker push %q in mock log, got:\n%s", expectedTag, logs)
	}

	// 5. Verify workflow was templated
	workflowPath := filepath.Join(targetDir, ".github", "workflows", "codemender.yml")
	workflowContent, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("failed to read installed workflow file: %v", err)
	}
	if !strings.Contains(string(workflowContent), expectedTag) {
		t.Errorf("expected workflow to contain %q, got:\n%s", expectedTag, string(workflowContent))
	}
}

// Scenario 10: Verify Pre-flight validation fails gracefully when gh or docker is missing or fails without --skip-build.
func TestInstallerScriptPreflightFailureGuidance(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := getInstallScriptPath(t)
	targetDir := createGitRepoWithRemote(t, "git@github.com:prod-team/main-service.git")

	t.Run("DockerUnavailable", func(t *testing.T) {
		mockBinDir := t.TempDir()
		mockDocker := filepath.Join(mockBinDir, "docker")
		dockerScript := "#!/usr/bin/env bash\necho 'Cannot connect to Docker daemon' >&2\nexit 1\n"
		if err := os.WriteFile(mockDocker, []byte(dockerScript), 0755); err != nil {
			t.Fatalf("failed to write mock docker: %v", err)
		}
		mockGH := filepath.Join(mockBinDir, "gh")
		ghScript := "#!/usr/bin/env bash\necho 'Logged in' >&2\nexit 0\n"
		if err := os.WriteFile(mockGH, []byte(ghScript), 0755); err != nil {
			t.Fatalf("failed to write mock gh: %v", err)
		}

		currentPath := os.Getenv("PATH")
		customEnv := append(os.Environ(), "PATH="+mockBinDir+":"+currentPath)

		stdout, stderr, exitCode := runCommandWithEnv(t, 5*time.Second, repoRoot, nil, customEnv, installScript, targetDir)
		if exitCode == 0 {
			t.Errorf("expected install.sh to fail preflight when docker is unavailable, but succeeded: stdout=%s", stdout)
		}
		combined := stdout + stderr
		if !strings.Contains(strings.ToLower(combined), "docker") || !strings.Contains(strings.ToLower(combined), "skip-build") {
			t.Errorf("expected preflight troubleshooting guidance mentioning docker and skip-build, got: %s", combined)
		}
	})

	t.Run("GHUnauthenticated", func(t *testing.T) {
		mockBinDir := t.TempDir()
		mockDocker := filepath.Join(mockBinDir, "docker")
		dockerScript := "#!/usr/bin/env bash\nexit 0\n"
		if err := os.WriteFile(mockDocker, []byte(dockerScript), 0755); err != nil {
			t.Fatalf("failed to write mock docker: %v", err)
		}
		mockGH := filepath.Join(mockBinDir, "gh")
		ghScript := "#!/usr/bin/env bash\necho 'You are not logged into any GitHub hosts' >&2\nexit 1\n"
		if err := os.WriteFile(mockGH, []byte(ghScript), 0755); err != nil {
			t.Fatalf("failed to write mock gh: %v", err)
		}

		currentPath := os.Getenv("PATH")
		customEnv := append(os.Environ(), "PATH="+mockBinDir+":"+currentPath)

		stdout, stderr, exitCode := runCommandWithEnv(t, 5*time.Second, repoRoot, nil, customEnv, installScript, targetDir)
		if exitCode == 0 {
			t.Errorf("expected install.sh to fail preflight when gh is unauthenticated, but succeeded: stdout=%s", stdout)
		}
		combined := stdout + stderr
		if !strings.Contains(strings.ToLower(combined), "gh") || !strings.Contains(strings.ToLower(combined), "skip-build") {
			t.Errorf("expected preflight troubleshooting guidance mentioning gh and skip-build, got: %s", combined)
		}
	})
}

// Scenario 11: Verify pre-flight warning when gh token lacks write:packages scope.
func TestInstallerScriptMissingScopeWarning(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := getInstallScriptPath(t)
	targetDir := createGitRepoWithRemote(t, "git@github.com:prod-team/main-service.git")

	mockBinDir := t.TempDir()
	mockDocker := filepath.Join(mockBinDir, "docker")
	dockerScript := "#!/usr/bin/env bash\nexit 0\n"
	if err := os.WriteFile(mockDocker, []byte(dockerScript), 0755); err != nil {
		t.Fatalf("failed to write mock docker: %v", err)
	}
	mockGH := filepath.Join(mockBinDir, "gh")
	ghScript := `#!/usr/bin/env bash
if [ "$1" = "auth" ] && [ "$2" = "token" ]; then
  echo "mock_token"
  exit 0
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo "Logged in to github.com account testuser"
  echo "Token scopes: 'repo', 'read:org'"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(mockGH, []byte(ghScript), 0755); err != nil {
		t.Fatalf("failed to write mock gh: %v", err)
	}

	currentPath := os.Getenv("PATH")
	customEnv := append(os.Environ(), "PATH="+mockBinDir+":"+currentPath)

	stdout, stderr, exitCode := runCommandWithEnv(t, 5*time.Second, repoRoot, nil, customEnv, installScript, targetDir)
	if exitCode != 0 {
		t.Fatalf("expected script to succeed with warning, got exit code %d: %s", exitCode, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "write:packages") || !strings.Contains(combined, "gh auth refresh") {
		t.Errorf("expected warning mentioning write:packages and gh auth refresh, got: %s", combined)
	}
}

// Scenario 12: Verify docker push failure outputs actionable gh auth refresh guidance.
func TestInstallerScriptPushFailureGuidance(t *testing.T) {
	repoRoot := getRepoRoot(t)
	installScript := getInstallScriptPath(t)
	targetDir := createGitRepoWithRemote(t, "git@github.com:prod-team/main-service.git")

	mockBinDir := t.TempDir()
	mockDocker := filepath.Join(mockBinDir, "docker")
	dockerScript := `#!/usr/bin/env bash
if [ "$1" = "push" ]; then
  echo "denied: permission_denied: The token provided does not match expected scopes." >&2
  exit 1
fi
exit 0
`
	if err := os.WriteFile(mockDocker, []byte(dockerScript), 0755); err != nil {
		t.Fatalf("failed to write mock docker: %v", err)
	}
	mockGH := filepath.Join(mockBinDir, "gh")
	ghScript := `#!/usr/bin/env bash
if [ "$1" = "auth" ] && [ "$2" = "token" ]; then
  echo "mock_token"
  exit 0
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo "Logged in to github.com account testuser"
  echo "Token scopes: 'write:packages'"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(mockGH, []byte(ghScript), 0755); err != nil {
		t.Fatalf("failed to write mock gh: %v", err)
	}

	currentPath := os.Getenv("PATH")
	customEnv := append(os.Environ(), "PATH="+mockBinDir+":"+currentPath)

	stdout, stderr, exitCode := runCommandWithEnv(t, 5*time.Second, repoRoot, nil, customEnv, installScript, targetDir)
	if exitCode == 0 {
		t.Fatalf("expected install.sh to fail when docker push fails, got exit code 0: %s", stdout)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "write:packages") || !strings.Contains(combined, "gh auth refresh -s write:packages") {
		t.Errorf("expected push failure guidance mentioning gh auth refresh -s write:packages, got: %s", combined)
	}
}
