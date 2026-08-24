//go:build integration

package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// getSetupWifScriptPath returns the absolute path to github-actions/scripts/setup-wif.sh.
func getSetupWifScriptPath(t *testing.T) string {
	t.Helper()
	repoRoot := getRepoRoot(t)
	return filepath.Join(repoRoot, "github-actions", "scripts", "setup-wif.sh")
}

// Scenario 1: Verify setup-wif.sh script existence and executable permissions (REQ-0008.13)
func TestSetupWifScriptExistenceAndPermissions(t *testing.T) {
	scriptPath := getSetupWifScriptPath(t)
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("setup-wif.sh does not exist at %s: %v", scriptPath, err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("setup-wif.sh at %s is not executable (mode: %v)", scriptPath, info.Mode())
	}
}

// Scenario 2: Verify setup-wif.sh passes bash syntax check (bash -n)
func TestSetupWifScriptBashSyntax(t *testing.T) {
	scriptPath := getSetupWifScriptPath(t)
	cmd := exec.Command("bash", "-n", scriptPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n %s failed: %v\nOutput:\n%s", scriptPath, err, string(out))
	}
}

// Scenario 3: Verify help flags display usage and exit code 0
func TestSetupWifScriptHelpFlags(t *testing.T) {
	scriptPath := getSetupWifScriptPath(t)

	tests := []struct {
		name string
		flag string
	}{
		{"LongHelp", "--help"},
		{"ShortHelp", "-h"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCommand(t, 0, "", nil, scriptPath, tc.flag)
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

// Scenario 4: Verify mandatory parameter validation errors (REQ-0002.3)
func TestSetupWifScriptValidationErrors(t *testing.T) {
	scriptPath := getSetupWifScriptPath(t)

	tests := []struct {
		name string
		args []string
		env  []string
	}{
		{
			name: "NoArguments",
			args: []string{},
		},
		{
			name: "OnlyProjectFlag",
			args: []string{"--project=my-test-project"},
		},
		{
			name: "OnlyRepoFlag",
			args: []string{"--repo=my-org/my-repo"},
		},
		{
			name: "UnknownFlag",
			args: []string{"--invalid-flag"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(scriptPath, tc.args...)
			if len(tc.env) > 0 {
				cmd.Env = append(os.Environ(), tc.env...)
			}
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Errorf("expected command to fail with non-zero exit code, but succeeded: %s", string(out))
			}
		})
	}
}

// Scenario 5: Verify CLI flags in --dry-run mode (REQ-0002.3, REQ-0008.7 - REQ-0008.12)
func TestSetupWifScriptFlagsDryRun(t *testing.T) {
	scriptPath := getSetupWifScriptPath(t)

	t.Run("LongFlags", func(t *testing.T) {
		stdout, stderr, code := runCommand(t, 0, "", nil, scriptPath,
			"--project=custom-proj",
			"--repo=custom-org/custom-repo",
			"--pool=custom-pool",
			"--provider=custom-provider",
			"--sa=custom-sa",
			"--dry-run",
		)
		if code != 0 {
			t.Fatalf("expected code 0, got %d. stderr: %s", code, stderr)
		}
		verifyDryRunOutput(t, stdout, "custom-proj", "custom-org/custom-repo", "custom-pool", "custom-provider", "custom-sa")
	})

	t.Run("ShortFlags", func(t *testing.T) {
		stdout, stderr, code := runCommand(t, 0, "", nil, scriptPath,
			"-p", "short-proj",
			"-r", "short-org/short-repo",
			"--dry-run",
		)
		if code != 0 {
			t.Fatalf("expected code 0, got %d. stderr: %s", code, stderr)
		}
		// Uses defaults for pool, provider, and sa
		verifyDryRunOutput(t, stdout, "short-proj", "short-org/short-repo", "codemender-pool", "codemender-provider", "codemender-runner")
	})
}

// Scenario 6: Verify Positional Arguments in --dry-run mode (REQ-0002.3)
func TestSetupWifScriptPositionalArgsDryRun(t *testing.T) {
	scriptPath := getSetupWifScriptPath(t)

	stdout, stderr, code := runCommand(t, 0, "", nil, scriptPath,
		"--dry-run",
		"pos-proj",
		"pos-org/pos-repo",
		"pos-pool",
		"pos-provider",
		"pos-sa",
	)
	if code != 0 {
		t.Fatalf("expected code 0, got %d. stderr: %s", code, stderr)
	}
	verifyDryRunOutput(t, stdout, "pos-proj", "pos-org/pos-repo", "pos-pool", "pos-provider", "pos-sa")
}

// Scenario 7: Verify Environment Variables in --dry-run mode (REQ-0002.3)
func TestSetupWifScriptEnvVarsDryRun(t *testing.T) {
	scriptPath := getSetupWifScriptPath(t)

	cmd := exec.Command(scriptPath)
	cmd.Env = append(os.Environ(),
		"PROJECT_ID=env-proj",
		"GITHUB_REPO=env-org/env-repo",
		"POOL_NAME=env-pool",
		"PROVIDER_NAME=env-provider",
		"SA_NAME=env-sa",
		"DRY_RUN=true",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed with env vars: %v\nOutput:\n%s", err, string(out))
	}
	verifyDryRunOutput(t, string(out), "env-proj", "env-org/env-repo", "env-pool", "env-provider", "env-sa")
}

// verifyDryRunOutput checks for required gcloud commands and secret instructions.
func verifyDryRunOutput(t *testing.T, output, projectID, repo, poolName, providerName, saName string) {
	t.Helper()

	// REQ-0008.7: Pool creation command
	expectedPoolCmd := "gcloud iam workload-identity-pools create " + poolName
	if !strings.Contains(output, expectedPoolCmd) {
		t.Errorf("missing pool creation command %q in output:\n%s", expectedPoolCmd, output)
	}

	// REQ-0008.8: Provider creation command with OIDC issuer and attribute mapping
	expectedProviderCmd := "gcloud iam workload-identity-pools providers create-oidc " + providerName
	if !strings.Contains(output, expectedProviderCmd) {
		t.Errorf("missing provider creation command %q in output:\n%s", expectedProviderCmd, output)
	}
	if !strings.Contains(output, "https://token.actions.githubusercontent.com") {
		t.Errorf("missing issuer URI in output:\n%s", output)
	}
	if !strings.Contains(output, "google.subject=assertion.sub,attribute.repository=assertion.repository") {
		t.Errorf("missing attribute mapping in output:\n%s", output)
	}

	// REQ-0008.9: Service Account creation command
	expectedSaCmd := "gcloud iam service-accounts create " + saName
	if !strings.Contains(output, expectedSaCmd) {
		t.Errorf("missing SA creation command %q in output:\n%s", expectedSaCmd, output)
	}

	// REQ-0008.10: Project IAM binding for roles/aiplatform.user
	if !strings.Contains(output, "gcloud projects add-iam-policy-binding "+projectID) {
		t.Errorf("missing project add-iam-policy-binding in output:\n%s", output)
	}
	if !strings.Contains(output, "roles/aiplatform.user") {
		t.Errorf("missing roles/aiplatform.user binding in output:\n%s", output)
	}
	expectedSaEmail := saName + "@" + projectID + ".iam.gserviceaccount.com"
	if !strings.Contains(output, "serviceAccount:"+expectedSaEmail) {
		t.Errorf("missing SA email member in project policy binding in output:\n%s", output)
	}

	// REQ-0008.11: Workload Identity User binding on SA
	if !strings.Contains(output, "gcloud iam service-accounts add-iam-policy-binding") {
		t.Errorf("missing SA add-iam-policy-binding in output:\n%s", output)
	}
	if !strings.Contains(output, "roles/iam.workloadIdentityUser") {
		t.Errorf("missing roles/iam.workloadIdentityUser in output:\n%s", output)
	}
	expectedMemberPattern := "/locations/global/workloadIdentityPools/" + poolName + "/attribute.repository/" + repo
	if !strings.Contains(output, expectedMemberPattern) {
		t.Errorf("missing principalSet member pattern %q in output:\n%s", expectedMemberPattern, output)
	}

	// REQ-0008.12: GitHub Secrets instructions
	if !strings.Contains(output, "GCP_WIF_PROVIDER: projects/") ||
		!strings.Contains(output, "/locations/global/workloadIdentityPools/"+poolName+"/providers/"+providerName) {
		t.Errorf("missing or invalid GCP_WIF_PROVIDER in instructions:\n%s", output)
	}
	if !strings.Contains(output, "GCP_SERVICE_ACCOUNT: "+expectedSaEmail) {
		t.Errorf("missing or invalid GCP_SERVICE_ACCOUNT in instructions:\n%s", output)
	}
}
