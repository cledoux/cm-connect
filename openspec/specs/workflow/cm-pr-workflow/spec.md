---
archetype: capability
status: accepted
category: workflow
name: cm-pr-workflow
governing_proposal: openspec/proposals/cm-pr-workflow/proposal.md
governing_adrs:
  - adrs/ADR-0001.md
  - adrs/ADR-0002.md
  - adrs/ADR-0003.md
  - adrs/ADR-0005.md
  - adrs/ADR-0006.md
  - adrs/ADR-0007.md
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
   (`github-actions/workflows/codemender.yml`, `github-actions/install.sh`,
   `github-actions/scripts/setup-wif.sh`,
   `github-actions/scripts/filter_findings.jq`,
   `github-actions/scripts/publish_comments.py`, and
   `github-actions/README.md`), ensuring zero unintentional workflow execution
   on `cm-connect`.

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

The `scan` job MUST execute `cm-runner find-diff` targeting the pull request
base and head commit SHAs
(`find-diff "${{ github.event.pull_request.base.sha }}" "${{ github.event.pull_request.head.sha }}"`)
and trap the exit code:

1. **Exit Code 0 (Clean / Empty Diff):** Scanner found zero vulnerabilities or
   the pull request diff was empty (0 bytes). The step MUST emit
   `has_findings=false` and terminate successfully without dispatching
   downstream remediation matrix jobs.
1. **Exit Code 1 (Findings Detected):** Scanner detected one or more
   vulnerabilities in modified lines. The step MUST NOT fail the job, but MUST
   capture the JSON output from `stdout` to `.codemender-out/findings.json` and
   set `has_findings=true`.
1. **Exit Code > 1 (Error):** Scanner encountered a fatal CLI, git, or runtime
   error. The step MUST fail immediately and propagate the non-zero exit code.

#### Scenario: Clean scan with zero vulnerabilities

- **GIVEN** a pull request with code changes containing no security
  vulnerabilities
- **WHEN** `cm-runner find-diff base.sha head.sha` terminates with exit code `0`
- **THEN** the scan step MUST set output `has_findings=false` and complete
  without scheduling downstream fix jobs.

#### Scenario: Trap exit code 1 on detected vulnerabilities

- **GIVEN** a pull request with vulnerabilities in modified PR files
- **WHEN** `cm-runner find-diff base.sha head.sha` terminates with exit code `1`
- **THEN** the workflow MUST capture the findings JSON payload, set
  `has_findings=true`, and proceed to dynamic matrix generation.

#### Scenario: Fast-path clean exit on empty diff

- **GIVEN** a pull request with zero modified source lines
- **WHEN** `cm-runner find-diff base.sha head.sha` runs
- **THEN** `cm-runner` MUST immediately emit `[]` on `stdout` and exit with code
  `0`.

#### Scenario: Propagate fatal scanner error

- **GIVEN** an invalid scanner invocation or container crash resulting in exit
  code `2`
- **WHEN** the scan step executes
- **THEN** the workflow MUST fail the step and output the scanner error message.

______________________________________________________________________

### REQ-0004: Dynamic Matrix Partitioning and Finding Scope Classification

When `has_findings=true`, the `scan` job MUST parse
`.codemender-out/findings.json` using `jq` against `commit.diff` and classify
findings into two streams:

1. **In-Diff Findings (Actionable / Blocking Fix Candidates):**
   - Findings whose `FilePath` and line range intersect lines added or modified
     in `commit.diff`.
   - If in-diff findings exceed a configurable maximum threshold $M$ (default:
     `10`), findings MUST be sorted by `Severity` (`CRITICAL` > `HIGH` >
     `MEDIUM` > `LOW`) and truncated to the top $M$ items.
   - Emitted to `outputs.findings_matrix` for parallel remediation via
     `cm-runner fix`.
   - If 1 or more in-diff findings exist, `outputs.has_findings` MUST be set to
     `true`.
1. **Out-of-Diff / Potentially Preexisting Findings (Advisory / Non-Blocking):**
   - Findings whose `FilePath` or line range fall outside the modified lines in
     `commit.diff`.
   - Out-of-diff findings MUST NOT spawn automated `fix` runner matrix jobs.
   - Out-of-diff findings MUST be captured in `outputs.preexisting_findings` (or
     formatted directly to PR issue comments / step summaries) and clearly
     labeled as **"Potentially Preexisting Finding (Advisory / Non-Blocking)"**.
   - Out-of-diff findings MUST NOT block CI checks or PR merging.
1. **Empty In-Diff Stream with Preexisting Findings:**
   - If all detected findings are out-of-diff, `outputs.has_findings` MUST be
     set to `false` (bypassing `fix` matrix jobs), while the workflow posts the
     advisory non-blocking comment on the PR / step summary and exits cleanly
     with status code `0`.

#### Scenario: Generate dynamic matrix for in-diff findings and separate preexisting findings

- **GIVEN** a scan output with 1 finding in modified `pkg/auth/store.go` and 1
  finding in untouched `legacy/db.go`
- **WHEN** the matrix generation step executes
- **THEN** `outputs.findings_matrix` MUST include only the finding for
  `pkg/auth/store.go`
- **AND** `outputs.has_findings` MUST be `true`
- **AND** the untouched finding in `legacy/db.go` MUST be emitted as a
  potentially preexisting advisory finding.

#### Scenario: All findings are out-of-diff preexisting vulnerabilities

- **GIVEN** a scan output where all findings fall on lines untouched by
  `commit.diff`
- **WHEN** the matrix generation step executes
- **THEN** `outputs.findings_matrix` MUST be `[]`
- **AND** `outputs.has_findings` MUST be `false`
- **AND** the workflow MUST post a non-blocking advisory PR summary noting the
  potentially preexisting findings
- **AND** the PR check run MUST conclude successfully with exit code `0`.

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

### REQ-0007: Diff-Boundary Fallback & Potentially Preexisting Advisory Comments

1. **HTTP 422 Diff-Boundary Fallback:**
   - If the GitHub API rejects an inline review comment with HTTP
     `422 Unprocessable Entity` (indicating the targeted fix hunk falls outside
     the pull request's diff hunk):
     - The publisher MUST catch the error without failing the job step.
     - The publisher MUST fall back to creating a top-level pull request issue
       comment (`POST /repos/{owner}/{repo}/issues/{id}/comments`) or writing to
       `$GITHUB_STEP_SUMMARY`.
     - The fallback comment MUST contain the finding title, summary, and the
       unified diff patch in a ```` ```diff ```` code block.
1. **Potentially Preexisting Finding Advisory Comments:**
   - For findings identified by the scanner that do not intersect modified lines
     in `commit.diff`:
     - The comment publisher or workflow step MUST format and publish a
       top-level pull request issue comment or step summary.
     - The comment header MUST clearly state:
       `### 🛡️ CodeMender Advisory: Potentially Preexisting Security Finding (Non-Blocking)`.
     - The comment MUST include the file path, line number, severity,
       vulnerability type, analysis summary, and an explicit disclaimer that
       this finding is outside the active pull request diff and does not block
       PR approval or merging.

#### Scenario: Fallback to top-level comment on out-of-diff rejection

- **GIVEN** a valid fix patch on a file line that GitHub rejects with HTTP 422
- **WHEN** `createReviewComment` throws an error
- **THEN** the publisher MUST post a top-level PR comment with the diff patch
  and log the fallback.

#### Scenario: Post non-blocking advisory comment for potentially preexisting finding

- **GIVEN** a scan finding on an untouched file line outside `commit.diff`
- **WHEN** the advisory comment publisher runs
- **THEN** it MUST post a top-level PR issue comment or `$GITHUB_STEP_SUMMARY`
  card marked as non-blocking and advisory
- **AND** the check status MUST remain successful (green).

______________________________________________________________________

### REQ-0008: Dedicated `github-actions/` Directory and Automated Installer with Target GHCR Build & Push

All GitHub Actions assets MUST be packaged under a dedicated `github-actions/`
directory:

- `github-actions/workflows/codemender.yml`: The complete standalone workflow
  template.
- `github-actions/install.sh`: An executable installation script located
  directly under `github-actions/install.sh` that automates discovery of the
  target repository's git remote slug, builds the `cm-runner` container image
  locally from `cm-connect`, logs into GitHub Container Registry (`ghcr.io`) via
  the GitHub CLI (`gh`), pushes the image directly to the target repository's
  GHCR namespace (`ghcr.io/<target-owner>/<target-repo>/cm-runner:latest`),
  templates `codemender.yml` with the resolved image tag, and installs the
  workflow and helper scripts into the target repository.
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

1. **Target Slug Auto-Discovery:** `install.sh` MUST automatically detect the
   target repository slug (`owner/repo`) from the target repository's git remote
   (`git -C <target-dir> config --get remote.origin.url`), while supporting an
   explicit override via `--repo <owner/repo>`.
1. **GHCR Authentication & Local Build:** By default, `install.sh` MUST
   authenticate to `ghcr.io` using `gh auth token`, compile and build the
   `cm-runner` container image from local `docker/Dockerfile`, and attach the
   OCI image source label
   (`org.opencontainers.image.source=https://github.com/<owner>/<repo>`).
1. **Image Push & Templating:** `install.sh` MUST push the image to
   `ghcr.io/<owner>/<repo>/cm-runner:latest` and template
   `github-actions/workflows/codemender.yml` to reference that exact image tag
   when copying to `<target-repo>/.github/workflows/codemender.yml`.
1. **Configurable Flags:** `install.sh` MUST support:
   - `--skip-build`: Skips local Docker building and pushing, copying assets
     directly.
   - `--image <custom-image>`: Overrides the container image reference in the
     templated workflow.
   - `--repo <owner/repo>`: Explicitly specifies the target repository slug.
1. **Helper Script Installation:** `install.sh` MUST copy `filter_findings.jq`,
   `publish_comments.py`, and `setup-wif.sh` from `github-actions/scripts/` into
   `<target-repo>/.github/scripts/` and set executable permissions.
1. **Repository Isolation:** The repository MUST NOT create or enable active
   workflows in `.github/workflows/codemender.yml` on `cm-connect` to prevent
   automated CI execution on this repository during development.

#### Scenario: Full automated installation with local build and GHCR push

- **GIVEN** a target git repository at `/path/to/target-repo` with remote
  `git@github.com:my-org/my-app.git`
- **WHEN** executing `./github-actions/install.sh /path/to/target-repo`
- **THEN** `install.sh` MUST discover slug `my-org/my-app`
- **AND** authenticate to `ghcr.io` via `gh auth token`
- **AND** build and push `ghcr.io/my-org/my-app/cm-runner:latest`
- **AND** template `/path/to/target-repo/.github/workflows/codemender.yml` with
  `ghcr.io/my-org/my-app/cm-runner:latest`
- **AND** copy helper scripts to `/path/to/target-repo/.github/scripts/`.

#### Scenario: Fast installation with --skip-build flag

- **GIVEN** an invocation
  `./github-actions/install.sh --skip-build /path/to/target-repo`
- **WHEN** the script executes
- **THEN** it MUST skip Docker build and push steps and copy workflow and script
  files to `/path/to/target-repo/.github/`.

#### Scenario: Custom image tag override via --image flag

- **GIVEN** an invocation
  `./github-actions/install.sh --image custom-registry.io/org/cm:v1 /path/to/target-repo`
- **WHEN** the script executes
- **THEN** the installed workflow in
  `/path/to/target-repo/.github/workflows/codemender.yml` MUST reference
  `custom-registry.io/org/cm:v1`.

#### Scenario: Verify template isolation in cm-connect

- **GIVEN** the `cm-connect` codebase
- **WHEN** reviewing root directory paths
- **THEN** the workflow file MUST exist at
  `github-actions/workflows/codemender.yml`
- **AND** the installer MUST exist at `github-actions/install.sh`
- **AND** `.github/workflows/codemender.yml` MUST NOT exist.

______________________________________________________________________

### REQ-0009: PR Review Comment and Fallback Publisher Verification

The test suite MUST verify the PR review comment publisher script
(`github-actions/scripts/publish_comments.py`) across single-line suggestions,
multi-line suggestions, HTTP 422 diff-boundary fallbacks, unresolved findings,
step summary generation, and CLI execution interfaces.

#### Scenario: Publish single-line review suggestion comment

- **GIVEN** a `ChangeEnvelope` JSON fixture with a single-line hunk
  (`change_envelope_single_line.json` with `start_line: 42, end_line: 42`)
- **WHEN** `publish_comments.py` processes the envelope
- **THEN** it MUST invoke `POST /repos/{owner}/{repo}/pulls/{number}/comments`
  with `path: "pkg/auth/store.go"`, `line: 42`, `side: "RIGHT"`, omitting
  `start_line` and `start_side`, with a ```` ```suggestion ```` markdown body
  containing the single-line replacement.

#### Scenario: Publish multi-line review suggestion comment

- **GIVEN** a `ChangeEnvelope` JSON fixture with a multi-line hunk
  (`change_envelope_multiline.json` with `start_line: 42, end_line: 43`)
- **WHEN** `publish_comments.py` processes the envelope
- **THEN** it MUST invoke `POST /repos/{owner}/{repo}/pulls/{number}/comments`
  with `path: "pkg/auth/store.go"`, `start_line: 42`, `line: 43`,
  `start_side: "RIGHT"`, `side: "RIGHT"`, with a ```` ```suggestion ````
  markdown body containing the multi-line replacement.

#### Scenario: Handle HTTP 422 error and fall back to top-level issue comment

- **GIVEN** a `ChangeEnvelope` where review comment creation rejects with HTTP
  422 Unprocessable Entity (out of PR diff hunk)
- **WHEN** `publish_comments.py` catches the HTTP 422 error
- **THEN** it MUST NOT fail the step and MUST invoke
  `POST /repos/{owner}/{repo}/issues/{number}/comments` with
  `issue_number: <PR_NUMBER>` and a markdown body containing finding metadata
  and the patch inside a ```` ```diff ```` block.

#### Scenario: Handle unresolved finding without posting review comments

- **GIVEN** a `ChangeEnvelope` JSON fixture with `status: "UNRESOLVED"` and
  empty hunks (`change_envelope_unresolved.json`)
- **WHEN** `publish_comments.py` processes the unresolved envelope
- **THEN** it MUST NOT invoke review or issue comment APIs, and MUST log
  diagnostic information and write the unresolved status to the step summary.

#### Scenario: Generate GitHub Actions step summary

- **GIVEN** an execution of `publish_comments.py` with `$GITHUB_STEP_SUMMARY`
  set to a summary file path
- **WHEN** `publish_comments.py` finishes processing the change envelope
- **THEN** `$GITHUB_STEP_SUMMARY` MUST contain a markdown summary table or card
  detailing the finding status, severity, title, and modified files.

#### Scenario: Execute via CLI with zero external dependencies

- **GIVEN** `publish_comments.py`
- **WHEN** invoked via Python 3 standard library CLI
  (`python3 publish_comments.py <envelope.json>`)
- **THEN** it MUST parse the change envelope and publish review comments, issue
  comments, and step summaries without requiring third-party pip packages or
  Node.js runtimes.
