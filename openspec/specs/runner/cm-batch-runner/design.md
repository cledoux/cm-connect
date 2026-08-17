---
archetype: capability
status: accepted
category: runner
name: cm-batch-runner
governing_spec: openspec/specs/runner/cm-batch-runner/spec.md
governing_adr: adrs/ADR-0001.md
---

# CodeMender Headless Batch Scanner Container Design (`find`)

## 1. Context & Objectives

To support automated CI/CD gating (e.g. GitHub Actions PR workflows) and
programmatic security analysis, `cm-batch-runner` establishes a headless,
deterministic, and completely stateless Docker scanner container for CodeMender
(`cm`).

Implementing [ADR-0001](../../../../adrs/ADR-0001.md), this capability is
focused strictly on the **`find`** vulnerability scanner phase while
establishing an extensible **Host $\\leftrightarrow$ Container Communication
Protocol** that seamlessly scales to future lifecycle commands (`verify`, `fix`,
`report`).

In CodeMender's underlying CLI architecture:

- `cm find <path> [flags]` performs LLM-driven vulnerability discovery against
  the codebase and records findings in a local SQLite state database
  (`state.db`).
- `cm report --format=<format>` queries the local SQLite state database and
  formats the findings into machine-readable payloads (`json`, `sarif`) or
  human-readable formats (`table`, `html`, `md`).

The `cm-runner` entrypoint encapsulates this workflow into a unified batch
protocol:

1. Validates the subcommand and resolves the scan target path against
   `/workspace`.
1. Executes **Phase 1 (`cm find`)** with all tool steps and progress routed to
   `stderr`.
1. Executes **Phase 2 (`cm report --format json`)** to query the resulting
   SQLite database and emit pure, structured findings on `stdout`.
1. Evaluates the findings count to emit a deterministic exit code (`0` for clean
   codebases, `1` when vulnerabilities are detected, `2` for usage errors, `> 2`
   for fatal errors).

Dynamic exploit verification (`verify`) and patch remediation (`fix`) are
explicitly scoped out of this design:

- `verify` requires repo-specific application runtime environments and will be
  addressed in a dedicated follow-up capability.
- `fix` requires a dedicated finding import mechanism to populate CodeMender's
  internal database prior to remediation and will also be handled in a separate
  spec.

### Goals

- Establish an unambiguous, extensible Host $\\leftrightarrow$ Container
  Communication Protocol.
- Implement a two-phase scan and report execution pipeline (`cm find`
  $\\rightarrow$ SQLite $\\rightarrow$ `cm report`).
- Default to structured, machine-readable JSON formatting
  (`cm report --format=json`) on `stdout`.
- Support format overrides (`sarif`, `table`, `html`, `md`) passed via
  `--format` or `-f`.
- Route all progress spinners, diagnostic logs, and session log paths strictly
  to `stderr`.
- Support both full repository scans (`.`) and scoped sub-path scans (e.g.
  `./src/auth`) while always retaining full repository context in `/workspace`.
- Pre-initialize default configuration at container build time so runtime
  `cm init` is never required.
- Require an explicit subcommand (`find`, `shell`), rejecting ambiguous empty
  invocations with exit code 2.
- Provide interactive debugging support via `shell` with strict TTY validation.
- Provide instant startup (\<1ms) and robust process group signal handling via a
  compiled Go entrypoint binary.
- Support multi-mode authentication (ADC directory mount or
  `CLOUDSDK_AUTH_ACCESS_TOKEN` / `GOOGLE_APPLICATION_CREDENTIALS`).
- Enforce strict unprivileged userspace execution (user `codemender`, UID 1000).

### Non-Goals

- Executing `verify` (deferred to repo-specific runtime runner capability).
- Executing `fix` (deferred to finding ingestion and remediation capability).
- Defaulting to a hidden command when no arguments are provided.
- Running container processes or shells as `root`.
- Host-level network egress proxying or firewall configuration (CodeMender
  handles sandbox isolation internally).

______________________________________________________________________

## 2. Host $\\leftrightarrow$ Container Communication Protocol

The communication protocol defines formal, isolated channels across the
host-container boundary:

```mermaid
flowchart TD
    subgraph Host["Host / Orchestrator / CI Runner"]
        WorkspaceMount["Channel 1: Repository Root Volume<br>-v $(pwd):/workspace (Full Context)"]
        AuthInjection["Channel 2: Auth Injection<br>-e CLOUDSDK_AUTH_ACCESS_TOKEN<br>or -v gcloud:ro"]
        CommandArgs["Channel 3: CLI Subcommand & Target Path<br>find [path] [flags], shell"]
        
        StdoutPipe["Channel A: stdout (Data Payload Stream)<br>Clean Structured JSON/SARIF -> jq / CI Gating"]
        StderrPipe["Channel B: stderr (Diagnostics Stream)<br>Logs, Spinners, Traces -> CI Console"]
        ExitCodeSignal["Channel C: Exit Code (Verdict Signal)<br>0: Clean, 1: Findings, 2: CLI/Path/TTY Error"]
    end

    subgraph Container["Stateless Container Sandbox (USER: codemender / UID 1000)"]
        direction TB
        GoRunner["Go Entrypoint Runner<br>(/usr/local/bin/cm-runner)"]
        
        subgraph SubcommandSwitch["Subcommand & Path Dispatcher"]
            FindPipeline["find Subcommand<br>Phase 1: cm find &lt;path&gt; (scan)<br>Phase 2: cm report (format)"]
            ShellBranch["shell Subcommand<br>validates TTY (isatty)"]
            InvalidBranch["Missing / Unknown Subcommand / Bad Path<br>emit usage & exit 2"]
        end
        
        CMFind["Phase 1: CodeMender Scanner<br>/usr/local/bin/cm find &lt;target&gt;"]
        SQLiteDB["SQLite State DB<br>/home/codemender/.codemender/state.db"]
        CMReport["Phase 2: CodeMender Reporter<br>/usr/local/bin/cm report --format=&lt;fmt&gt;"]
        BashShell["Interactive Shell<br>(/bin/bash)"]
        Preconfig["Pre-seeded Build-Time Config<br>(/home/codemender/.codemender)"]
    end

    subgraph Cloud["Google Cloud Backend"]
        Vertex["Vertex AI API<br>(aiplatform.googleapis.com)"]
    end

    %% Ingestion Flow
    WorkspaceMount -.->|Mount full repo to /workspace| Container
    AuthInjection -.->|Inject credentials| Container
    CommandArgs -->|docker run args| GoRunner

    %% Dispatch Flow
    GoRunner -->|Parses & validates| SubcommandSwitch
    SubcommandSwitch -->|find [path] [flags]| FindPipeline
    SubcommandSwitch -->|shell| ShellBranch
    SubcommandSwitch -->|empty / invalid / bad path| InvalidBranch

    %% Phase 1 Execution
    FindPipeline -->|Step 1: Execute scan| CMFind
    Preconfig -.->|Provides default DB/config| CMFind
    CMFind -->|Direct HTTPS (Port 443)| Vertex
    CMFind -->|Writes findings| SQLiteDB
    CMFind -->|stderr: Scanning progress & logs| StderrPipe

    %% Phase 2 Execution
    FindPipeline -->|Step 2 (on scan success)| CMReport
    SQLiteDB -->|Reads findings| CMReport
    CMReport -->|stdout: Clean JSON / SARIF payload| StdoutPipe
    CMReport -->|stderr: Session log notices| StderrPipe

    %% Interactive & Error Branches
    ShellBranch -->|TTY valid| BashShell
    ShellBranch -->|TTY missing| StderrPipe
    InvalidBranch --> StderrPipe

    %% Exit Code Verdict
    FindPipeline -->|Evaluates finding count| ExitCodeSignal
    GoRunner -->|Propagates status| ExitCodeSignal
```

### 2.1 Input Channels (Host $\\rightarrow$ Container)

1. **Workspace Channel (Full Repository Context):**
   - The entire host repository is mounted to `/workspace` (container
     `WORKDIR`).
   - For `find`, read-only (`:ro`) or read-write mounts are supported.
   - Retaining the full repository root gives CodeMender access to root build
     files, dependency manifests, and workspace symbols even when scanning a
     specific sub-module.
1. **Scan Target Path Resolution:**
   - **Full Repository Scan (Default):** If the caller runs `find` without a
     positional path (e.g. `docker run <image> find`), `cm-runner` targets `.`.
   - **Scoped Sub-Path Scan:** If the caller specifies a sub-path (e.g.
     `docker run <image> find src/auth`), `cm-runner` validates that the
     directory/file exists inside `/workspace` and targets that path while
     keeping `/workspace` as the root context.
   - **Path Error:** If the sub-path does not exist in `/workspace`, `cm-runner`
     terminates immediately with exit code 2 and outputs an error to `stderr`.
1. **Authentication Channel:**
   - **Access Token:**
     `-e CLOUDSDK_AUTH_ACCESS_TOKEN="$(gcloud auth print-access-token)"` (ideal
     for ephemeral CI runners).
   - **Workload Identity:**
     `-e GOOGLE_APPLICATION_CREDENTIALS=/auth/creds.json -v /host/path:/auth/creds.json:ro`.
   - **Host ADC Directory:**
     `-v ~/.config/gcloud:/home/codemender/.config/gcloud:ro` (ideal for local
     development).
1. **Command & Argument Channel:**
   - Explicit CLI tokens passed to `docker run <image> <subcommand> [flags]`.
   - Idempotent: `find` and `cm find` are treated identically.
   - Flag Separation: Scanner flags (`-c`, `-y`, `--unrestricted`) are passed to
     Phase 1 (`cm find`), while format flags (`-f`, `--format`) are passed to
     Phase 2 (`cm report`).

### 2.2 Output Channels (Container $\\rightarrow$ Host)

1. **Data Payload Stream (`stdout`):**
   - Exclusively reserved for structured machine-readable payloads emitted by
     `cm report`.
   - Guaranteed zero log contamination (no banner headers, progress text, or
     ANSI escape codes).
   - Pipeable directly to `jq`, artifact uploaders, or downstream CI steps.
1. **Diagnostics Stream (`stderr`):**
   - Captures all human-readable diagnostic messages, progress spinners, LLM
     reasoning telemetry, session log paths, and error traces from both phases.
   - Redirectable to CI step logs (`2> run.log`).
1. **Verdict & Status Channel (Exit Codes):**
   - `0`: Scan completed cleanly with zero findings.
   - `1`: Scan completed with findings detected (actionable CI PR gating
     signal).
   - `2`: Invocation / CLI error (missing subcommand, non-existent target path,
     unrecognized flag, or `shell` invoked without pseudo-TTY).
   - `> 2`: Fatal tooling or authentication error.

______________________________________________________________________

## 3. Structured Output Schema

### 3.1 Default JSON Schema (`cm report --format=json`)

When invoked without format flags or with `--format=json`, `cm-runner` emits a
JSON array of finding objects on `stdout`:

```json
[
  {
    "FindingID": "478a8868-b05a-5258-99ac-aa9e932374a7",
    "SessionID": "ChAyYWI5YWYzYjg0MTc0YzgwEAgaATAqBG1haW4",
    "Title": "XML External Entity (XXE) Injection via File Upload",
    "FilePath": "/workspace/lib/xml.ts",
    "Severity": "CRITICAL",
    "Confidence": 100,
    "Analysis": "An XML External Entity (XXE) Injection vulnerability exists in the XML file upload handling logic...",
    "Snippet": "export async function parseXmlString (data: string, timeoutMs = 2000): Promise<string> {\n  const libxml2 = await loadLibxml2()...",
    "VulnType": "XXE",
    "VulnID": "CWE-611",
    "Fingerprint": "",
    "Status": "OPEN",
    "SourceStage": "",
    "FindingJSON": "",
    "UpdatedAt": "",
    "StartLine": 29,
    "EndLine": 38,
    "DismissReason": "",
    "ConfidenceLevel": ""
  }
]
```

### 3.2 SARIF v2.1.0 Schema (`cm report --format=sarif`)

When invoked with `-f sarif` or `--format=sarif`, `cm-runner` emits a standard
OASIS SARIF v2.1.0 document on `stdout`:

```json
{
  "$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json",
  "version": "2.1.0",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "CodeMender",
          "version": "dev"
        }
      },
      "results": [
        {
          "ruleId": "XXE",
          "level": "error",
          "message": {
            "text": "XML External Entity (XXE) Injection via File Upload: ..."
          },
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": {
                  "uri": "/workspace/lib/xml.ts"
                }
              }
            }
          ]
        }
      ]
    }
  ]
}
```

______________________________________________________________________

## 4. Decisions & Rationale

### 4.1 Decision 1: Two-Phase Scan & Report Orchestration

- **Choice:** `cm-runner find` executes `cm find <path>` (Phase 1) followed by
  `cm report --format <fmt>` (Phase 2).
- **Rationale:** CodeMender's `cm find` command is designed for scanning and
  updating local state; it does not natively support direct structured JSON
  output on stdout. In contrast, `cm report` specializes in querying the SQLite
  state database and exporting cleanly formatted findings. By coordinating both
  phases inside the Go runner, we provide callers with a single, seamless `find`
  command that directly returns machine-readable results.
- **Alternatives Considered:**
  - *Attempting to parse `cm find` terminal output:* Highly brittle, susceptible
    to ANSI escape sequences and UI changes.
  - *Requiring callers to execute two container commands (`find` then
    `report`):* Burdens CI/CD pipelines with maintaining persistent state
    volumes between container runs.

### 4.2 Decision 2: Format Negotiation & Strict Filter Exclusion

- **Choice:** `cm-runner find` inspects CLI arguments, forwards scan-specific
  flags (`-c`, `-y`, `--unrestricted`) to `cm find`, and configures the Phase 2
  report generation using `--format <fmt>` (defaulting to `--format=json`).
  Report filtering flags (e.g. `--severity`, `--status`, `--session`, `--sort`)
  are intentionally NOT supported on `find`.
- **Rationale:** Keeps the batch `find` execution contract clean and
  predictable. The container always exports the complete, unfiltered findings
  dataset in the requested structure, allowing downstream consumers, CI gates,
  and PR bots to perform whatever filtering or triage they require without state
  divergence.

### 4.3 Decision 3: Finding-Aware Exit Code Evaluation

- **Choice:** `cm-runner` inspects the report payload:
  - If findings array is non-empty (`len > 0`): Exits with code `1`
    (`ExitFindings`).
  - If findings array is empty (`[]` or `null`): Exits with code `0`
    (`ExitClean`).
  - If a command fails: Propagates error exit code (`2` for usage, `>2` for
    fatal).
- **Rationale:** Provides standard CI/CD security gating semantics where exit
  code `1` signals actionable policy violations / security findings, while exit
  code `0` signals a clean bill of health.

### 4.4 Decision 4: Target Path Normalization & Full Repo Context

- **Choice:** `cm-runner` normalizes the scan target: defaults to `.` if no
  positional path is provided, forwards valid subpaths (e.g. `src/auth`), and
  checks path existence inside `/workspace` before invoking `cm`.
- **Rationale:** Code analysis tools perform significantly better when they have
  access to top-level dependency manifests and configuration files. Mounting the
  full repo at `/workspace` and passing sub-paths as arguments supports targeted
  scanning without blinding CodeMender to root repo context.

### 4.5 Decision 5: Explicit `shell` Subcommand with TTY Enforcement

- **Choice:** An explicit `shell` subcommand launches `/bin/bash`. If standard
  input is not a terminal (missing `-it`), `cm-runner` terminates immediately
  with exit code 2.
- **Rationale:** Running a non-interactive shell hangs batch scripts
  indefinitely waiting for EOF or input. Detecting TTY presence upfront prevents
  pipeline stalls.

### 4.6 Decision 6: Build-Time Configuration Pre-Initialization

- **Choice:** During Docker build, `cm init --dry-run || true` is executed under
  user `codemender` to generate default configuration and database structures in
  `/home/codemender/.codemender`.
- **Rationale:** Eliminates any prerequisite `cm init` step at runtime. Scans
  are completely stateless and work immediately upon container boot.

______________________________________________________________________

## 5. Multi-Stage Dockerfile Architecture

```dockerfile
# Stage 1: Build Go runner binary
FROM golang:1.24-bookworm AS builder
WORKDIR /app
COPY cmd/cm-runner /app/cmd/cm-runner
COPY pkg/cmrunner /app/pkg/cmrunner
COPY go.mod go.sum* /app/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /app/bin/cm-runner ./cmd/cm-runner

# Stage 2: Runtime image
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Install core build and developer tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    unzip \
    git \
    python3 \
    python3-pip \
    build-essential \
    make \
    jq \
    && rm -rf /var/lib/apt/lists/*

# Download and install CodeMender CLI
ARG CM_DOWNLOAD_URL="https://artifactregistry.googleapis.com/download/v1/projects/cmoc-prod/locations/us/repositories/codemender-cli-production/files/cm%3Astable%3Acm-linux-amd64.zip:download?alt=media"
RUN curl -fsSL -o /tmp/cm-linux-amd64.zip "${CM_DOWNLOAD_URL}" \
    && unzip /tmp/cm-linux-amd64.zip -d /tmp/cm-bin \
    && chmod +x /tmp/cm-bin/cm \
    && mv /tmp/cm-bin/cm /usr/local/bin/cm \
    && rm -rf /tmp/cm-linux-amd64.zip /tmp/cm-bin

# Install compiled Go entrypoint runner from builder stage
COPY --from=builder /app/bin/cm-runner /usr/local/bin/cm-runner
RUN chmod +x /usr/local/bin/cm-runner

# Create unprivileged user codemender and directories
RUN groupadd -g 1000 codemender \
    && useradd -u 1000 -g codemender -m -s /bin/bash codemender \
    && mkdir -p /workspace /home/codemender/.codemender /home/codemender/.config/gcloud \
    && chown -R codemender:codemender /workspace /home/codemender

# Switch to codemender and pre-initialize default config at build time
USER codemender
WORKDIR /workspace

# Run build-time initialization to pre-seed default config
RUN cm init --dry-run || true

ENTRYPOINT ["/usr/local/bin/cm-runner"]
```

______________________________________________________________________

## 6. Risks & Mitigations

| Risk                                                                            | Mitigation                                                                                                                           |
| :------------------------------------------------------------------------------ | :----------------------------------------------------------------------------------------------------------------------------------- |
| **Pipeline stalls when `shell` is invoked in non-interactive CI**               | `cm-runner` inspects terminal status and exits immediately with code 2 if `isatty(0)` is false.                                      |
| **Exit code 1 causes CI scripts with `set -e` to fail prematurely on findings** | Document clear pattern in CI templates to capture exit code (e.g. `set +e` or `status=$?`).                                          |
| **User specifies non-existent target path**                                     | `cm-runner` validates path existence in `/workspace` upfront and emits clear error on `stderr` with code 2.                          |
| **Flag conflict between `cm find` and `cm report`**                             | `cm-runner` parses and splits flags, applying scan flags to Phase 1 and report flags to Phase 2.                                     |
| **Session state pollution across runs in persistent mounts**                    | `cm-runner` can filter by active session ID or invoke `cm clean` if non-persistent behavior is requested.                            |
| **Child toolchain orphaned processes on cancellation**                          | `cm-runner` uses process group signaling (`Setpgid: true` with `syscall.Kill(-pid, sig)`) to terminate child analyzer trees cleanly. |

______________________________________________________________________

## 7. Verification Strategy

1. **Two-Phase Scan Execution Test:** Run
   `docker run --rm -v $(pwd):/workspace <image> find`; verify Phase 1 executes
   `cm find` (progress to `stderr`) and Phase 2 executes
   `cm report --format=json` (clean JSON to `stdout`).
1. **Default Machine-Readable Output Test:** Pipe `stdout` directly to `jq .`;
   assert valid JSON with zero syntax errors.
1. **Format Override Test (SARIF):** Run
   `docker run --rm <image> find -f sarif`; verify standard SARIF 2.1.0 output
   on `stdout`.
1. **Clean Codebase Exit Code Test:** Scan clean codebase; verify exit code `0`
   and empty findings array.
1. **Vulnerability Detection Exit Code Test:** Scan vulnerable codebase; verify
   exit code `1` and populated findings array.
1. **Scoped Sub-Path Test:** Run
   `docker run -v $(pwd):/workspace <image> find src/auth`; verify targeted scan
   with workspace root preserved.
1. **Invalid Sub-Path Test:** Run `docker run <image> find non/existent/path`;
   assert immediate exit code 2 and path error on `stderr`.
1. **Missing TTY Test:** Run `docker run <image> shell` without `-it`; assert
   immediate exit code 2 and descriptive TTY error on `stderr`.
1. **Interactive TTY Test:** Run `docker run -it <image> shell`; assert
   interactive `/bin/bash` prompt in `/workspace` as user `codemender`.
1. **Build-Time Pre-Init Test:** Launch fresh container with only
   `-v $(pwd):/workspace`; execute `find` without `cm init` and assert scan
   succeeds.
1. **Signal Handling Test:** Send `SIGINT`/`SIGTERM` to running container;
   verify clean shutdown within 500ms.
1. **Unprivileged User Test:** Execute `docker run <image> id -u && id -g` to
   verify strict UID/GID 1000 enforcement.
