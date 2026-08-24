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
  - adrs/ADR-0006.md
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
production-grade pipeline tailored to the GitHub Actions runner environment
that:

1. **Captures Pull Request Diffs Directly (`commit.diff`):** Emits the complete
   pull request diff using
   `git diff ${{ github.event.pull_request.base.sha }} ${{ github.event.pull_request.head.sha }} > commit.diff`,
   scoping vulnerability discovery and patch remediation strictly to modified
   lines without complex interval-parsing scripts.
1. **Authenticates Keylessly via Google Cloud WIF:** Leverages GitHub Actions
   native OIDC token injection (`id-token: write`) and Workload Identity
   Federation (`google-github-actions/auth@v2`) to generate ephemeral
   Application Default Credentials (ADC) without long-lived service account
   keys.
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
1. **Organizes Dedicated GitHub Actions Assets:** Packages the workflow
   template, setup guide, WIF configuration script, and automated installer
   script strictly under `github-actions/`
   (`github-actions/workflows/codemender.yml`,
   `github-actions/scripts/install.sh`, `github-actions/scripts/setup-wif.sh`,
   and `github-actions/README.md`), ensuring zero unintentional workflow
   execution on `cm-connect`.

## Requirements

### REQ-0001: Trigger and Diff-Scoped Workspace Discovery via `commit.diff`

The workflow MUST trigger on pull request events (`pull_request` types:
`[opened, synchronize, reopened]`) targeting the repository's default branch
(`main`).

The workflow MUST check out the repository with full git history
(`fetch-depth: 0`) and extract the complete pull request diff directly to
`commit.diff` in the workspace root using:
`git diff ${{ github.event.pull_request.base.sha }} ${{ github.event.pull_request.head.sha }} > commit.diff`

The scanner and filtering steps MUST consume `commit.diff` to identify modified
files and restrict finding analysis strictly to lines changed by the pull
request.

#### Scenario: Extract pull request diff to commit.diff

- **GIVEN** a pull request targeting `main` with modified files
  `pkg/auth/store.go` and `cmd/server/main.go`
- **WHEN** the `scan` job executes the diff extraction step
- **THEN** the workflow MUST execute
  `git diff ${{ github.event.pull_request.base.sha }} ${{ github.event.pull_request.head.sha }} > commit.diff`
  and verify `commit.diff` is populated.

#### Scenario: Short-circuit on empty diff

- **GIVEN** a pull request with an empty diff or zero scannable source changes
- **WHEN** `commit.diff` is evaluated
- **THEN** the workflow MUST complete the scan step with zero findings, set
  `has_findings=false`, and skip downstream remediation jobs.

______________________________________________________________________

### REQ-0002: Keyless Google Cloud Workload Identity Federation (WIF) Authentication

The workflow jobs (`scan` and `fix`) MUST authenticate keylessly to Google Cloud
Vertex AI via Workload Identity Federation (`google-github-actions/auth@v2`).

The workflow MUST require the `id-token: write` permission to request a GitHub
OIDC JWT and exchange it with GCP Security Token Service (STS) for a temporary
Application Default Credentials (ADC) file.

The workflow MUST pass the temporary ADC credential file into Docker container
invocations via a read-only volume mount:
`-v "${GOOGLE_APPLICATION_CREDENTIALS}:/tmp/gcp_creds.json:ro"` and set
`-e GOOGLE_APPLICATION_CREDENTIALS=/tmp/gcp_creds.json`.

#### Scenario: Authenticate runner via WIF

- **GIVEN** configured repository secrets `GCP_WIF_PROVIDER` and
  `GCP_SERVICE_ACCOUNT`
- **WHEN** the `auth` step executes in the `scan` or `fix` job
- **THEN** `google-github-actions/auth` MUST generate a short-lived credentials
  file at `$GOOGLE_APPLICATION_CREDENTIALS` and mount it to the container.

#### Scenario: Fail fast when WIF secrets are unconfigured

- **GIVEN** missing or empty `GCP_WIF_PROVIDER` secret
- **WHEN** the `auth` step executes
- **THEN** the step MUST fail with a descriptive diagnostic error guiding the
  repository maintainer to configure GitHub secrets.

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

1. **Diff Filtering:** The generator MUST filter findings against `commit.diff`
   and discard findings that do not touch modified files or lines in the diff.
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

- **GIVEN** a scan output with 2 findings touching lines in `commit.diff`
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
   ```
   ````
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

### REQ-0008: Dedicated `github-actions/` Directory and Automated Installer

All GitHub Actions assets MUST be packaged under a dedicated `github-actions/`
directory:

- `github-actions/workflows/codemender.yml`: The complete standalone workflow
  template.
- `github-actions/scripts/install.sh`: An executable installation script that
  copies the workflow template and configuration files to a target repository
  with a single command.
- `github-actions/scripts/setup-wif.sh`: An executable helper script that runs
  `gcloud` commands to provision GCP Workload Identity Pool, Provider, Service
  Account, and IAM bindings.
- `github-actions/scripts/filter_findings.jq`: Standalone jq filter generating
  the dynamic fix matrix.
- `github-actions/scripts/publish_comments.py`: Standalone Python 3 standard
  library script translating ChangeEnvelope records into PR review suggestions
  and handling diff-boundary fallbacks.
- `github-actions/README.md`: Quickstart onboarding and configuration
  documentation.

The repository MUST NOT create or enable active workflows in
`.github/workflows/` on `cm-connect` to prevent automated CI execution on this
repository during development.

#### Scenario: Install workflow to target repository via installer script

- **GIVEN** a target repository at `/path/to/my-repo`
- **WHEN** the user executes
  `./github-actions/scripts/install.sh /path/to/my-repo`
- **THEN** the script MUST copy `github-actions/workflows/codemender.yml` to
  `/path/to/my-repo/.github/workflows/codemender.yml` and display next-step
  secret configuration instructions.

#### Scenario: Verify template isolation in cm-connect

- **GIVEN** the `cm-connect` codebase
- **WHEN** reviewing root directory paths
- **THEN** the workflow file MUST exist at
  `github-actions/workflows/codemender.yml` and
  `.github/workflows/codemender.yml` MUST NOT exist.

______________________________________________________________________

### REQ-TEST.2: PR Review Comment and Fallback Publisher Verification

The test suite MUST verify the PR review comment publisher script
(`github-actions/scripts/publish_comments.py`) across single-line suggestions,
multi-line suggestions, HTTP 422 diff-boundary fallbacks, unresolved findings,
step summary generation, and CLI execution interfaces.

#### Scenario: Publish single-line review suggestion comment

- **GIVEN** a `ChangeEnvelope` JSON fixture with a single-line hunk
  (`change_envelope_single_line.json` with `start_line: 42, end_line: 42`)
- **WHEN** `publish_comments.py` processes the envelope
- **THEN** it MUST invoke `POST /repos/{owner}/{repo}/pulls/{number}/comments` with
  `path: "pkg/auth/store.go"`, `line: 42`, `side: "RIGHT"`, omitting
  `start_line` and `start_side`, with a ```` ```suggestion ```` markdown body
  containing the single-line replacement.

#### Scenario: Publish multi-line review suggestion comment

- **GIVEN** a `ChangeEnvelope` JSON fixture with a multi-line hunk
  (`change_envelope_multiline.json` with `start_line: 42, end_line: 43`)
- **WHEN** `publish_comments.py` processes the envelope
- **THEN** it MUST invoke `POST /repos/{owner}/{repo}/pulls/{number}/comments` with
  `path: "pkg/auth/store.go"`, `start_line: 42`, `line: 43`,
  `start_side: "RIGHT"`, `side: "RIGHT"`, with a ```` ```suggestion ```` markdown
  body containing the multi-line replacement.

#### Scenario: Handle HTTP 422 error and fall back to top-level issue comment

- **GIVEN** a `ChangeEnvelope` where review comment creation rejects with
  HTTP 422 Unprocessable Entity (out of PR diff hunk)
- **WHEN** `publish_comments.py` catches the HTTP 422 error
- **THEN** it MUST NOT fail the step and MUST invoke
  `POST /repos/{owner}/{repo}/issues/{number}/comments` with
  `issue_number: <PR_NUMBER>` and a markdown body containing finding metadata
  and the patch inside a ```` ```diff ```` block.

#### Scenario: Handle unresolved finding without posting review comments

- **GIVEN** a `ChangeEnvelope` JSON fixture with `status: "UNRESOLVED"` and
  empty hunks (`change_envelope_unresolved.json`)
- **WHEN** `publish_comments.py` processes the unresolved envelope
- **THEN** it MUST NOT invoke review or issue comment APIs, and MUST log diagnostic
  information and write the unresolved status to the step summary.

#### Scenario: Generate GitHub Actions step summary

- **GIVEN** an execution of `publish_comments.py` with `$GITHUB_STEP_SUMMARY` set
  to a summary file path
- **WHEN** `publish_comments.py` finishes processing the change envelope
- **THEN** `$GITHUB_STEP_SUMMARY` MUST contain a markdown summary table or card
  detailing the finding status, severity, title, and modified files.

#### Scenario: Execute via CLI with zero external dependencies

- **GIVEN** `publish_comments.py`
- **WHEN** invoked via Python 3 standard library CLI (`python3 publish_comments.py <envelope.json>`)
- **THEN** it MUST parse the change envelope and publish review comments, issue comments,
  and step summaries without requiring third-party pip packages or Node.js runtimes.

