---
archetype: capability
status: accepted
category: runner
name: cm-fix-runner
governing_adrs:
  - adrs/ADR-0001.md
  - adrs/ADR-0002.md
  - adrs/ADR-0003.md
  - adrs/ADR-0005.md
  - adrs/ADR-0008.md
  - adrs/ADR-0009.md
---

# CodeMender Stateless Fix Runner Container Specification (`fix`)

## Overview

The `cm-fix-runner` capability defines a stateless, deterministic, and isolated
Docker remediation container protocol for CodeMender (`cm`), implementing
[ADR-0001](../../../../adrs/ADR-0001.md),
[ADR-0002](../../../../adrs/ADR-0002.md),
[ADR-0003](../../../../adrs/ADR-0003.md), and
[ADR-0005](../../../../adrs/ADR-0005.md).

In production CI/CD pipelines (e.g. GitHub Actions PR comment bots, automated
remediation loops), vulnerability discovery (`find`) and remediation (`fix`) are
executed across decoupled containers. The `cm-fix-runner` protocol enables
automated patch generation starting from a **blank slate** without persistent
host state mounts (`~/.codemender`):

1. **Exact Subcommand Dispatch (`os.Args[1]`):** `cm-runner` requires an
   explicit subcommand (`fix`) as `os.Args[1]`. Redundant `cm` prefixes (e.g.
   `docker run <image> cm fix`) are rejected immediately with exit code 2.
1. **Single-Finding Granularity:** The runner processes exactly one
   vulnerability finding per container invocation, enabling trivial CI
   parallelization (e.g. GitHub Actions matrix jobs), bounded timeouts, and
   elimination of cross-finding patch conflicts.
1. **Flexible Finding Ingestion Channels:** Accepts finding JSON either from a
   mounted file path (`cm-runner fix /tmp/finding.json`) or standard input
   (`cm-runner fix -`).
1. **Closed-Loop Schema Normalization:** Automatically converts the PascalCase
   finding output of `cm report -f json` (`FilePath`, `StartLine`, `Title`,
   `Analysis`, `Severity`, `VulnType`, `Snippet`) into the target schema
   expected by `cm report import` (`file_path`, `line`, `title`, `message`,
   `severity`, `vuln_type`, `snippet`).
1. **Ephemeral State Seeding via `cm report import`:** Delegates database
   seeding to CodeMender's native `cm report import` command, populating the
   ephemeral SQLite database (`state.db`) while keeping `go.mod` 100%
   dependency-free.
1. **Instantaneous Finding ID Resolution:** Queries the single imported finding
   from ephemeral `state.db` via `cm report --format=json` to resolve the
   `FindingID` before dispatching `cm fix`.
1. **Verbatim Subprocess Flag Passthrough (`--`):** Forwards all unowned flags
   passed after `--` (e.g. `-c "Use parameterized query"`, `--architecture=3-1`,
   `--no-cache`) directly to `cm fix`.
1. **Ephemeral OverlayFS Workspace Isolation:** Operates over a read-only host
   workspace mount (`/workspace-ro:ro`) with an ephemeral container scratch
   layer (`fuse-overlayfs`), ensuring the host working copy remains 100%
   untouched while permitting unrestricted in-container file modifications
   during agent execution and test validation.
1. **Machine-Readable JSON Change Envelope (`stdout`):** Emits exclusively a
   structured JSON change envelope on `stdout` containing the unified diff,
   modified file lists, and remediation summary for downstream CI PR suggestion
   bots.
1. **Clean Stream Separation:** Directs all diagnostic logs, compiler outputs,
   and LLM interaction traces strictly to `stderr`.
1. **Remediation-Aware Exit Codes:** Terminates with exit code `0` when a patch
   is generated, `1` when remediation fails / no patch is produced, `2` on CLI
   usage or finding parsing errors, and `>2` on fatal tooling errors.

## Requirements

### REQ-0001: Subcommand Routing and Validation (`fix`)

`cm-runner` MUST require `os.Args[1] == "fix"` for triggering the remediation
pipeline. Redundant `cm` prefixes (e.g. `cm fix`) or empty subcommand
invocations MUST be rejected immediately with exit code 2 and a descriptive
error message on `stderr`.

#### Scenario: Route to fix handler on exact subcommand

- **GIVEN** a container invocation `cm-runner fix /tmp/finding.json`
- **WHEN** `os.Args[1]` equals `"fix"`
- **THEN** `cm-runner` MUST dispatch the remediation execution pipeline.

#### Scenario: Reject redundant cm prefix

- **GIVEN** a container invocation `cm-runner cm fix /tmp/finding.json`
- **WHEN** the command executes
- **THEN** `cm-runner` MUST exit with code 2 and emit
  `Error: unrecognized subcommand 'cm'` to `stderr`.

______________________________________________________________________

### REQ-0002: Positional Finding Input Channels (File and Stdin)

`cm-runner fix` MUST accept the target finding input as the first positional
argument before the `--` delimiter:

1. **File Path:** If given a file path (e.g. `cm-runner fix /tmp/finding.json`
   or `cm-runner fix relative/finding.json`), `cm-runner` MUST validate that the
   file exists and read its contents.
1. **Stdin Identifier (`-`):** If given `-` (e.g. `cm-runner fix -`),
   `cm-runner` MUST read the finding JSON payload from `os.Stdin`.
1. **Missing Argument Error:** If `fix` is invoked without a target argument or
   with an empty string, `cm-runner` MUST terminate immediately with exit code 2
   and print usage instructions to `stderr`.

#### Scenario: Read finding from mounted file path

- **GIVEN** a valid finding JSON file mounted at `/tmp/finding.json`
- **WHEN** executing `cm-runner fix /tmp/finding.json`
- **THEN** `cm-runner` MUST read and parse the file contents from
  `/tmp/finding.json`.

#### Scenario: Read finding from stdin

- **GIVEN** a finding JSON payload piped via standard input
- **WHEN** executing `cm-runner fix -`
- **THEN** `cm-runner` MUST read the complete payload from `stdin`.

#### Scenario: Error on missing target argument

- **GIVEN** an invocation `cm-runner fix` without arguments
- **WHEN** the command executes
- **THEN** `cm-runner` MUST exit with code 2 and emit usage instructions to
  `stderr`.

#### Scenario: Error on non-existent file path

- **GIVEN** an invocation `cm-runner fix /non/existent/finding.json`
- **WHEN** the file does not exist
- **THEN** `cm-runner` MUST exit with code 2 and emit a file not found error to
  `stderr`.

______________________________________________________________________

### REQ-0003: Single-Finding Closed-Loop Schema Normalization

`cm-runner` MUST normalize the input finding JSON into the schema required by
`cm report import`:

1. **Input Schema Compatibility:** MUST accept finding payloads formatted by
   `cm report -f json` (single JSON object or 1-element array) with PascalCase
   fields (`FilePath`, `StartLine`, `Title`, `Analysis`, `Severity`, `VulnType`,
   `Snippet`, `Status`) or standard camelCase/snake_case variants.
1. **Target Import Schema:** MUST serialize the finding to a temporary JSON file
   (`/tmp/cm-import.json`) matching the array-of-objects structure expected by
   `cm report import`:
   - `file_path`: Path to target file (stripping any `file://` URI prefix).
   - `line`: Integer line number $\\ge 1$ (defaulting to `1` if omitted or `0`).
   - `title`: Headline summary (defaulting to `"Security Finding"`).
   - `message`: Finding analysis/remediation advice (falling back to `Snippet`
     or `Title` if empty).
   - `severity`: Uppercase severity string (`CRITICAL`, `HIGH`, `MEDIUM`,
     `LOW`).
   - `vuln_type`: Vulnerability classification identifier.
   - `snippet`: Source code snippet.
   - `status`: Lifecycle state (defaulting to `"OPEN"`).
1. **Workspace Boundary Validation:** MUST validate that `file_path` resolves to
   an existing file within `/workspace`. If the target file does not exist,
   `cm-runner` MUST emit an error to `stderr` and exit with code 2.

#### Scenario: Convert PascalCase cm find JSON to import schema

- **GIVEN** an input JSON finding with `FilePath`, `StartLine`, `Title`, and
  `Analysis`
- **WHEN** `cm-runner` parses the payload
- **THEN** it MUST write a normalized JSON array to `/tmp/cm-import.json`
  containing `file_path`, `line`, `title`, and `message`.

#### Scenario: Normalize 1-element finding array

- **GIVEN** an input JSON containing an array with a single finding object
- **WHEN** `cm-runner` parses the payload
- **THEN** it MUST extract the single finding and normalize it successfully.

#### Scenario: Error on invalid JSON syntax

- **GIVEN** an unparseable or corrupted JSON input
- **WHEN** `cm-runner` parses the payload
- **THEN** `cm-runner` MUST exit with code 2 and emit a JSON parse error to
  `stderr`.

______________________________________________________________________

### REQ-0004: Ephemeral State Seeding via Native `cm report import`

`cm-runner` MUST execute `/usr/local/bin/cm report import` to seed the
normalized finding into the ephemeral SQLite database (`state.db`):

1. **Subprocess Ingestion:** Executes
   `/usr/local/bin/cm report import -f /tmp/cm-import.json -p /workspace`.
1. **Stream Routing:** Directs all import progress output and logs to `stderr`.
1. **Import Failure Handling:** If `cm report import` fails (exit code $\\ne
   0$), `cm-runner` MUST terminate immediately with exit code $>2$ and propagate
   the error details on `stderr`.

#### Scenario: Successfully seed ephemeral database

- **GIVEN** a normalized `/tmp/cm-import.json` file
- **WHEN** `cm-runner` executes the import step
- **THEN** `cm report import` MUST exit with status 0 and populate `state.db`.

#### Scenario: Import failure aborts pipeline

- **GIVEN** an invalid schema causing `cm report import` to fail
- **WHEN** `cm-runner` executes the import step
- **THEN** `cm-runner` MUST abort the pipeline, skip fix execution, and exit
  with status $>2$.

______________________________________________________________________

### REQ-0005: Finding ID Resolution and Automated `cm fix` Execution

Upon successful database seeding, `cm-runner` MUST:

1. **Resolve Target ID:** Query `/usr/local/bin/cm report --format=json` to
   retrieve the single imported finding object and extract its `FindingID`.
1. **Execute Fix Agent:** Execute
   `/usr/local/bin/cm fix <FindingID> -y --unrestricted [passthrough_flags...]`
   against the codebase.
1. **Headless Defaults:** Automatically inject `-y` (skip confirmation prompts)
   and `--unrestricted` (allow agent tool modifications in the CoW layer).
1. **Diagnostic Logging:** Stream all fix agent progress spinners, LLM reasoning
   telemetry, compiler logs, and test outputs strictly to `stderr`.

#### Scenario: Resolve finding ID and execute fix

- **GIVEN** a successfully imported finding in `state.db`
- **WHEN** `cm-runner` queries `cm report --format=json`
- **THEN** it MUST extract the generated `FindingID` (e.g. `uuid-1234`)
- **AND** execute `cm fix uuid-1234 -y --unrestricted`.

______________________________________________________________________

### REQ-0006: Verbatim Subprocess Flag Forwarding via `--` Partitioning

All arguments specified after the POSIX `--` delimiter MUST be partitioned from
the target path and forwarded verbatim to `cm fix`:

1. **Flag Integrity:** Flags passed after `--` (e.g.
   `-c "Use parameterized queries"`, `--architecture=3-1`, `--no-cache`,
   `--model=gemini-1.5-pro`) MUST NOT be inspected, parsed, or modified by
   `cm-runner`.
1. **Flag Passthrough Order:** Arguments MUST be appended to the `cm fix`
   execution vector in their exact original order.

#### Scenario: Forward unowned flags to cm fix

- **GIVEN** an invocation
  `cm-runner fix /tmp/finding.json -- -c "Sanitize input" --architecture=3-1`
- **WHEN** `cm-runner` prepares the fix command
- **THEN** `cm fix` MUST receive
  `["<FindingID>", "-y", "--unrestricted", "-c", "Sanitize input", "--architecture=3-1"]`.

______________________________________________________________________

### REQ-0007: Ephemeral OverlayFS Isolation and Host Immutability

When executing in containerized environments supporting OverlayFS
(`--device /dev/fuse`):

1. **Host Protection:** The host repository MUST be mounted read-only
   (`/workspace-ro:ro`) and remain 100% immutable throughout execution.
1. **In-Container Read/Write Semantics:** Code edits, temporary test fixtures,
   and build outputs MUST accumulate exclusively in the ephemeral CoW upper
   layer (`/tmp/overlay/upper`).
1. **Clean Teardown:** Upon container exit, all ephemeral scratch layers MUST be
   discarded without host filesystem residue.

#### Scenario: Host files remain unchanged after remediation

- **GIVEN** a host workspace mounted read-only
- **WHEN** `cm-runner fix` executes and modifies source files in `/workspace`
- **THEN** all modifications MUST remain in the container CoW layer
- **AND** host source files MUST retain their exact initial checksums.

______________________________________________________________________

### REQ-0008: Structured JSON Output Envelope on `stdout` and Stream Isolation

Upon completion of `cm fix`, `cm-runner` MUST:

1. **Extract Unified Patch with Pathspec Exclusions:** Execute `git diff HEAD`
   (or directory diff fallback) inside `/workspace` applying magic pathspec
   exclusions (`:!.cm_project`, `:!.codemender`, `:!.codemender-out`,
   `:!change_envelope.json`, `:!*creds*.json`, `:!*.diff`, `:!*.tmp`) during
   `git add -N` and `git diff HEAD`, executing deferred `git reset HEAD -- .`
   cleanup, and passing matching `-x` exclusions during fallback directory
   diffing, guaranteeing runtime state, credentials, and output artifacts do not
   pollute remediation patches.
1. **Synthesize JSON Envelope:** Construct and emit a valid JSON change envelope
   exclusively to `stdout` matching the schema:
   - `finding_id`: The ID of the targeted finding.
   - `status`: `"FIXED"` if a non-empty patch was generated, or `"UNRESOLVED"`
     if no patch was produced.
   - `vuln_type`: Vulnerability classification.
   - `title`: Finding headline.
   - `summary`: Remediation summary / commit message.
   - `files_modified`: Array of modified relative file paths.
   - `patch`: Complete unified diff string.
   - `hunks`: Array of per-file structured replacement blocks (`file_path`,
     `start_line`, `end_line`, `original`, `replacement`).
1. **Stream Isolation:** `stdout` MUST contain pure JSON with zero ANSI escape
   codes, banner headers, or diagnostic logs.

#### Scenario: Emit valid JSON change envelope on stdout

- **GIVEN** a successful remediation generating code changes
- **WHEN** `cm-runner fix` completes
- **THEN** `stdout` MUST be parseable by `jq .`
- **AND** `status` MUST equal `"FIXED"`
- **AND** `patch` MUST contain a valid Git unified diff.

#### Scenario: Exclude runtime state, credentials, and diff artifacts from generated patch

- **GIVEN** an ephemeral workspace containing `.codemender`, `.codemender-out`,
  `change_envelope.json`, or credential files
- **WHEN** `cm-runner fix` extracts the patch
- **THEN** the synthesized patch and `files_modified` list MUST NOT include any
  of the excluded artifacts.

______________________________________________________________________

### REQ-0009: Stream Isolation & Diagnostic Log Routing (`stderr`)

All operational messages, progress indicators, LLM interaction notices,
subprocess outputs, and error traces from all pipeline stages
(`cm report import`, `cm report`, `cm fix`) MUST be routed strictly to `stderr`.

#### Scenario: Verify stderr captures diagnostic progress

- **GIVEN** a fix execution redirected to separate output files
  `cm-runner fix /tmp/finding.json > patch.json 2> run.log`
- **WHEN** the command completes
- **THEN** `patch.json` MUST contain only valid JSON
- **AND** `run.log` MUST contain the progress banners and compiler logs.

______________________________________________________________________

### REQ-0010: Remediation-Aware Exit Code Evaluation

`cm-runner` MUST evaluate the remediation outcome to return deterministic exit
codes:

- `0`: Remediation succeeded, non-empty patch generated and validated.
- `1`: Remediation attempted, but LLM agent failed to generate a patch or build
  verification failed (`status == "UNRESOLVED"`).
- `2`: CLI usage error, non-existent target path, or malformed finding JSON.
- `>2`: Tooling execution failure, authentication error, or fatal subprocess
  crash.

#### Scenario: Exit code 0 on successful patch generation

- **GIVEN** a finding remediated with valid code changes
- **WHEN** `cm-runner fix` completes
- **THEN** the process MUST exit with code 0.

#### Scenario: Exit code 1 when fix agent fails to produce patch

- **GIVEN** a complex finding where `cm fix` fails to generate a patch
- **WHEN** `cm-runner fix` completes
- **THEN** `cm-runner` MUST emit an `"UNRESOLVED"` JSON envelope to `stdout`
- **AND** terminate with exit code 1.

#### Scenario: Exit code 2 on invalid JSON input

- **GIVEN** a malformed finding JSON file
- **WHEN** executing `cm-runner fix`
- **THEN** the process MUST exit with code 2.

______________________________________________________________________

### REQ-0011: Strict Unprivileged Userspace Execution

The container MUST execute strictly as the unprivileged non-root user
`codemender` (UID 1000, GID 1000). The container MUST NOT run processes as
`root`.

#### Scenario: Verify non-root execution identity

- **GIVEN** a running container instance
- **WHEN** checking process credentials
- **THEN** UID and GID MUST both equal 1000.

______________________________________________________________________

### REQ-0012: In-Band Token Telemetry and Diagnostic Banner (`fix`)

When executing `fix`, `cm-runner` MUST capture LLM token telemetry and enrich the
`ChangeEnvelope` emitted on `stdout`:

1. **Enriched ChangeEnvelope Payload (`stdout`):** The output on `stdout` MUST
   include a `"tokens"` object matching the `TokenMetrics` schema containing
   `input_tokens`, `output_tokens`, `cached_tokens`, `thought_tokens`,
   `total_tokens`, `cache_hit_percent`, `think_ratio_percent`,
   `duration_seconds`, and `duration_formatted`.
2. **Precision Stats Resolution:** Upon completion of `cm fix`, `cm-runner` MUST
   extract the active session ID (from `stderr` session log notification or
   `~/.codemender/state.db`) and query `/usr/local/bin/cm stats --session <id> --json`.
3. **Diagnostic Console Banner (`stderr`):** `cm-runner` MUST format and emit a
   human-readable token summary box to `stderr` displaying token counts with
   metric notation (e.g. `12.5k`, `8.2k`) and prompt cache efficiency.

#### Scenario: Emit ChangeEnvelope with in-band token metrics on stdout

- **GIVEN** a valid finding remediated via `cm-runner fix /tmp/finding.json`
- **WHEN** remediation generates code modifications
- **THEN** `stdout` MUST parse as a JSON object containing `"status": "FIXED"`
  and a populated `"tokens"` object
- **AND** a human-readable telemetry banner MUST be printed to `stderr`.

