//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// workflowDocument represents the schema of a GitHub Actions workflow YAML.
type workflowDocument struct {
	Name        string                 `yaml:"name"`
	On          workflowTrigger        `yaml:"on"`
	Permissions map[string]string      `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

// workflowTrigger represents the trigger events configuration.
type workflowTrigger struct {
	PullRequest *pullRequestTrigger `yaml:"pull_request"`
}

// pullRequestTrigger represents pull_request trigger configuration.
type pullRequestTrigger struct {
	Types    []string `yaml:"types"`
	Branches []string `yaml:"branches"`
}

// workflowJob represents a single job definition in the workflow.
type workflowJob struct {
	Name        string            `yaml:"name"`
	RunsOn      string            `yaml:"runs-on"`
	Needs       yaml.Node         `yaml:"needs"`
	If          string            `yaml:"if"`
	Strategy    *workflowStrategy `yaml:"strategy"`
	Permissions map[string]string `yaml:"permissions"`
	Outputs     map[string]string `yaml:"outputs"`
	Steps       []workflowStep    `yaml:"steps"`
}

// workflowStrategy represents the job matrix strategy configuration.
type workflowStrategy struct {
	FailFast *bool                  `yaml:"fail-fast"`
	Matrix   map[string]interface{} `yaml:"matrix"`
}

// workflowStep represents an individual step within a job.
type workflowStep struct {
	ID   string            `yaml:"id"`
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	With map[string]string `yaml:"with"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
	If   string            `yaml:"if"`
}

// getWorkflowTemplatePath returns the path to github-actions/workflows/codemender.yml.
func getWorkflowTemplatePath(t *testing.T) string {
	t.Helper()
	repoRoot := getRepoRoot(t)
	return filepath.Join(repoRoot, "github-actions", "workflows", "codemender.yml")
}

// loadWorkflowTemplate parses github-actions/workflows/codemender.yml into workflowDocument.
func loadWorkflowTemplate(t *testing.T) (workflowDocument, string) {
	t.Helper()
	workflowPath := getWorkflowTemplatePath(t)

	content, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("failed to read workflow template at %s: %v", workflowPath, err)
	}

	var doc workflowDocument
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatalf("failed to parse workflow template YAML: %v", err)
	}

	return doc, string(content)
}

// TestWorkflow_TemplateFileExists verifies workflow file location and repo isolation (REQ-0008).
func TestWorkflow_TemplateFileExists(t *testing.T) {
	repoRoot := getRepoRoot(t)
	workflowPath := getWorkflowTemplatePath(t)

	if info, err := os.Stat(workflowPath); err != nil || info.IsDir() {
		t.Fatalf("expected workflow template at %s, got err: %v", workflowPath, err)
	}

	// Verify isolation: .github/workflows/codemender.yml MUST NOT exist in cm-connect
	activeWorkflowPath := filepath.Join(repoRoot, ".github", "workflows", "codemender.yml")
	if _, err := os.Stat(activeWorkflowPath); err == nil {
		t.Errorf("isolated workflow template must NOT exist at %s in cm-connect", activeWorkflowPath)
	}
}

// TestWorkflow_TemplateValidYAML verifies the template is valid YAML and has basic metadata.
func TestWorkflow_TemplateValidYAML(t *testing.T) {
	doc, raw := loadWorkflowTemplate(t)

	if strings.TrimSpace(raw) == "" {
		t.Fatalf("workflow template file is empty")
	}
	if doc.Name == "" {
		t.Errorf("workflow name is empty")
	}
	if len(doc.Jobs) != 2 {
		t.Errorf("expected 2 jobs (scan, fix), got %d: %v", len(doc.Jobs), doc.Jobs)
	}
}

// TestWorkflow_TriggerAndPermissions verifies PR triggers, branch scoping, and OIDC permissions (REQ-0001, REQ-0002).
func TestWorkflow_TriggerAndPermissions(t *testing.T) {
	doc, _ := loadWorkflowTemplate(t)

	if doc.On.PullRequest == nil {
		t.Fatalf("expected pull_request trigger definition")
	}

	// Verify PR types: opened, synchronize, reopened
	expectedTypes := map[string]bool{"opened": false, "synchronize": false, "reopened": false}
	for _, typ := range doc.On.PullRequest.Types {
		expectedTypes[typ] = true
	}
	for typ, found := range expectedTypes {
		if !found {
			t.Errorf("pull_request.types missing %q", typ)
		}
	}

	// Verify branches: main
	hasMain := false
	for _, branch := range doc.On.PullRequest.Branches {
		if branch == "main" {
			hasMain = true
		}
	}
	if !hasMain {
		t.Errorf("pull_request.branches missing 'main'")
	}

	// Verify top-level or job-level permissions: contents: read, id-token: write, pull-requests: write
	perms := doc.Permissions
	if perms["contents"] != "read" {
		t.Errorf("permissions.contents = %q, want 'read'", perms["contents"])
	}
	if perms["id-token"] != "write" {
		t.Errorf("permissions.id-token = %q, want 'write'", perms["id-token"])
	}
	if perms["pull-requests"] != "write" {
		t.Errorf("permissions.pull-requests = %q, want 'write'", perms["pull-requests"])
	}
}

// TestWorkflow_ScanJobDefinition verifies the scan job steps, native find-diff execution, exit code trapping, and matrix emission (REQ-0001..REQ-0004, REQ-WF.1, REQ-WF.2).
func TestWorkflow_ScanJobDefinition(t *testing.T) {
	doc, raw := loadWorkflowTemplate(t)

	scanJob, exists := doc.Jobs["scan"]
	if !exists {
		t.Fatalf("workflow missing 'scan' job")
	}

	if scanJob.RunsOn != "ubuntu-latest" {
		t.Errorf("scan.runs-on = %q, want 'ubuntu-latest'", scanJob.RunsOn)
	}

	// Outputs verification
	if _, exists := scanJob.Outputs["has_findings"]; !exists {
		t.Errorf("scan job missing output 'has_findings'")
	}
	if _, exists := scanJob.Outputs["findings_matrix"]; !exists {
		t.Errorf("scan job missing output 'findings_matrix'")
	}

	// Step checks
	var hasCheckout, hasWIFAuth, hasScanRun, hasMatrixGen bool
	var hasDiffExtract bool
	for _, step := range scanJob.Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout@v4") {
			hasCheckout = true
			if step.With["fetch-depth"] != "0" {
				t.Errorf("scan checkout step fetch-depth = %q, want '0'", step.With["fetch-depth"])
			}
		}
		if step.Name == "Extract Pull Request diff" {
			hasDiffExtract = true
		}
		if strings.HasPrefix(step.Uses, "google-github-actions/auth@v2") {
			hasWIFAuth = true
			if !strings.Contains(step.With["workload_identity_provider"], "GCP_WIF_PROVIDER") {
				t.Errorf("scan auth missing GCP_WIF_PROVIDER secret: %v", step.With)
			}
			if !strings.Contains(step.With["service_account"], "GCP_SERVICE_ACCOUNT") {
				t.Errorf("scan auth missing GCP_SERVICE_ACCOUNT secret: %v", step.With)
			}
		}
		if strings.Contains(step.Run, "cm-runner") && strings.Contains(step.Run, "find-diff") {
			hasScanRun = true
			if !strings.Contains(step.Run, "github.event.pull_request.base.sha") ||
				!strings.Contains(step.Run, "github.event.pull_request.head.sha") {
				t.Errorf("scan find-diff missing base.sha or head.sha reference: %s", step.Run)
			}
			if !strings.Contains(step.Run, "-v \"$(pwd):/workspace\"") && !strings.Contains(step.Run, "-v \"$PWD:/workspace\"") {
				t.Errorf("scan docker run missing workspace volume mount: %s", step.Run)
			}
			if !strings.Contains(step.Run, "GOOGLE_APPLICATION_CREDENTIALS") {
				t.Errorf("scan docker run missing GOOGLE_APPLICATION_CREDENTIALS: %s", step.Run)
			}
			if !strings.Contains(step.Run, "NO_COLOR=1") || !strings.Contains(step.Run, "TERM=dumb") {
				t.Errorf("scan docker run missing NO_COLOR=1 or TERM=dumb: %s", step.Run)
			}
		}
		if strings.Contains(step.Run, "filter_findings.jq") {
			hasMatrixGen = true
		}
	}

	if !hasCheckout {
		t.Errorf("scan job missing actions/checkout@v4 step with fetch-depth: 0")
	}
	if hasDiffExtract {
		t.Errorf("scan job MUST NOT contain host-side commit.diff extraction step (REQ-WF.2)")
	}
	if !hasWIFAuth {
		t.Errorf("scan job missing google-github-actions/auth@v2 step")
	}
	if !hasScanRun {
		t.Errorf("scan job missing docker run cm-runner find-diff step (REQ-WF.1)")
	}
	if !hasMatrixGen {
		t.Errorf("scan job missing filter_findings.jq matrix generation step")
	}

	// Exit code trapping validation in raw YAML or run script
	if !strings.Contains(raw, "has_findings") {
		t.Errorf("scan job missing has_findings exit code trapping")
	}
}

// TestWorkflow_FixJobDefinition verifies the fix matrix job, FUSE mounting, and comment publisher (REQ-0005..REQ-0007).
func TestWorkflow_FixJobDefinition(t *testing.T) {
	doc, raw := loadWorkflowTemplate(t)

	fixJob, exists := doc.Jobs["fix"]
	if !exists {
		t.Fatalf("workflow missing 'fix' job")
	}

	if fixJob.RunsOn != "ubuntu-latest" {
		t.Errorf("fix.runs-on = %q, want 'ubuntu-latest'", fixJob.RunsOn)
	}

	// Verify needs: scan
	var needsList []string
	if fixJob.Needs.Kind == yaml.ScalarNode {
		needsList = append(needsList, fixJob.Needs.Value)
	} else if fixJob.Needs.Kind == yaml.SequenceNode {
		for _, node := range fixJob.Needs.Content {
			needsList = append(needsList, node.Value)
		}
	}
	hasScanNeed := false
	for _, n := range needsList {
		if n == "scan" {
			hasScanNeed = true
		}
	}
	if !hasScanNeed {
		t.Errorf("fix job needs = %v, want to include 'scan'", needsList)
	}

	// Verify if condition
	if !strings.Contains(fixJob.If, "has_findings") || !strings.Contains(fixJob.If, "true") {
		t.Errorf("fix job if condition %q does not check has_findings == 'true'", fixJob.If)
	}

	// Strategy verification
	if fixJob.Strategy == nil {
		t.Fatalf("fix job missing strategy")
	}
	if fixJob.Strategy.FailFast == nil || *fixJob.Strategy.FailFast != false {
		t.Errorf("fix job strategy fail-fast = %v, want false", fixJob.Strategy.FailFast)
	}
	if !strings.Contains(raw, "fromJson(needs.scan.outputs.findings_matrix)") {
		t.Errorf("fix strategy matrix missing fromJson(needs.scan.outputs.findings_matrix)")
	}

	// Step checks
	var hasCheckout, hasWIFAuth, hasFixRun, hasPublishRun bool
	for _, step := range fixJob.Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout@v4") {
			hasCheckout = true
		}
		if strings.HasPrefix(step.Uses, "google-github-actions/auth@v2") {
			hasWIFAuth = true
		}
		if strings.Contains(step.Run, "cm-runner") && strings.Contains(step.Run, "fix") {
			hasFixRun = true
			if !strings.Contains(step.Run, "-v \"$(pwd):/workspace\"") && !strings.Contains(step.Run, "-v \"$PWD:/workspace\"") {
				t.Errorf("fix docker run missing workspace volume mount: %s", step.Run)
			}
			if !strings.Contains(step.Run, "--user \"$(id -u):$(id -g)\"") {
				t.Errorf("fix docker run missing --user user mapping: %s", step.Run)
			}
			if !strings.Contains(step.Run, "change_envelope.json") {
				t.Errorf("fix docker run missing change_envelope.json output redirect: %s", step.Run)
			}
		}
		if strings.Contains(step.Run, "publish_comments.py") {
			hasPublishRun = true
			if !strings.Contains(raw, "secrets.GITHUB_TOKEN") && !strings.Contains(step.Env["GITHUB_TOKEN"], "GITHUB_TOKEN") {
				t.Errorf("publish step missing GITHUB_TOKEN: %v", step.Env)
			}
			if !strings.Contains(raw, "github.repository") && !strings.Contains(step.Env["GITHUB_REPOSITORY"], "github.repository") {
				t.Errorf("publish step missing GITHUB_REPOSITORY: %v", step.Env)
			}
			if !strings.Contains(raw, "github.event.pull_request.number") && !strings.Contains(step.Env["PR_NUMBER"], "pull_request.number") {
				t.Errorf("publish step missing PR_NUMBER: %v", step.Env)
			}
			if !strings.Contains(raw, "github.event.pull_request.head.sha") && !strings.Contains(step.Env["COMMIT_SHA"], "pull_request.head.sha") {
				t.Errorf("publish step missing COMMIT_SHA: %v", step.Env)
			}
		}
	}

	if !hasCheckout {
		t.Errorf("fix job missing actions/checkout@v4 step")
	}
	if !hasWIFAuth {
		t.Errorf("fix job missing google-github-actions/auth@v2 step")
	}
	if !hasFixRun {
		t.Errorf("fix job missing docker run cm-runner fix step")
	}
	if !hasPublishRun {
		t.Errorf("fix job missing publish_comments.py execution step")
	}
}
