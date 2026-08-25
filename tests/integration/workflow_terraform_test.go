//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// getTerraformDir returns the absolute path to github-actions/terraform.
func getTerraformDir(t *testing.T) string {
	t.Helper()
	repoRoot := getRepoRoot(t)
	tfDir := filepath.Join(repoRoot, "github-actions", "terraform")
	info, err := os.Stat(tfDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("terraform directory does not exist at %s: %v", tfDir, err)
	}
	return tfDir
}

// Scenario 1: Verify all required Terraform module files exist
func TestTerraformModuleFilesExistence(t *testing.T) {
	tfDir := getTerraformDir(t)
	expectedFiles := []string{
		"main.tf",
		"variables.tf",
		"outputs.tf",
		"versions.tf",
		"terraform.tfvars.example",
		".gitignore",
		"README.md",
	}

	for _, file := range expectedFiles {
		filePath := filepath.Join(tfDir, file)
		info, err := os.Stat(filePath)
		if err != nil {
			t.Errorf("expected terraform file %s does not exist: %v", file, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("terraform file %s is empty", file)
		}
	}
}

// Scenario 2: Verify versions.tf declares required Terraform and Google provider constraints
func TestTerraformVersionsContent(t *testing.T) {
	tfDir := getTerraformDir(t)
	content, err := os.ReadFile(filepath.Join(tfDir, "versions.tf"))
	if err != nil {
		t.Fatalf("failed to read versions.tf: %v", err)
	}

	str := string(content)
	if !strings.Contains(str, "required_version") {
		t.Errorf("versions.tf missing required_version")
	}
	if !strings.Contains(str, "hashicorp/google") {
		t.Errorf("versions.tf missing hashicorp/google provider")
	}
}

// Scenario 3: Verify variables.tf declares project_id and github_repo
func TestTerraformVariablesContent(t *testing.T) {
	tfDir := getTerraformDir(t)
	content, err := os.ReadFile(filepath.Join(tfDir, "variables.tf"))
	if err != nil {
		t.Fatalf("failed to read variables.tf: %v", err)
	}

	str := string(content)
	requiredVars := []string{
		`variable "project_id"`,
		`variable "github_repo"`,
		`variable "pool_id"`,
		`variable "provider_id"`,
		`variable "sa_name"`,
	}

	for _, v := range requiredVars {
		if !strings.Contains(str, v) {
			t.Errorf("variables.tf missing declaration for %s", v)
		}
	}
}

// Scenario 4: Verify main.tf contains all required GCP IAM and WIF resources [REQ-0008.7 - REQ-0008.11]
func TestTerraformMainContent(t *testing.T) {
	tfDir := getTerraformDir(t)
	content, err := os.ReadFile(filepath.Join(tfDir, "main.tf"))
	if err != nil {
		t.Fatalf("failed to read main.tf: %v", err)
	}

	str := string(content)

	// REQ-0008.7: Workload Identity Pool
	if !strings.Contains(str, `resource "google_iam_workload_identity_pool" "pool"`) {
		t.Errorf("main.tf missing google_iam_workload_identity_pool resource")
	}

	// REQ-0008.8: OIDC Provider
	if !strings.Contains(str, `resource "google_iam_workload_identity_pool_provider" "provider"`) {
		t.Errorf("main.tf missing google_iam_workload_identity_pool_provider resource")
	}
	if !strings.Contains(str, `https://token.actions.githubusercontent.com`) {
		t.Errorf("main.tf missing GitHub Actions issuer_uri")
	}
	if !strings.Contains(str, `"google.subject"`) || !strings.Contains(str, `"assertion.sub"`) {
		t.Errorf("main.tf missing google.subject attribute mapping")
	}
	if !strings.Contains(str, `"attribute.repository"`) || !strings.Contains(str, `"assertion.repository"`) {
		t.Errorf("main.tf missing attribute.repository attribute mapping")
	}
	if !strings.Contains(str, `attribute_condition`) || !strings.Contains(str, `assertion.repository ==`) {
		t.Errorf("main.tf missing attribute_condition for repository validation")
	}

	// REQ-0008.9: Service Account
	if !strings.Contains(str, `resource "google_service_account" "runner"`) {
		t.Errorf("main.tf missing google_service_account resource")
	}

	// REQ-0008.10: aiplatform.user binding
	if !strings.Contains(str, `resource "google_project_iam_member" "aiplatform_user"`) {
		t.Errorf("main.tf missing google_project_iam_member resource")
	}
	if !strings.Contains(str, `roles/aiplatform.user`) {
		t.Errorf("main.tf missing roles/aiplatform.user role binding")
	}

	// REQ-0008.11: workloadIdentityUser binding with principalSet
	if !strings.Contains(str, `resource "google_service_account_iam_member" "wif_user"`) {
		t.Errorf("main.tf missing google_service_account_iam_member resource")
	}
	if !strings.Contains(str, `roles/iam.workloadIdentityUser`) {
		t.Errorf("main.tf missing roles/iam.workloadIdentityUser role binding")
	}
	if !strings.Contains(str, `principalSet://iam.googleapis.com/`) || !strings.Contains(str, `attribute.repository/${var.github_repo}`) {
		t.Errorf("main.tf missing principalSet attribute.repository mapping for GitHub repo")
	}
}

// Scenario 5: Verify outputs.tf exports GCP_WIF_PROVIDER and GCP_SERVICE_ACCOUNT [REQ-0008.12]
func TestTerraformOutputsContent(t *testing.T) {
	tfDir := getTerraformDir(t)
	content, err := os.ReadFile(filepath.Join(tfDir, "outputs.tf"))
	if err != nil {
		t.Fatalf("failed to read outputs.tf: %v", err)
	}

	str := string(content)

	if !strings.Contains(str, `output "gcp_wif_provider"`) {
		t.Errorf("outputs.tf missing gcp_wif_provider output")
	}
	if !strings.Contains(str, `output "gcp_service_account"`) {
		t.Errorf("outputs.tf missing gcp_service_account output")
	}
}

// Scenario 6: Verify terraform.tfvars.example contains required and optional configuration keys
func TestTerraformExampleTfvarsContent(t *testing.T) {
	tfDir := getTerraformDir(t)
	content, err := os.ReadFile(filepath.Join(tfDir, "terraform.tfvars.example"))
	if err != nil {
		t.Fatalf("failed to read terraform.tfvars.example: %v", err)
	}

	str := string(content)

	if !strings.Contains(str, `project_id =`) {
		t.Errorf("terraform.tfvars.example missing project_id example")
	}
	if !strings.Contains(str, `github_repo =`) {
		t.Errorf("terraform.tfvars.example missing github_repo example")
	}
	if !strings.Contains(str, `pool_id`) {
		t.Errorf("terraform.tfvars.example missing pool_id comment")
	}
	if !strings.Contains(str, `sa_name`) {
		t.Errorf("terraform.tfvars.example missing sa_name comment")
	}
}

// Scenario 7: Verify .gitignore ignores .terraform, state files, and credentials
func TestTerraformGitignoreContent(t *testing.T) {
	tfDir := getTerraformDir(t)
	content, err := os.ReadFile(filepath.Join(tfDir, ".gitignore"))
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}

	str := string(content)

	if !strings.Contains(str, ".terraform") {
		t.Errorf(".gitignore missing .terraform ignore pattern")
	}
	if !strings.Contains(str, "*.tfstate") {
		t.Errorf(".gitignore missing *.tfstate ignore pattern")
	}
	if !strings.Contains(str, "*.tfvars") {
		t.Errorf(".gitignore missing *.tfvars ignore pattern")
	}
	if !strings.Contains(str, "!terraform.tfvars.example") {
		t.Errorf(".gitignore missing exception for terraform.tfvars.example")
	}
}
