# Tasks: CodeMender Headless Batch Scanner Container (`cm-batch-runner`)

**Governing Spec:** `openspec/specs/runner/cm-batch-runner/spec.md` \
**Governing Design:** `openspec/specs/runner/cm-batch-runner/design.md` \
**Governing ADR:** `adrs/ADR-0001.md` \
**GitHub Epic:**
[Issue #1](https://github.com/ledoux-google/cm-connect/issues/1)

______________________________________________________________________

## Batch 1: Go Entrypoint Runner (`cm-runner`)

### Track 1: CLI Core & Subcommand Routing

- [x] **Task 1.1 (#2):** Initialize Go Module & CLI Foundation (`REQ-0001`,
  `ADR-0001`)
- [x] **Task 1.2 (#3):** Implement Subcommand Dispatch & Target Path Resolution
  (`REQ-0003`, `REQ-0004`, `REQ-0007`)
- [x] **Task 1.3 (#4):** Implement Default Structured JSON Formatting
  (`REQ-0005`, `REQ-0006`, `REQ-0009`)

### Track 2: Interactive Shell & Process Management

- [x] **Task 1.4 (#5):** Implement Interactive Shell & TTY Enforcement
  (`REQ-0008`)
- [x] **Task 1.5 (#6):** Implement Process Execution & Signal Propagation
  (`REQ-0001`, `REQ-0006`, `REQ-0012`)

______________________________________________________________________

## Batch 2: Multi-Stage Dockerfile & Build Automation

### Track 1: Container Build & Automation

- [x] **Task 2.1 (#7):** Multi-Stage Dockerfile with Build-Time Pre-Init
  (`REQ-0001`, `REQ-0002`, `REQ-0010`, `REQ-0011`)
- [x] **Task 2.2 (#8):** Makefile Build Targets & Host Runner Script
  (`REQ-0001`, `REQ-0006`, `REQ-0011`)

______________________________________________________________________

## Batch 3: Integration Test Suite & Verification

### Track 1: Verification Suite

- [x] **Task 3.1 (#9):** Implement Container Verification Test Suite (`REQ-0001`
  through `REQ-0012`)
