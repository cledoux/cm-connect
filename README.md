# CodeMender Connect (`cm-connect`)

> **Stateless Orchestration & CI/CD Gating for CodeMender**

`cm-connect` is an orchestrator that transforms the
[CodeMender](https://docs.cloud.google.com/gemini-enterprise-agent-platform/codemender/set-up-environment)
(`cm`) binary into a **stateless, deterministic job** that requires zero local
storage to survive across executions.

______________________________________________________________________

## Why Does This Repo Exist?

CodeMender is Google's AI-driven security reasoning engine, capable of finding
vulnerabilities, verifying exploitability, and automatically synthesizing code
remediations.

However, CodeMender is designed out of the box as a stateful, interactive
developer CLI:

- **Stateful Local Storage:** It expects a local SQLite database
  (`~/.codemender/state.db`) to track sessions, findings, and remediation
  workflows.
- **Interactive Stalls:** It prompts the user interactively before running
  commands or modifying files (`confirm_commands`, `confirm_writes`).
- **In-Place Working Tree Mutations:** Dynamic exploit verification and patch
  generation write directly to the local repository, littering the workspace
  with build artifacts, dependencies, and partial edits.

### The Problem in Modern CI/CD & Cloud Pipelines

In automated CI/CD runners (such as GitHub Actions), serverless environments
(Cloud Run), and container clusters (GKE):

- **Persistent local storage does not exist** across ephemeral runner VMs or
  container jobs.
- **Shared SQLite databases lock up** when multiple concurrent worker jobs try
  to write simultaneously.
- **Dirty host working copies** corrupt subsequent build steps or cause flaky
  tests.
- **Interactive prompts** hang headless automation indefinitely.

**If you want to run CodeMender reliably in CI/CD, parallel PR review gating, or
ephemeral cloud jobs without managing persistent host disks or dealing with
broken workspaces, `cm-connect` is built for you.**

______________________________________________________________________

## The Solution: A Stateless Orchestrator & Ephemeral Container Protocol

`cm-connect` wraps CodeMender in an ultra-lightweight orchestrator and container
protocol designed specifically for headless, parallel, and stateless execution:

1. **Lightweight Compiled Go Orchestrator (`cm-runner`):**
   - Statically compiled Go entrypoint with sub-millisecond (~1ms) startup
     latency and zero external runtime dependencies (`go.mod` stdlib only).
   - Enforces exact subcommand routing (`find`, `find-diff`, `fix`, `shell`),
     process group signal propagation (`Setpgid: true`), and strict I/O stream
     separation (`stdout` pure JSON / `stderr` operational diagnostics).
1. **Ephemeral Copy-on-Write Workspace Sandbox (OverlayFS):**
   - The host repository is mounted **strictly read-only** (`:ro`).
   - An ephemeral Copy-on-Write (CoW) layer (`fuse-overlayfs`) intercepts all
     filesystem mutations.
   - CodeMender gets unrestricted POSIX read/write access to compile code, run
     test suites, and generate patches, while the host working copy remains
     **100% untouched and pristine**.
   - Patches are cleanly extracted as unified Git diffs (`.patch`) from the CoW
     scratch layer.
1. **Stateless Lifecycle Decoupling:**
   - Decouples vulnerability discovery (`find` / `find-diff`) from patch
     remediation (`fix`).
   - Each remediation job runs statelessly on a single finding payload,
     eliminating multi-finding patch collisions and database lock contention.
1. **Strict Userspace Security:**
   - Runs strictly as an unprivileged, non-root user (`codemender`, UID 1000,
     GID 1000).
1. **Keyless Cloud Authentication:**
   - Native support for Google Cloud Application Default Credentials (ADC),
     OAuth access tokens (`CLOUDSDK_AUTH_ACCESS_TOKEN`), and Workload Identity
     Federation (WIF) with GitHub Actions OIDC.

*For detailed architectural specifications and design rationale, see our
[Architecture Decision Records](adrs/index.md) and
[OpenSpec Capabilities](openspec/specs/).*

______________________________________________________________________

## Where We Are Today

Today, `cm-connect` provides an end-to-end stateless scanning and remediation
pipeline:

### 1. Text-Based Protocol for Findings & Fixes

- **Headless Scanning (`cm-runner find` / `cm-runner find-diff`):** Executes
  static and AI-assisted scanning and emits structured, machine-readable JSON
  finding arrays directly to `stdout`.
- **Stateless Single-Finding Remediation (`cm-runner fix`):** Ingests a single
  finding payload from a file or standard input (`stdin`), seeds an ephemeral
  in-container database, executes the remediation agent in the OverlayFS
  sandbox, and emits a structured `ChangeEnvelope` JSON record containing
  unified diff hunks.
- **Massive Parallelism:** Because each `fix` container is completely stateless
  and self-contained, CI/CD orchestrators can fan out dozens of concurrent fix
  jobs in parallel with zero shared state.

### 2. Turnkey GitHub Actions PR Review Gating

- **Diff-Scoped Scanning:** Analyzes incoming pull request diffs
  (`commit.diff`), eliminating noise from untouched legacy code.
- **Dynamic Parallel Fix Matrix:** Evaluates scan findings, filters out-of-diff
  items, sorts by severity, and dynamically spawns parallel fix matrix jobs.
- **1-Click Apply Review Suggestions:** Automatically translates
  `ChangeEnvelope` hunk records into inline GitHub Pull Request Review comments
  (```` ```suggestion ```` blocks) on PR review threads with automatic HTTP 422
  diff-boundary fallbacks.
- **1-Command Installer:** Includes an automated installer
  (`github-actions/install.sh`) that builds and pushes the container image to
  GitHub Container Registry (GHCR) and templates the workflow into your
  repository.

👉 **Read the full
[GitHub Actions Integration & Setup Guide](github-actions/README.md)** for
quickstart instructions, WIF configuration, and Terraform automation.

### 3. Interactive Developer Sandbox (`cm-shell`)

Developers can still drop into an interactive userspace shell for local
experimentation:

```bash
./bin/cm-shell
```

______________________________________________________________________

## Where We Want to Go Next (Roadmap)

We are actively expanding `cm-connect` to support broader cloud-native and
enterprise deployment models:

1. **Centralized External State Store (Google Cloud Firestore / Spanner):**
   - Connect CodeMender with an external database (e.g. Google Cloud Firestore
     or Cloud SQL) to persist findings, triage statuses, fix histories, and scan
     trends across ephemeral container executions without requiring local disks.
1. **Scheduled & Serverless Security Sweeps (Cloud Run / GKE):**
   - Facilitate scheduled repository sweeps, continuous compliance monitoring,
     and batch vulnerability audits using serverless container runners on
     **Google Cloud Run** and **Google Kubernetes Engine (GKE)**.
1. **Hermetic Dynamic Exploit Verification (`verify`):**
   - Build runtime execution environments tailored for application build
     isolation and dynamic exploit PoC execution.
1. **Enterprise Policy Gating & Multi-VCS Exploration:**
   - Implement configurable PR gating thresholds, SARIF compliance export, and
     explore adapters for Mercurial (`hg`), Jujutsu (`jj`), and Google Piper.

______________________________________________________________________

## Quickstart & Local Usage

### Prerequisites

1. **Docker**: Docker Engine running locally.
1. **Google Cloud ADC**: Authenticated on the host:
   ```bash
   gcloud auth application-default login
   ```
1. **GCP Permissions**: `roles/aiplatform.user` on the project hosting Vertex
   AI.

### Build the Runner Image

```bash
make build
```

### Run Batch Scanning

```bash
# Scan full repository (emits JSON to stdout, logs to stderr)
./bin/cm-runner find

# Scan scoped sub-path
./bin/cm-runner find src/auth

# Scan pull request diff between revisions
./bin/cm-runner find-diff origin/main HEAD
```

### Run Stateless Remediation

```bash
# Remediate a single finding from JSON payload
./bin/cm-runner fix /path/to/finding.json

# Remediate via standard input
cat finding.json | ./bin/cm-runner fix -
```

______________________________________________________________________

## Documentation & Reference Index

- **[GitHub Actions CI/CD Integration Guide](github-actions/README.md)** —
  Production PR review workflow, WIF setup, and Terraform templates.
- **[Architecture Decision Records (ADRs)](adrs/index.md)** — Complete index of
  architectural decisions (`ADR-0001` through `ADR-0008`).
- **[OpenSpec Capability Specifications](openspec/specs/)** — Normative specs
  and design documents:
  - [`cm-batch-runner`](openspec/specs/runner/cm-batch-runner/spec.md) (Headless
    Scanner)
  - [`cm-fix-runner`](openspec/specs/runner/cm-fix-runner/spec.md) (Stateless
    Fix Runner)
  - [`cm-pr-workflow`](openspec/specs/workflow/cm-pr-workflow/spec.md) (GitHub
    Actions PR Workflow)
  - [`cm-docker-container`](openspec/specs/runner/cm-docker-container/spec.md)
    (Interactive Sandbox)
