# CodeMender GitHub Actions CI/CD PR Review Workflow

This directory provides an opinionated, production-grade GitHub Actions CI/CD
workflow template and setup tooling for automated CodeMender (`cm`) security
scanning and patch remediation on pull requests.

For architectural background and specification details, see:

- [cm-pr-workflow Specification](../openspec/specs/workflow/cm-pr-workflow/spec.md)
- [cm-pr-workflow Design Document](../openspec/specs/workflow/cm-pr-workflow/design.md)
- [cm-pr-workflow Proposal](../openspec/proposals/cm-pr-workflow/proposal.md)
- [ADR-0001: Headless Batch Scanner](../adrs/ADR-0001.md)
- [ADR-0005: Stateless Fix Runner Protocol](../adrs/ADR-0005.md)
- [ADR-0007: Native Diff-Aware Scanning Protocol (find-diff)](../adrs/ADR-0007.md)

______________________________________________________________________

## Architecture Overview

The CodeMender review pipeline executes as a distributed two-stage workflow on
every pull request targeting the default branch (`main`).

````mermaid
flowchart TD
    subgraph Trigger["1. Pull Request Trigger"]
        PR["Pull Request to main<br>(types: opened, synchronize, reopened)"]
    end

    subgraph ScanJob["2. Scan Job (runs-on: ubuntu-latest)"]
        direction TB
        Checkout["actions/checkout@v4<br>(fetch-depth: 0)"]
        WIFScan["GCP WIF Authentication<br>google-github-actions/auth@v2"]
        DockerFind["docker run cm-runner find-diff base.sha head.sha<br>(Ephemeral staging at /tmp/cm-diff.diff)"]
        ExitTrap{"Trap Exit Code"}
        CleanExit["Exit 0: Clean / Empty Diff<br>outputs.has_findings = false"]
        FindingsExit["Exit 1: Findings Detected<br>outputs.has_findings = true"]
        MatrixGen["jq Dynamic Matrix Generator<br>Sort by severity & max limit<br>outputs.findings_matrix = [...]"]

        Checkout --> WIFScan --> DockerFind --> ExitTrap
        ExitTrap -->|"Code 0 (Clean)"| CleanExit
        ExitTrap -->|"Code 1 (Findings)"| FindingsExit --> MatrixGen
    end

    subgraph FixMatrixJob["3. Parallel Fix Matrix Jobs (strategy: matrix)"]
        direction TB
        MatrixItem["Matrix Item: finding[i]<br>(isolated VM / container)"]
        WIFFix["GCP WIF Authentication<br>google-github-actions/auth@v2"]
        DockerFix["docker run cm-runner fix finding.json<br>(stdout -> ChangeEnvelope JSON)"]
        FixVerdict{"ChangeEnvelope Status"}
        Fixed["Status: FIXED<br>Extract Hunks & Patch"]
        Unresolved["Status: UNRESOLVED<br>Log diagnostic skip"]

        MatrixItem --> WIFFix --> DockerFix --> FixVerdict
        FixVerdict -->|FIXED| Fixed
        FixVerdict -->|UNRESOLVED| Unresolved
    end

    subgraph ReviewBot["4. PR Review Suggestion Publisher"]
        direction TB
        DiffCheck{"Is hunk line range inside<br>PR diff hunks?"}
        PostInline["POST /repos/{owner}/{repo}/pulls/{id}/reviews<br>Inline ```suggestion``` block"]
        PostFallback["POST /repos/{owner}/{repo}/issues/{id}/comments<br>Top-Level PR Comment with ```diff```"]
        StepSummary["Emit to $GITHUB_STEP_SUMMARY"]

        Fixed --> DiffCheck
        DiffCheck -->|In-Diff| PostInline
        DiffCheck -->|Out-of-Diff| PostFallback
        PostInline & PostFallback --> StepSummary
    end

    PR --> ScanJob
    MatrixGen -->|outputs.findings_matrix| FixMatrixJob
````

### 1. Diff-Scoped Vulnerability Scanning (`scan`)

- **Native Diff-Aware Scanning (`find-diff`):** Executes `cm-runner find-diff`
  with PR base and head commit SHAs:
  ```bash
  docker run --rm \
    -v "$(pwd):/workspace" \
    -v "${GOOGLE_APPLICATION_CREDENTIALS}:/tmp/gcp_creds.json:ro" \
    -e GOOGLE_APPLICATION_CREDENTIALS=/tmp/gcp_creds.json \
    -e NO_COLOR=1 \
    -e TERM=dumb \
    ghcr.io/cledoux/cm-runner:latest find-diff "${{ github.event.pull_request.base.sha }}" "${{ github.event.pull_request.head.sha }}" > .codemender-out/findings.json
  ```
- **Zero Host Workspace Pollution & `git clean` Immunity:** Computes and stages
  the patch inside container scratch space (`/tmp/cm-diff.diff`), completely
  outside `/workspace`. This guarantees immunity against internal `cm find`
  repository cleanup routines (`git clean -fdx`) and prevents leftover diff
  files from polluting subsequent patch extraction during `cm-runner fix`.
- **Dynamic Config Mutation & Prompt Grounding:** Dynamically registers `.diff`
  in `scan.extensions.include` in `$HOME/.codemender/config.yaml` and injects a
  grounding context flag
  (`--context="The target is a Git unified diff for this repository."`).
- **Keyless GCP WIF Authentication:** Uses `google-github-actions/auth@v2` with
  the runner's OIDC token (`id-token: write`) to generate short-lived
  Application Default Credentials (ADC).
- **Scanner Execution & Exit Code Trapping:** Traps the exit code of
  `find-diff`:
  - **Exit Code 0 (Clean / Empty Diff):** Zero vulnerabilities found or PR diff
    was 0 bytes. Sets `has_findings=false` and terminates successfully without
    spawning downstream fix matrix jobs.
  - **Exit Code 1 (Findings Detected):** Captures findings JSON to
    `.codemender-out/findings.json` and sets `has_findings=true`.
  - **Exit Code > 1 (Error):** Fatal Git, CLI, or runtime error (e.g. shallow
    clone requiring `fetch-depth: 0`); fails the step.
- **Dynamic Matrix Partitioning:** Parses findings with `jq`, sorts remaining
  findings by severity (`CRITICAL` > `HIGH` > `MEDIUM` > `LOW`), bounds to the
  top $M$ items (default: 10), and emits the JSON payload array to
  `outputs.findings_matrix`.

### 2. Parallel Stateless Patch Remediation (`fix`)

- **Dynamic Matrix Execution:** Dispatches concurrent runner jobs using
  `strategy: matrix: { finding: ${{ fromJson(needs.scan.outputs.findings_matrix) }} }`
  with `fail-fast: false`.
- **Stateless Agent Fix:** Each job feeds a single finding payload into
  `cm-runner fix` and captures the structured `ChangeEnvelope` JSON from
  `stdout`.
- **Remediation Status:**
  - `status == "FIXED"`: Formats diff hunks into 1-click apply review
    suggestions.
  - `status == "UNRESOLVED"`: Logs diagnostic summary and completes cleanly
    without failing the check run.

### 3. PR Review Suggestions & Fallback Handling

- **1-Click Apply Review Comments:** For each modified hunk located within the
  pull request diff, posts an inline review comment using
  `POST /repos/{owner}/{repo}/pulls/{id}/reviews` with a markdown suggestion:
  ````markdown
  ### 🛡️ CodeMender Auto-Fix Suggestion: SQL Injection in User Lookup
  > **Vulnerability:** CWE-89 | **Severity:** HIGH | **Status:** FIXED

  Replaced string concatenation with parameterized query.

  ```suggestion
      query := "SELECT * FROM users WHERE id = $1"
      row := db.QueryRowContext(ctx, query, id)
  ```
  ````
- **Diff-Boundary Fallback (HTTP 422 Mitigation):** If the GitHub API rejects an
  inline comment with HTTP `422 Unprocessable Entity` (line outside PR diff),
  the step catches the error and posts a top-level issue comment via
  `POST /repos/{owner}/{repo}/issues/{id}/comments` and writes the patch to
  `$GITHUB_STEP_SUMMARY`.

______________________________________________________________________

## Authentication: Google Cloud Workload Identity Federation (WIF)

The workflow connects keylessly to Google Cloud Vertex AI using Workload
Identity Federation, eliminating persistent service account keys.

```mermaid
sequenceDiagram
    autonumber
    participant GHA as GitHub Actions Runner VM
    participant OIDC as GitHub OIDC Token Provider
    participant STS as GCP Security Token Service (STS)
    participant IAM as GCP IAM Service Account
    participant Container as Docker Container (cm-runner)
    participant Vertex as Google Cloud Vertex AI (Gemini)

    GHA->>OIDC: 1. Request OIDC Token (permissions: id-token: write)
    OIDC-->>GHA: Return GitHub OIDC JWT (contains sub, actor, repository)
    GHA->>STS: 2. Exchange JWT via google-github-actions/auth@v2
    STS->>IAM: 3. Verify PrincipalSet repository claim & impersonate Service Account
    IAM-->>STS: Issue short-lived OAuth token / ADC config
    STS-->>GHA: Write temp credentials file ($GOOGLE_APPLICATION_CREDENTIALS)
    GHA->>Container: 4. docker run -v "$GOOGLE_APPLICATION_CREDENTIALS:/tmp/gcp_creds.json:ro" -e GOOGLE_APPLICATION_CREDENTIALS=/tmp/gcp_creds.json
    Container->>Vertex: 5. Execute find/fix with ADC credentials (roles/aiplatform.user)
    Vertex-->>Container: Return model completions & vulnerability analysis
```

______________________________________________________________________

## Prerequisites & Required Configuration

### GitHub Repository Secrets

Configure the following repository secrets under **Settings > Secrets and
variables > Actions**:

| Secret Name           | Description                                          | Example Format                                                                                    |
| :-------------------- | :--------------------------------------------------- | :------------------------------------------------------------------------------------------------ |
| `GCP_WIF_PROVIDER`    | Full resource name of the Workload Identity Provider | `projects/123456789/locations/global/workloadIdentityPools/github-pool/providers/github-provider` |
| `GCP_SERVICE_ACCOUNT` | Email address of the dedicated GCP Service Account   | `codemender-runner@my-gcp-project.iam.gserviceaccount.com`                                        |

### Required Workflow Permissions

The workflow requires the following permissions in the job definition:

```yaml
permissions:
  contents: read # Read repository code and commit history
  id-token: write # Request GitHub OIDC JWT for GCP WIF authentication
  pull-requests: write # Publish review comments and PR suggestions
```

### Required Google Cloud IAM Roles

The GCP Service Account and Workload Identity Provider require:

1. **`roles/aiplatform.user`:** Granted to the Service Account on the GCP
   project hosting Vertex AI models.
1. **`roles/iam.workloadIdentityUser`:** Bound on the Service Account to the
   Workload Identity Pool PrincipalSet:
   `principalSet://iam.googleapis.com/projects/<PROJECT_NUM>/locations/global/workloadIdentityPools/<POOL>/attribute.repository/<OWNER>/<REPO>`

______________________________________________________________________

## Quickstart Setup Guide

### Step 1: Install Workflow Template in Target Repository

Use the provided `install.sh` script to copy the workflow template to your
target repository:

```bash
# From the cm-connect repository:
./github-actions/scripts/install.sh /path/to/target-repository
```

This copies `workflows/codemender.yml` into `.github/workflows/codemender.yml`
in the target repository.

### Step 2: Provision GCP WIF & IAM via Terraform (Recommended) or Helper Script

#### Option A: Declarative Terraform Module (Recommended)

Use the provided Terraform module in `github-actions/terraform/`:

```bash
cd github-actions/terraform
terraform init
terraform apply -var="project_id=my-gcp-project" -var="github_repo=my-org/my-target-repo"
```

#### Option B: Automated CLI Helper Script

Alternatively, run the automated `setup-wif.sh` helper script with `gcloud`
credentials:

```bash
# Run WIF automated setup script:
./github-actions/scripts/setup-wif.sh \
  --project="my-gcp-project" \
  --repo="my-org/my-target-repo" \
  --pool-name="codemender-pool" \
  --provider-name="github-provider" \
  --service-account="codemender-runner"
```

Both options output the exact values for `GCP_WIF_PROVIDER` and
`GCP_SERVICE_ACCOUNT`.

### Step 3: Add GitHub Repository Secrets

Add the values returned by Terraform or `setup-wif.sh` to your target GitHub
repository:

1. Navigate to **Settings > Secrets and variables > Actions**.
1. Click **New repository secret**.
1. Create `GCP_WIF_PROVIDER` with the provider resource name.
1. Create `GCP_SERVICE_ACCOUNT` with the service account email.

### Step 4: Verify on Pull Request

1. Create a feature branch and commit changes.
1. Open a pull request targeting `main`.
1. Verify that the `scan` job triggers, executes `find-diff`, and launches
   parallel `fix` matrix jobs to post inline suggestions when findings occur.

______________________________________________________________________

## Package Directory Structure

```
github-actions/
├── README.md               # Quickstart onboarding and configuration guide
├── scripts/
│   ├── filter_findings.jq  # jq filter for diff-scoped matrix partitioning
│   ├── install.sh          # One-command installer copying workflow to target repo
│   ├── publish_comments.py # PR inline suggestion and fallback comment publisher
│   └── setup-wif.sh        # GCP IAM & WIF automated CLI setup script
├── terraform/              # Declarative GCP WIF & IAM Terraform module
│   ├── main.tf             # WIF pool, provider, SA, and IAM member resources
│   ├── outputs.tf          # Output values for GitHub secrets
│   ├── variables.tf        # Configurable variables (project_id, github_repo, etc.)
│   ├── versions.tf         # Terraform and Google provider version constraints
│   └── README.md           # Terraform usage guide
└── workflows/
    └── codemender.yml      # Standalone GitHub Actions workflow template
```

> **Note on Repository Isolation:** All workflow files are maintained under
> `github-actions/` rather than `.github/` to prevent unintentional automated CI
> execution on `cm-connect`.
