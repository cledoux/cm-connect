______________________________________________________________________

## archetype: capability status: accepted category: runner name: cm-batch-runner governing_spec: openspec/specs/runner/cm-batch-runner/spec.md governing_adr: adrs/ADR-0001.md

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

The container distinguishes between the mounted repository root (`/workspace`)
and the target scan path (full repo `.` by default, or scoped sub-tree like
`./src/auth`), pre-initializes CodeMender configuration structures at build time
(eliminating runtime `cm init`), mandates an explicit subcommand (`find`,
`shell`), outputs structured JSON data strictly on `stdout`, routes all
diagnostics to `stderr`, and runs under the unprivileged user `codemender` (UID
1000\) via a compiled Go entrypoint binary (`cm-runner`).

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
- Support both full repository scans (`.`) and scoped sub-path scans (e.g.
  `./src/auth`) while always retaining full repository context in `/workspace`.
- Pre-initialize default configuration at container build time so runtime
  `cm init` is never required.
- Require an explicit subcommand (`find`, `shell`), rejecting ambiguous empty
  invocations with exit code 2.
- Provide interactive debugging support via `shell` with strict TTY validation.
- Default to structured, machine-readable JSON formatting on `stdout`.
- Direct all diagnostic banners, logs, and progress indicators strictly to
  `stderr`.
- Provide instant startup (\<1ms) and robust process group signal handling via a
  compiled Go entrypoint binary.
- Support dual local and CI authentication modes (ADC directory mount or
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
        InputArtifactSlot["Channel 4: Input Artifact Slot (Future)<br>-v finding.json:/input/finding.json<br>or stdin pipe"]
        
        StdoutPipe["Channel A: stdout (Data Payload Stream)<br>Clean Structured JSON -> jq / CI Artifacts"]
        StderrPipe["Channel B: stderr (Diagnostics Stream)<br>Logs, Spinners, Traces -> CI Console"]
        ExitCodeSignal["Channel C: Exit Code (Verdict Signal)<br>0: Clean, 1: Findings, 2: CLI/Path/TTY Error"]
    end

    subgraph Container["Stateless Container Sandbox (USER: codemender / UID 1000)"]
        direction TB
        GoRunner["Go Entrypoint Runner<br>(/usr/local/bin/cm-runner)"]
        
        subgraph SubcommandSwitch["Subcommand & Path Dispatcher"]
            FindBranch["find Subcommand<br>resolves target path (. or subpath)<br>injects --format json"]
            ShellBranch["shell Subcommand<br>validates TTY (isatty)"]
            InvalidBranch["Missing / Unknown Subcommand / Bad Path<br>emit usage & exit 2"]
        end
        
        CM["CodeMender CLI<br>(/usr/local/bin/cm)"]
        BashShell["Interactive Shell<br>(/bin/bash)"]
        Preconfig["Pre-seeded Build-Time Config<br>(/home/codemender/.codemender)"]
    end

    subgraph Cloud["Google Cloud Backend"]
        Vertex["Vertex AI API<br>(aiplatform.googleapis.com)"]
    end

    %% Ingestion Flow
    WorkspaceMount -.->|Mount full repo to /workspace| Container
    AuthInjection -.->|Inject credentials| Container
    InputArtifactSlot -.->|Mount / pipe| Container
    CommandArgs -->|docker run args| GoRunner

    %% Dispatch Flow
    GoRunner -->|Parses & validates| SubcommandSwitch
    SubcommandSwitch -->|find [path] [flags]| FindBranch
    SubcommandSwitch -->|shell| ShellBranch
    SubcommandSwitch -->|empty / invalid / bad path| InvalidBranch

    FindBranch -->|Spawns /usr/local/bin/cm find <target_path> --format json| CM
    Preconfig -.->|Provides default DB/config| CM
    CM -->|Direct HTTPS (Port 443)| Vertex

    ShellBranch -->|TTY valid| BashShell
    ShellBranch -->|TTY missing| StderrPipe
    InvalidBranch --> StderrPipe

    %% Output Flow
    CM -->|stdout: Clean JSON findings| StdoutPipe
    CM -->|stderr: Diagnostic logs & progress| StderrPipe
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
     positional path (e.g. `docker run <image> find` or `find --model ...`),
     `cm-runner` targets `.`.
   - **Scoped Sub-Path Scan:** If the caller specifies a sub-path (e.g.
     `docker run <image> find src/auth` or `find ./pkg/api`), `cm-runner`
     validates that the directory/file exists inside `/workspace` and targets
     that path while keeping `/workspace` as the root context.
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
1. **Structured Ingestion Extension Slot (Future-Proofing for `verify`/`fix`):**
   - Designed to ingest finding artifacts via volume mount
     (`-v $(pwd)/findings.json:/input/findings.json:ro`) or standard input
     stream (`stdin`).

### 2.2 Output Channels (Container $\\rightarrow$ Host)

1. **Data Payload Stream (`stdout`):**
   - Exclusively reserved for structured machine-readable JSON payloads.
   - Guaranteed zero log contamination (no banner headers, progress text, or
     ANSI escape codes).
   - Pipeable directly to `jq`, artifact uploaders, or downstream CI steps.
1. **Diagnostics Stream (`stderr`):**
   - Captures all human-readable diagnostic messages, progress spinners, LLM
     reasoning telemetry, and error traces.
   - Redirectable to CI step logs (`2> run.log`).
1. **Verdict & Status Channel (Exit Codes):**
   - `0`: Scan completed cleanly with zero findings.
   - `1`: Scan completed with findings detected (actionable CI PR gating
     signal).
   - `2`: Invocation / CLI error (missing subcommand, non-existent target path,
     unrecognized flag, or `shell` invoked without pseudo-TTY).
   - `> 2`: Fatal tooling or authentication error.

______________________________________________________________________

## 3. Decisions & Rationale

### 3.1 Decision 1: Target Path Normalization & Full Repo Context

- **Choice:** `cm-runner` normalizes the scan target: defaults to `.` if no
  positional path is provided, forwards valid subpaths (e.g. `src/auth`), and
  checks path existence inside `/workspace` before invoking `cm`.
- **Rationale:** Code analysis tools perform significantly better when they have
  access to top-level dependency manifests and configuration files. By mounting
  the full repo at `/workspace` and passing sub-paths as arguments to `cm find`,
  we support targeted scanning without blinding CodeMender to root repo context.
- **Alternatives Considered:**
  - *Mounting only the sub-folder directly to `/workspace`:* Rejected because it
    breaks root dependency resolution and configuration lookup for mono-repos
    and multi-module packages.

### 3.2 Decision 2: Mandatory Subcommand & No Default Action

- **Choice:** `cm-runner` requires an explicit subcommand. Invoking
  `docker run <image>` without arguments immediately exits with code 2 and
  prints usage to `stderr`.
- **Rationale:** Default actions in container entrypoints mask invocation errors
  (e.g. typos or forgotten arguments) and create ambiguity in CI/CD pipeline
  definitions.
- **Alternatives Considered:**
  - *Default to `find` on empty args:* Rejected because implicit execution makes
    pipeline failures harder to debug and risks unexpected execution.

### 3.3 Decision 3: Explicit `shell` Subcommand with TTY Enforcement

- **Choice:** An explicit `shell` subcommand launches `/bin/bash`. If standard
  input is not a terminal (missing `-it`), `cm-runner` terminates immediately
  with exit code 2 and prints:
  `Error: 'shell' subcommand requires an interactive terminal. Please run with 'docker run -it <image> shell'`.
- **Rationale:** Running a non-interactive shell hangs batch scripts
  indefinitely waiting for EOF or input. Detecting TTY presence upfront prevents
  pipeline stalls.

### 3.4 Decision 4: Build-Time Configuration Pre-Initialization

- **Choice:** During Docker build, `cm init --dry-run || true` is executed under
  user `codemender` to generate default configuration and database structures in
  `/home/codemender/.codemender`.
- **Rationale:** Eliminates any prerequisite `cm init` step at runtime. Scans
  are completely stateless and work immediately upon container boot.

### 3.5 Decision 5: Compiled Go Entrypoint Runner (`cm-runner`)

- **Choice:** Implement `/usr/local/bin/cm-runner` in Go
  (`cmd/cm-runner/main.go`) using a multi-stage Docker build.
- **Rationale:** Sub-millisecond startup (~1ms), zero runtime interpreter memory
  overhead, static compilation, robust signal trapping (`os/signal` forwarding
  `SIGINT`/`SIGTERM` to child process groups via
  `syscall.Kill(-cmd.Process.Pid, sig)` with `Setpgid: true`), and native TTY
  detection (`golang.org/x/term` or `term.IsTerminal`).

______________________________________________________________________

## 4. Multi-Stage Dockerfile Architecture

```dockerfile
# Stage 1: Build Go runner binary
FROM golang:1.24-bookworm AS builder
WORKDIR /app
COPY cmd/cm-runner /app/cmd/cm-runner
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

## 5. Risks & Mitigations

| Risk | Mitigation | | :--- | :--- | | **Pipeline stalls when `shell` is
invoked in non-interactive CI** | `cm-runner` inspects terminal status and exits
immediately with code 2 if `isatty(0)` is false. | | **Exit code 1 causes CI
scripts with `set -e` to fail prematurely on findings** | Document clear pattern
in CI templates to capture exit code (e.g. `set +e` or `status=$?`). | | **User
specifies non-existent target path** | `cm-runner` validates path existence in
`/workspace` upfront and emits clear error on `stderr` with code 2. | | **Output
format flag collision when user specifies `-f sarif` or `--format text`** |
`cm-runner` checks for existing `--format` and `-f` flags before injecting
`--format json`. | | **Token expiration during long-running batch scans** |
Document use of short-lived tokens generated immediately prior to step execution
in CI. | | **Child toolchain orphaned processes on cancellation** | `cm-runner`
uses process group signaling (`Setpgid: true` with `syscall.Kill(-pid, sig)`) to
terminate child analyzer trees cleanly. |

______________________________________________________________________

## 6. Verification Strategy

1. **Mandatory Subcommand Test:** Run `docker run <image>` without arguments;
   assert immediate exit code 2 and usage instructions on `stderr`.
1. **Full Repo Scan Test:** Run `docker run -v $(pwd):/workspace <image> find`;
   verify `/usr/local/bin/cm find . --format json` is executed and clean JSON is
   on `stdout`.
1. **Scoped Sub-Path Test:** Run
   `docker run -v $(pwd):/workspace <image> find src/auth`; verify
   `/usr/local/bin/cm find src/auth --format json` is executed.
1. **Invalid Sub-Path Test:** Run
   `docker run -v $(pwd):/workspace <image> find non/existent/path`; assert
   immediate exit code 2 and path error on `stderr`.
1. **Missing TTY Test:** Run `docker run <image> shell` without `-it`; assert
   immediate exit code 2 and descriptive TTY error on `stderr`.
1. **Interactive TTY Test:** Run `docker run -it <image> shell`; assert
   interactive `/bin/bash` prompt in `/workspace` as user `codemender`.
1. **Build-Time Pre-Init Test:** Launch fresh container with only
   `-v $(pwd):/workspace`; execute `find` without `cm init` and assert scan
   succeeds.
1. **Signal Handling Test:** Send `SIGINT`/`SIGTERM` to running container;
   verify clean shutdown within 500ms.
1. **Exit Code Transparency Test:** Assert exit code `0` on clean fixtures, `1`
   on vulnerability detection, and `> 2` on auth/fatal errors.
1. **Multi-Mode Auth Test:** Verify Vertex AI execution with
   `CLOUDSDK_AUTH_ACCESS_TOKEN` injected without mounting `~/.config/gcloud`.
1. **Unprivileged User Test:** Execute `docker run <image> id -u && id -g` to
   verify strict UID/GID 1000 enforcement.
