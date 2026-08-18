# Proposal: CodeMender Headless Batch Scanner Container (`find`)

**Change ID:** `cm-batch-scanner` \
**Status:** Accepted \
**Author:** Charles LeDoux \
**Target Spec:** `openspec/specs/runner/cm-batch-runner/spec.md` \
**Governing ADR:** `adrs/ADR-0001.md`

## Why

Automated CI/CD pipelines (e.g., GitHub Actions PR gating, automated security
sweeps, batch triage) require a headless, deterministic, and completely
stateless scanner container for CodeMender (`cm`).

Unlike the interactive sandbox, this runner container is dedicated strictly to
the **`find`** lifecycle phase. It requires zero runtime initialization
(`cm init`), takes all necessary inputs (workspace codebase and auth
credentials) at startup, executes static and AI-assisted vulnerability
discovery, emits structured machine-readable JSON directly to `stdout`, directs
all diagnostic logs to `stderr`, and terminates with actionable exit codes.

Dynamic exploit verification (`verify`) and patch remediation (`fix`) are
explicitly scoped out of this proposal:

- `verify` requires repo-specific application runtime environments and will be
  addressed in a dedicated follow-up capability.
- `fix` requires a dedicated finding import mechanism to populate CodeMender's
  internal database prior to remediation and will also be handled in a separate
  spec.

## What Changes

- Scope the container execution protocol strictly to the **`find`**
  vulnerability scanner phase.
- Pre-initialize default CodeMender configuration during Docker image build time
  via `cm init`, applying minimal in-place headless configuration overrides
  (`.rs` file extension inclusion, `json` output format, disabled command/write
  confirmations) and fail-fast validation of critical keys via a reusable
  configuration mutator so that runtime `cm init` is completely unnecessary.
- Provide a completely stateless execution model where runs depend strictly on
  mounted codebase and authentication inputs.
- Create a statically-compiled Go entrypoint runner binary
  (`cmd/cm-runner/main.go` compiled to `/usr/local/bin/cm-runner`).
- Automate two-phase scan and reporting orchestration (`cm find` scan followed
  by `cm report --format=json`) on stdout.
- Ensure clean I/O separation (machine-readable findings JSON on stdout, logs
  and progress indicators on stderr).
- Enforce exact subcommand dispatch on `os.Args[1]` (`find`, `shell`, or `init`)
  with interactive TTY `/bin/bash` fallback.
- Strictly enforce unprivileged non-root userspace execution (user `codemender`,
  UID 1000, GID 1000).
- Support multi-mode Google Cloud authentication (OAuth access token env var,
  Workload Identity credential JSON, or mounted ADC directory).
- Propagate OS signals and faithful exit codes (`0` clean, `1` findings
  detected, `>1` error).

## Capabilities (The Core Contract)

### New Capabilities

- `cm-batch-runner`: Headless, stateless batch scanner container protocol for
  CodeMender `find` with build-time configuration pre-initialization, Go
  entrypoint runner, default machine-readable JSON output, clean I/O streams,
  and strict non-root userspace isolation. Maps to
  `openspec/specs/runner/cm-batch-runner/spec.md`.

### Modified Capabilities

- None.

## Impact

- Delivers a lean, generic vulnerability scanner container that works
  out-of-the-box across any repository without runtime setup steps (`cm init`).
- Provides a clean, machine-parseable finding stream (`jq`, SARIF export, GitHub
  Actions PR comments) with zero log pollution.
