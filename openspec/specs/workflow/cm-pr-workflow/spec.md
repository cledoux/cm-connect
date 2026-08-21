---
archetype: capability
status: proposed
category: workflow
name: cm-pr-workflow
governing_proposal: openspec/proposals/cm-pr-workflow/proposal.md
governing_adrs:
  - adrs/ADR-0001.md
  - adrs/ADR-0002.md
  - adrs/ADR-0003.md
  - adrs/ADR-0005.md
---

# CodeMender GitHub Actions CI/CD PR Review Workflow Specification (`cm-pr-workflow`)

## Overview

The `cm-pr-workflow` capability defines a distributed, opinionated GitHub
Actions CI/CD pull request review workflow for CodeMender (`cm`), implementing
the contract defined in
[cm-pr-workflow proposal](../../../proposals/cm-pr-workflow/proposal.md) and
operationalizing [ADR-0001](../../../../adrs/ADR-0001.md) (Batch Scanner) and
[ADR-0005](../../../../adrs/ADR-0005.md) (Stateless Fix Runner Protocol).

In automated development workflows, pull requests require fast, accurate
security feedback without developer fatigue caused by un-scoped codebase scans
or pre-existing legacy issues. The `cm-pr-workflow` capability establishes a
production-grade pipeline that:

1. **Scopes Analysis to Pull Request Diffs:** Analyzes only the files and line
   ranges modified by the pull request, eliminating scan noise and remediation
   overhead on untouched legacy files.
1. **Authenticates Keylessly via Google Cloud WIF:** Leverages GitHub OIDC and
   Workload Identity Federation (`google-github-actions/auth@v2`) to generate
   ephemeral Application Default Credentials (ADC) without long-lived service
   account keys.
1. **Traps Scanner Exit Codes:** Treats `cm-runner find` exit code `1` (findings
   detected) as an expected branch rather than a job abort, cleanly
   transitioning to parallel remediation.
1. **Dispatches Dynamic Parallel Fix Matrices:** Generates dynamic GitHub
   Actions matrix payloads (`outputs.findings_matrix`) using `jq` to execute
   `cm-runner fix` across concurrent runner jobs with configurable
   `max-parallel` bounds and top-$N$ severity prioritization.
1. **Synthesizes 1-Click Apply Review Comments:** Converts `ChangeEnvelope` hunk
   records into inline GitHub Pull Request Review comments
   (`POST /pulls/{id}/reviews`) with markdown ```` ```suggestion ```` blocks.
1. **Gracefully Handles Diff Boundaries:** Detects whether findings intersect
   the pull request diff, routing out-of-diff findings to PR issue comments or
   `$GITHUB_STEP_SUMMARY` to prevent GitHub API `422 Unprocessable Entity`
   rejections.
1. **Isolates Example Deliverables:** Maintains the workflow template strictly
   under `examples/workflows/codemender.yml` and `examples/workflows/README.md`,
   ensuring zero unintentional workflow execution on `cm-connect`.

## Requirements

### REQ-0001: Trigger and Diff-Scoped Workspace Discovery

The workflow MUST trigger on pull request events (`pull_request` types:
`[opened, synchronize, reopened]`) targeting the repository's default branch
(`main`).

The workflow MUST check out the repository with full git history
(`fetch-depth: 0`) and identify the set of modified files and line ranges
introduced by the pull request using `git diff --name-only origin/main...HEAD`.

#### Scenario: Discover modified files in pull request

- **GIVEN** a pull request targeting `main` containing modifications to
  `pkg/auth/store.go` and `cmd/server/main.go`
- **WHEN** the `scan` job executes
- **THEN** the workflow MUST extract the modified file list
  `["pkg/auth/store.go", "cmd/server/main.go"]` and record the diff boundaries
  for downstream filtering.

#### Scenario: Short-circuit on non-scannable pull request diff

- **GIVEN** a pull request modifying only non-code documentation files (e.g.
  `README.md`)
- **WHEN** the diff extraction step executes
- **THEN** the workflow MUST complete the scan step with zero findings and
  record `has_findings=false`.

______________________________________________________________________

### REQ-0002: Keyless Google Cloud Workload Identity Federation (WIF) Authentication

The workflow jobs (`scan` and `fix`) MUST use keyless authentication to Google
Cloud Vertex AI via Workload Identity Federation
(`google-github-actions/auth@v2`).

The workflow MUST require the `id-token: write` permission to request a GitHub
OIDC token and exchange it with GCP Security Token Service (STS) for a temporary
Application Default Credentials (ADC) file.

The workflow MUST pass the temporary ADC credential file into Docker container
invocations via a read-only volume mount:
`-v "${GOOGLE_APPLICATION_CREDENTIALS}:/tmp/gcp_creds.json:ro"` and set
`-e GOOGLE_APPLICATION_CREDENTIALS=/tmp/gcp_creds.json`.

#### Scenario: Authenticate runner via WIF

- **GIVEN** valid repository secrets `GCP_WIF_PROVIDER` and
  `GCP_SERVICE_ACCOUNT`
- **WHEN** the `auth` step executes in the `scan` or `fix` job
- **THEN** `google-github-actions/auth` MUST generate a short-lived credentials
  file at `$GOOGLE_APPLICATION_CREDENTIALS` and provide it to the container.

#### Scenario: Reject execution when WIF secrets are missing

- **GIVEN** missing or empty `GCP_WIF_PROVIDER` secret
- **WHEN** the `auth` step executes
- **THEN** the step MUST fail with a descriptive diagnostic message instructing
  the user to configure repository secrets.

______________________________________________________________________

### REQ-0003: Headless Batch Scanner Execution and Exit Code Trapping

The `scan` job MUST execute `cm-runner find` against the target workspace and
trap the exit code:

1. **Exit Code 0 (Clean):** Scanner found zero vulnerabilities. The step MUST
   emit `has_findings=false` and terminate successfully.
1. **Exit Code 1 (Findings Detected):** Scanner detected one or more
   vulnerabilities. The step MUST NOT fail the job, but MUST capture the JSON
   output from `stdout` to `.codemender-out/findings.json` and set
   `has_findings=true`.
1. **Exit Code > 1 (Error):** Scanner encountered a fatal CLI or runtime error.
   The step MUST fail immediately and propagate the non-zero exit code.

#### Scenario: Clean scan with zero vulnerabilities

- **GIVEN** a workspace containing no security vulnerabilities
- **WHEN** `cm-runner find` terminates with exit code `0`
- **THEN** the scan step MUST set output `has_findings=false` and complete
  without scheduling downstream fix jobs.

#### Scenario: Trap exit code 1 on detected vulnerabilities

- **GIVEN** a workspace containing vulnerabilities in modified PR files
- **WHEN** `cm-runner find` terminates with exit code `1`
- **THEN** the workflow MUST capture the findings JSON payload, set
  `has_findings=true`, and proceed to dynamic matrix generation.

#### Scenario: Propagate fatal scanner error

- **GIVEN** an invalid scanner invocation or container crash resulting in exit
  code `2`
- **WHEN** the scan step executes
- **THEN** the workflow MUST fail the step and output the scanner error message.

______________________________________________________________________

### REQ-0004: Dynamic Matrix Partitioning and Concurrency Throttling

When `has_findings=true`, the `scan` job MUST parse
`.codemender-out/findings.json` using `jq` and construct a dynamic matrix JSON
array:

1. **Diff Filtering:** The generator MUST discard findings whose `FilePath` and
   `StartLine` do not intersect the pull request diff.
1. **Finding Prioritization:** If remaining findings exceed a configurable
   maximum threshold $M$ (default: `10`), findings MUST be sorted by `Severity`
   (`CRITICAL` > `HIGH` > `MEDIUM` > `LOW`) and truncated to the top $M$ items.
1. **Output Matrix:** The generator MUST emit the resulting JSON array to
   `outputs.findings_matrix` where each element contains:
   - `finding_id`: The canonical finding identifier.
   - `file_path`: Path to the affected source file.
   - `start_line`: Vulnerability start line number.
   - `title`: Summary headline.
   - `payload`: The complete single-finding JSON object consumable by
     `cm-runner fix`.

#### Scenario: Generate dynamic matrix from diff findings

- **GIVEN** a scan output with 2 findings touching modified PR files
- **WHEN** the matrix generation step executes
- **THEN** `outputs.findings_matrix` MUST contain a 2-element JSON array
  formatted for GitHub Actions `strategy: matrix`.

#### Scenario: Discard out-of-diff findings during matrix generation

- **GIVEN** a scan output with 1 finding in modified `pkg/auth/store.go` and 1
  finding in untouched `legacy/db.go`
- **WHEN** the matrix generation step executes
- **THEN** `outputs.findings_matrix` MUST include only the finding for
  `pkg/auth/store.go`.

______________________________________________________________________

### REQ-0005: Stateless Parallel Fix Runner Orchestration

The `fix` job MUST execute with
`strategy: matrix: { finding: ${{ fromJson(needs.scan.outputs.findings_matrix) }} }`
and `fail-fast: false`:

1. Each matrix job MUST execute on an independent runner VM or container.
1. The job MUST pass the single finding payload to `cm-runner fix -` via
   standard input or a temporary file.
1. The job MUST capture the resulting `ChangeEnvelope` JSON from `stdout`.
1. If `cm-runner fix` returns exit code `0` (`status == "FIXED"`), the job MUST
   proceed to post review comments.
1. If `cm-runner fix` returns exit code `1` (`status == "UNRESOLVED"`), the job
   MUST log the unresolved finding and complete without failing the matrix
   pipeline.

#### Scenario: Remediate finding in parallel matrix job

- **GIVEN** an incoming matrix item with finding payload
- **WHEN** `docker run ... cm-runner fix /tmp/finding.json` executes
- **THEN** `cm-runner` MUST emit a `ChangeEnvelope` JSON record to `stdout` with
  exit code `0`.

#### Scenario: Handle unresolved fix gracefully

- **GIVEN** a complex finding that the fix agent cannot resolve
- **WHEN** `cm-runner fix` returns exit code `1`
- **THEN** the matrix job MUST record `fix_status=UNRESOLVED` and complete
  without failing the PR check run.

______________________________________________________________________

### REQ-0006: Pull Request Review Suggestion Translation and Formatting

For each fixed finding with valid `hunks`, the review publisher MUST format each
hunk into an inline review suggestion comment:

1. **Comment Body Format:**
   ````markdown
   ### 🛡️ CodeMender Auto-Fix: <Title>
   > **Vulnerability:** <VulnType> | **Severity:** <Severity>

   <Summary>

   ```suggestion
   <Hunk.Replacement>
   ````
   ```
   ```
1. **API Coordinates:**
   - `path`: `hunk.file_path`
   - `side`: `"RIGHT"`
   - `start_side`: `"RIGHT"` (if multi-line)
   - `start_line`: `hunk.start_line` (omitted if single-line
     `start_line == end_line`)
   - `line`: `hunk.end_line`

#### Scenario: Translate single-line hunk to review suggestion

- **GIVEN** a `ChangeEnvelope` with a single-line hunk replacing line 42 of
  `pkg/auth/store.go`
- **WHEN** the comment publisher processes the hunk
- **THEN** it MUST invoke `createReviewComment` targeting
  `path="pkg/auth/store.go"`, `line=42`, and a ```` ```suggestion ```` block
  containing the replacement.

#### Scenario: Translate multi-line hunk to review suggestion

- **GIVEN** a `ChangeEnvelope` with a multi-line hunk replacing lines 42–45 of
  `pkg/auth/store.go`
- **WHEN** the comment publisher processes the hunk
- **THEN** it MUST invoke `createReviewComment` targeting
  `path="pkg/auth/store.go"`, `start_line=42`, `line=45`, `start_side="RIGHT"`,
  and `side="RIGHT"`.

______________________________________________________________________

### REQ-0007: Diff-Boundary Fallback Handling

If the GitHub API rejects an inline review comment with HTTP
`422 Unprocessable Entity` (indicating the targeted line falls outside the pull
request's diff hunk):

1. The publisher MUST catch the error without failing the job step.
1. The publisher MUST fall back to creating a top-level pull request issue
   comment (`POST /repos/{owner}/{repo}/issues/{id}/comments`) or writing to
   `$GITHUB_STEP_SUMMARY`.
1. The fallback comment MUST contain the finding title, summary, and the unified
   diff patch in a ```` ```diff ```` code block.

#### Scenario: Fallback to top-level comment on out-of-diff rejection

- **GIVEN** a valid fix patch on a file line that GitHub rejects with HTTP 422
- **WHEN** `createReviewComment` throws an error
- **THEN** the publisher MUST post a top-level PR comment with the diff patch
  and log the fallback.

______________________________________________________________________

### REQ-0008: Standalone Template Isolation and Repository Protection

The example workflow and setup documentation MUST reside strictly under:

- `examples/workflows/codemender.yml`
- `examples/workflows/README.md`

The repository MUST NOT create or enable active workflows in
`.github/workflows/` on `cm-connect` to prevent automated CI execution on this
repository during development.

#### Scenario: Verify template location

- **GIVEN** the `cm-connect` codebase
- **WHEN** reviewing root directory paths
- **THEN** the workflow file MUST exist at `examples/workflows/codemender.yml`
  and `.github/workflows/codemender.yml` MUST NOT exist.
