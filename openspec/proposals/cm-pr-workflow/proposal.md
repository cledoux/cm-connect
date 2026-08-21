# Proposal: CodeMender GitHub Actions CI/CD PR Review Workflow

**Change ID:** `cm-pr-workflow` \
**Status:** In Review \
**Author:** Charles LeDoux \
**Target Spec:** `openspec/specs/workflow/cm-pr-workflow/spec.md` \
**Governing ADR:** `adrs/ADR-0005.md`

## Why

Automated CI/CD pull request review gating requires a turnkey, zero-friction
GitHub Actions workflow that continuously validates code modifications against
security vulnerabilities.

With the completion of the headless batch scanner (`cm-runner find` per
ADR-0001) and the stateless remediation runner (`cm-runner fix` per ADR-0005),
`cm-connect` possesses the core engine to detect vulnerabilities and synthesize
structured, hunk-level patches (`ChangeEnvelope`).

To operationalize these capabilities for development teams, we need an
opinionated GitHub Actions workflow template and setup protocol. The workflow
must execute on every pull request to `main`, scan the codebase for
vulnerabilities, dynamically partition findings across parallel fix runner jobs,
and post review suggestions directly onto the pull request with 1-click apply
diff blocks.

## What Changes

- Create an example GitHub Actions workflow template
  (`.github/workflows/codemender.yml`) implementing a distributed two-stage
  CI/CD pipeline:
  1. **Scan Stage (`scan`):** Checks out the pull request codebase,
     authenticates to Google Cloud via Workload Identity Federation (WIF),
     executes `cm-runner find .`, and emits `findings.json`.
  1. **Partitioning & Dynamic Matrix Dispatch:** Evaluates the scanner exit code
     and findings array using `jq`. If clean (0 findings), completes immediately
     with a success check. If findings exist ($N \\ge 1$), dynamically
     constructs a GitHub Actions job matrix payload (`outputs.findings_matrix`)
     containing individual finding records.
  1. **Parallel Fix Stage (`fix` with `strategy: matrix`):** Launches $N$
     isolated matrix jobs in parallel, each executing `cm-runner fix` on a
     single finding payload to generate a structured `ChangeEnvelope` JSON
     record.
  1. **PR Review Suggestion Bot:** Translates `ChangeEnvelope` hunk records into
     GitHub Pull Request Review comments
     (`POST /repos/{owner}/{repo}/pulls/{id}/reviews`) with markdown
     ```` ```suggestion ```` blocks for instant review and 1-click application
     by developers.
- Implement intelligent **Diff-Boundary Fallback Handling**:
  - When a finding's line range falls **inside** the pull request diff, post as
    an **inline review suggestion**.
  - When a finding falls **outside** the pull request diff (e.g. pre-existing
    vulnerability in untouched code), gracefully fall back to posting a
    top-level pull request issue comment or `$GITHUB_STEP_SUMMARY` entry,
    preventing GitHub API `422 Unprocessable Entity` errors.
- Support keyless Google Cloud authentication using **Workload Identity
  Federation (WIF)** (`google-github-actions/auth`), mounting temporary ADC
  credentials securely into the Docker container without long-lived service
  account keys.
- Provide comprehensive repository setup and onboarding documentation
  (`docs/github_workflow_setup.md`) covering GCP IAM configuration, Workload
  Identity Pool binding, GitHub repository secrets, workflow permissions, and
  container registry hosting.

## Capabilities (The Core Contract)

### New Capabilities

- `cm-pr-workflow`: Turnkey GitHub Actions CI/CD workflow and setup protocol
  orchestrating automated pull request vulnerability scanning, dynamic matrix
  parallel patch remediation via `cm-runner fix`, and automated PR review
  comment generation with inline suggestion diffs. Maps to
  `openspec/specs/workflow/cm-pr-workflow/spec.md`.

### Modified Capabilities

- None.

## Impact

- Provides an out-of-the-box, drop-in GitHub Actions workflow that repository
  owners can install in minutes.
- Delivers rapid, parallelized security feedback to developers directly inside
  the GitHub Pull Request UI with 1-click apply remediation patches.
- Eliminates host state dependencies and multi-job database lock contention by
  leveraging the stateless `cm-runner fix` container protocol (ADR-0005).
