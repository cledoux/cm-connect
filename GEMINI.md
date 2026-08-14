# cm-connect Project Instructions & Context

`cm-connect` provides an unprivileged, userspace Docker container environment
for running the CodeMender (`cm`) CLI against codebases with state persistence,
sub-millisecond static Go dispatch, and Google Cloud ADC authentication.

## Architecture & Structure

- `pkg/cmrunner/`: Reusable Go execution engine specialized for CodeMender:
  - `command.go`: `FindCommand` encapsulating target paths, flags, format
    checks, and automatic `--format json` injection on stdout.
  - `runner.go`: `Runner` with process group isolation (`Setpgid: true`), signal
    forwarding (`SIGINT`/`SIGTERM` to `-cmd.Process.Pid`), and exit code
    propagation (0 clean, 1 findings, >2 fatal).
- `cmd/cm-runner/`: Statically compiled Go entrypoint binary (`main.go`,
  `dispatch.go`):
  - Subcommand parsing, whitespace trimming, sentinel error handling
    (`errMissingSubcommand`, `errInvalidSubcommand`, `errPathNotFound`,
    `errPathTraversal`), and standard `--` flag delimiter support.
- `docker/Dockerfile`: Multi-stage build (`golang:1.24-bookworm` builder ->
  `debian:bookworm-slim` runtime):
  - Pre-seeded configuration via `RUN cm init` under unprivileged `codemender`
    user (UID/GID 1000).
  - Headless environment defaults (`ENV NO_COLOR=1`, `ENV TERM=dumb`).
- `bin/cm-runner`: Executable host runner script that mounts the workspace
  (`$(pwd):/workspace`) and forwards Application Default Credentials (ADC).
- `Makefile`: Build and test automation:
  - `make fmt`: Expands `goall` workflow (`goimports -w` and `gofmt -s -w`).
  - `make lint`: Enforces formatting and runs `go vet ./...`.
  - `make test`: Runs unit tests with race detector, statement coverage, and
    `-timeout 60s`.
  - `make build`: Enforces `lint` before building Docker image.
  - `make integration-test`: Runs 10-scenario container verification test suite
    under a 60s timeout envelope.
- `tests/integration_test.sh`: Container verification test suite with defensive
  `run_with_timeout 15s` execution envelopes on every test scenario.
- `adrs/ADR-0001.md`: Architectural Decision Record for the headless batch
  runner container protocol.
- `openspec/specs/runner/cm-batch-runner/`: OpenSpec capability specifications
  (`spec.md`, `design.md`).

## Development Rules

- **Markdown Formatting**: Always run `mdformat --wrap 80 <file.md>` after
  creating or editing Markdown files.
- **Commits**: Use the `/commit` workflow. Every commit must be a single logical
  unit of work (containing as much as it needs and no more) with explicit Why
  and What sections.
- **Pre-Commit Safety**: Check for loose threads before committing, ask if any
  remain, and never touch foreign working copy changes without permission.
- **Task Tracking**: Track all backlog items and tasks exclusively through
  GitHub Issues (do not check in local `tasks.md` files).
- **Defensive Execution**: Enforce strict outer timeouts (`timeout <limit> ...`
  or `run_with_timeout`) on all test scripts, subprocesses, and commands.

## Project Roadmap & Next Steps

1. **Phase 2: Finding Ingestion & Fix Runner**:
   - Design finding import/export mechanism.
   - Build stateless `fix` remediation container protocol.
1. **Phase 3: Verify Runner & App Execution Environments**:
   - Design and implement `verify` execution container tailored for app runtime
     isolation and build environments.
1. **Phase 4: GitHub PR Gating Workflow**:
   - Implement GitHub Actions workflows in `.github/workflows/` using the
     Two-Workflow pattern (`pull_request` scanner -> artifact upload ->
     `workflow_run` gating job).
   - Automate parsing `cm report -f json` to post inline GitHub suggestion
     blocks (`suggestion ... `) on PR review threads.
