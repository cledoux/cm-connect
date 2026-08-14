______________________________________________________________________

## archetype: capability status: accepted category: runner name: cm-batch-runner governing_proposal: cm-batch-runner governing_adr: adrs/ADR-0001.md

# CodeMender Headless Batch Scanner Container Specification (`find`)

## Overview

The `cm-batch-runner` capability defines a headless, deterministic, and
completely stateless Docker scanner container protocol for CodeMender (`cm`),
implementing [ADR-0001](../../../../adrs/ADR-0001.md).

The container provides a structured **Host $\\leftrightarrow$ Container
Communication Protocol** dedicated to the **`find`** vulnerability discovery
lifecycle phase, with built-in architectural extension points for future
lifecycle commands (`verify`, `fix`, `report`):

1. **Workspace & Scan Target Distinction:**
   - **Repository Root (`/workspace`):** The entire repository is mounted to
     `/workspace` (`WORKDIR`) so CodeMender retains complete repository-wide
     context (build files, dependencies, configurations).
   - **Scan Target Scope:** Supports scanning the entire repository by default
     (`.`), or scoping analysis strictly to a sub-tree/module (e.g.
     `./src/auth`).
1. **Build-Time Pre-Initialization:** Pre-seeds default configuration at image
   build time, completely eliminating the need for runtime `cm init`.
1. **Execution & Subcommand Requirement:** Requires an explicit subcommand
   (`find`, `shell`), rejecting empty invocations.
1. **Outputs:** Emits structured machine-readable findings (JSON) strictly on
   `stdout`, directs all diagnostic logs and progress banners to `stderr`, and
   propagates actionable exit codes.
1. **Interactive Support:** Supports an explicit `shell` subcommand that
   validates pseudo-TTY presence and drops the user into `/bin/bash` in
   `/workspace`.
1. **Userspace Security:** Executes strictly as an unprivileged userspace user
   (`codemender`, UID 1000).

## Requirements

### REQ-0001: Go Entrypoint Runner Binary

The container image MUST compile and install a dedicated Go entrypoint binary
(`/usr/local/bin/cm-runner`) configured as the default container `ENTRYPOINT`.
The runner MUST execute with sub-millisecond startup latency, handle subcommand
validation, translate default output formats, resolve target paths, and manage
child process groups.

#### Scenario: Verify Go runner binary existence and execution

- **GIVEN** a built container image
- **WHEN** inspecting the container entrypoint
- **THEN** `/usr/local/bin/cm-runner` MUST exist, be executable, and be
  configured as the container `ENTRYPOINT`.

______________________________________________________________________

### REQ-0002: Build-Time Configuration Pre-Initialization (Zero Runtime `cm init`)

The container image MUST pre-initialize and populate default CodeMender
configuration structures during container build time under
`/home/codemender/.codemender`, ensuring that runtime execution of `find`
requires NO preceding `cm init` step.

#### Scenario: Execute scan without runtime initialization

- **GIVEN** a freshly launched container with only `/workspace` mounted
- **WHEN** executing `docker run --rm -v $(pwd):/workspace <image> find`
- **THEN** CodeMender MUST execute the scan immediately without erroring due to
  uninitialized configuration or missing database structures.

______________________________________________________________________

### REQ-0003: Mandatory Subcommand Requirement (No Default Action)

`cm-runner` MUST require an explicit subcommand at invocation. If the container
is launched without arguments, or with an unrecognized subcommand, `cm-runner`
MUST NOT execute a default command; it MUST print a helpful usage guide to
`stderr` and terminate immediately with exit code 2.

#### Scenario: Error on invocation without arguments

- **GIVEN** a container invocation with no CLI arguments (`docker run <image>`)
- **WHEN** the container starts
- **THEN** `cm-runner` MUST exit with status code 2 and output usage
  instructions to `stderr`.

#### Scenario: Error on unrecognized subcommand

- **GIVEN** a container invocation with an invalid subcommand
  (`docker run <image> invalid-cmd`)
- **WHEN** the container starts
- **THEN** `cm-runner` MUST exit with status code 2 and output an error message
  to `stderr`.

______________________________________________________________________

### REQ-0004: Target Scan Path Resolution (Full Repo vs Sub-Path)

`cm-runner` MUST distinguish between the mounted repository root (`/workspace`)
and the target scan path argument:

1. **Full Repository Scan (Default):** If `find` is invoked with no positional
   path argument, `cm-runner` MUST automatically supply `.` as the scan target,
   scanning the entire mounted codebase in `/workspace`.
1. **Scoped Sub-Path Scan:** If `find` is invoked with a specific relative or
   absolute sub-path (e.g. `find ./src/auth` or `find pkg/api`), `cm-runner`
   MUST validate that the path exists within `/workspace` and scope the scan to
   that specific directory/file while maintaining the full repository context in
   `/workspace`.
1. **Invalid Path Error:** If the requested sub-path does not exist within
   `/workspace`, `cm-runner` MUST terminate immediately with exit code 2 and
   emit a descriptive path error to `stderr`.

#### Scenario: Default full repository scan

- **GIVEN** a container invocation
  `docker run --rm -v $(pwd):/workspace <image> find`
- **WHEN** no positional target path is provided
- **THEN** `cm-runner` MUST invoke `/usr/local/bin/cm find . --format json`.

#### Scenario: Scoped sub-path scan

- **GIVEN** a container invocation
  `docker run --rm -v $(pwd):/workspace <image> find src/auth`
- **WHEN** `src/auth` exists inside `/workspace`
- **THEN** `cm-runner` MUST invoke
  `/usr/local/bin/cm find src/auth --format json` with `/workspace` remaining
  the current working directory.

#### Scenario: Error on non-existent sub-path

- **GIVEN** a container invocation
  `docker run --rm -v $(pwd):/workspace <image> find non/existent/path`
- **WHEN** `non/existent/path` does not exist in `/workspace`
- **THEN** `cm-runner` MUST exit with status code 2 and output an error to
  `stderr`:
  `Error: scan target path 'non/existent/path' does not exist in /workspace`.

______________________________________________________________________

### REQ-0005: Structured Machine-Readable Output by Default

Batch analysis commands (such as `find`) MUST produce structured,
machine-readable data payloads (JSON) on `stdout` by default. `cm-runner` MUST
automatically inject `--format json` into the forwarded argument list unless an
explicit format flag (`--format` or `-f`) is provided by the caller.

#### Scenario: Default structured JSON output on find

- **GIVEN** a running container instance
- **WHEN** executing `find` with no format flags
- **THEN** `cm-runner` MUST invoke `/usr/local/bin/cm find . --format json` and
  emit structured JSON on `stdout`.

#### Scenario: Respect explicit format override

- **GIVEN** a running container instance
- **WHEN** executing `find . --format text`
- **THEN** `cm-runner` MUST invoke `/usr/local/bin/cm find . --format text`
  without injecting `--format json`.

#### Scenario: Respect short format flag override

- **GIVEN** a running container instance
- **WHEN** executing `find . -f sarif`
- **THEN** `cm-runner` MUST invoke `/usr/local/bin/cm find . -f sarif` without
  injecting `--format json`.

______________________________________________________________________

### REQ-0006: Host $\\leftrightarrow$ Container Communication Protocol & Stream Separation

The container protocol MUST define strict input and output channels that isolate
data payloads from operational diagnostics:

1. **Input Channels (Host $\\rightarrow$ Container):**
   - **Workspace Channel (Repository Context):** `-v <host_dir>:/workspace`
     provides the complete repository tree (`WORKDIR /workspace`).
   - **Authentication Channel:** Injected via `CLOUDSDK_AUTH_ACCESS_TOKEN` env
     var, `GOOGLE_APPLICATION_CREDENTIALS` config file, or mounted ADC directory
     (`/home/codemender/.config/gcloud:ro`).
   - **Command & Target Channel:** Explicit CLI subcommand and optional sub-path
     targets (`find [path] [flags]`, `shell`).
   - **Structured Ingestion Extension Slot:** Prepares support for future
     subcommands via `-v <host_file>:/input/<file>:ro` or `stdin` stream pipes.
1. **Output Channels (Container $\\rightarrow$ Host):**
   - **Data Payload Stream (`stdout`):** Exclusively reserved for structured
     machine-readable payloads (JSON findings list). Guaranteed to contain zero
     ANSI escape codes, banner headers, or log lines.
   - **Diagnostics Stream (`stderr`):** Exclusively captures operational logs,
     scanning progress indicators, LLM interaction notices, and error traces.
   - **Status Signal (Exit Codes):** Communicates execution verdicts via exit
     codes.

#### Scenario: Verify stdout parseability with jq and stream separation

- **GIVEN** a codebase mounted at `/workspace`
- **WHEN** executing
  `docker run --rm -v $(pwd):/workspace <image> find > findings.json 2> runner.log`
- **THEN** `findings.json` MUST be directly parseable by `jq .` with zero
  parsing errors.
- **AND** `runner.log` MUST contain runtime progress logs and diagnostic traces.

______________________________________________________________________

### REQ-0007: Subcommand Normalization (`cm` Prefix Stripping)

`cm-runner` MUST accept both direct subcommands (e.g. `docker run <image> find`)
and prefixed command invocations (e.g. `docker run <image> cm find`),
automatically stripping any redundant leading `cm` token.

#### Scenario: Invoke with cm prefix

- **GIVEN** a container execution
- **WHEN** passing arguments `cm find src/auth`
- **THEN** `cm-runner` MUST normalize the arguments and execute
  `/usr/local/bin/cm find src/auth --format json`.

#### Scenario: Invoke without cm prefix

- **GIVEN** a container execution
- **WHEN** passing arguments `find src/auth`
- **THEN** `cm-runner` MUST execute
  `/usr/local/bin/cm find src/auth --format json`.

______________________________________________________________________

### REQ-0008: Explicit `shell` Subcommand & TTY Enforcement

The container MUST support an explicit `shell` subcommand to drop into an
interactive shell for debugging and ad-hoc inspection:

1. **TTY Detection:** `cm-runner` MUST check whether standard input (`stdin`) is
   attached to an interactive terminal (pseudo-TTY).
1. **Missing TTY Error:** If `shell` is invoked without a pseudo-TTY (e.g.,
   missing `-it`), `cm-runner` MUST exit immediately with status code 2 and
   output a helpful error message to `stderr`:
   `Error: 'shell' subcommand requires an interactive terminal. Please run with 'docker run -it <image> shell'`.
1. **Interactive Shell Launch:** If a valid pseudo-TTY is present, `cm-runner`
   MUST launch `/bin/bash` in `/workspace` as user `codemender`.

#### Scenario: Error when shell invoked without TTY flags

- **GIVEN** a non-interactive execution (`docker run <image> shell` without
  `-it`)
- **WHEN** the container starts
- **THEN** `cm-runner` MUST detect missing TTY, exit with code 2, and emit
  `Error: 'shell' subcommand requires an interactive terminal. Please run with 'docker run -it <image> shell'`
  to `stderr`.

#### Scenario: Drop into interactive shell when TTY attached

- **GIVEN** an interactive execution (`docker run -it <image> shell`)
- **WHEN** the container starts
- **THEN** `cm-runner` MUST present an interactive `/bin/bash` prompt in
  `/workspace` running as `codemender`.

______________________________________________________________________

### REQ-0009: Headless Non-Interactive Environment Defaults

When executing batch subcommands, `cm-runner` MUST configure:

1. `NO_COLOR=1` and `TERM=dumb` in the child process environment to prevent ANSI
   escape code pollution in redirected output streams.
1. Standard CodeMender telemetry enabled by default (no opt-out override).

#### Scenario: Validate clean environment in batch mode

- **GIVEN** a batch container invocation
- **WHEN** running `find`
- **THEN** child processes MUST inherit `NO_COLOR=1` and `TERM=dumb`.

______________________________________________________________________

### REQ-0010: Strict Unprivileged Userspace Execution

The container MUST execute strictly as the unprivileged, non-root user
`codemender` (UID 1000, GID 1000). The container MUST NOT run processes or
commands as `root`. All home directory structures MUST be rooted under
`/home/codemender`.

#### Scenario: Verify unprivileged user identity

- **GIVEN** a running container instance
- **WHEN** executing `id -u` and `id -g`
- **THEN** both UID and GID MUST equal 1000.

______________________________________________________________________

### REQ-0011: Multi-Mode Authentication Support

The container MUST support Google Cloud authentication through any of the
following three mechanisms:

1. Short-lived OAuth/OIDC access token via `CLOUDSDK_AUTH_ACCESS_TOKEN`.
1. Workload Identity / Service Account configuration via
   `GOOGLE_APPLICATION_CREDENTIALS` (with mounted credential file).
1. Mounted host Application Default Credentials (ADC) directory at
   `/home/codemender/.config/gcloud:ro`.

#### Scenario: Authenticate via access token

- **GIVEN** `CLOUDSDK_AUTH_ACCESS_TOKEN` passed in environment
- **WHEN** running `find`
- **THEN** CodeMender MUST authenticate to Vertex AI without requiring a mounted
  `.config/gcloud` directory.

#### Scenario: Authenticate via Workload Identity credential file

- **GIVEN** `GOOGLE_APPLICATION_CREDENTIALS=/creds/key.json` and host credential
  file mounted
- **WHEN** running `find`
- **THEN** CodeMender MUST authenticate to Vertex AI successfully.

#### Scenario: Authenticate via mounted ADC directory

- **GIVEN** host `~/.config/gcloud` mounted to
  `/home/codemender/.config/gcloud:ro`
- **WHEN** running `find`
- **THEN** CodeMender MUST authenticate to Vertex AI successfully.

______________________________________________________________________

### REQ-0012: Signal Forwarding & Standardized Exit Code Protocol

1. `cm-runner` MUST trap OS signals (`SIGINT`, `SIGTERM`) and propagate them to
   the child process group.
1. The container MUST terminate with standardized exit codes:
   - `0`: Scan completed successfully, no policy violations / vulnerabilities
     detected.
   - `1`: Scan completed, vulnerabilities detected (actionable CI gating
     signal).
   - `2`: CLI usage / invocation error (missing subcommand, non-existent target
     path, unrecognized flag, or missing TTY on `shell`).
   - `> 2`: Fatal tooling or authentication error.

#### Scenario: Propagate exit code 1 on vulnerability findings

- **GIVEN** a codebase containing detected vulnerabilities
- **WHEN** running `find`
- **THEN** the container process MUST exit with code 1 and emit findings JSON on
  `stdout`.

#### Scenario: Propagate exit code 0 on clean codebase

- **GIVEN** a clean codebase without vulnerabilities
- **WHEN** running `find`
- **THEN** the container process MUST exit with code 0.

#### Scenario: Propagate exit code 2 on missing subcommand

- **GIVEN** an invocation with no arguments
- **WHEN** executing `cm-runner`
- **THEN** the container process MUST exit with code 2.

#### Scenario: Handle graceful shutdown on SIGTERM

- **GIVEN** a running scan inside the container
- **WHEN** the container receives a `SIGTERM` signal
- **THEN** `cm-runner` MUST forward the signal to child process groups and exit
  cleanly within 1 second.
