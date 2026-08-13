# Proposal: CodeMender Interactive Sandbox Container

**Change ID:** `codemender-interactive-sandbox` \
**Status:** Active \
**Author:** Charles LeDoux \
**Target Spec:** `openspec/specs/runner/cm-docker-container/spec.md` \
**Bug/Epic:** N/A

## Why

To simplify local development and experimentation with CodeMender, we need a
lightweight interactive Docker sandbox. Rather than executing a complex headless
pipeline, the container provides an interactive shell where developers can
directly execute the `cm` CLI against a mounted codebase with persistent state
and Google Cloud ADC authentication.

## What Changes

- Create a minimal Dockerfile that installs the CodeMender CLI (`cm`) and core
  build utilities on Ubuntu 24.04.
- Support mounting the target workspace directory to `/workspace`.
- Support mounting the host configuration and state directory (`~/.codemender`
  or project `.codemender`) to `/root/.codemender` so state and initialization
  persist across container sessions.
- Support mounting Google Cloud Application Default Credentials (ADC) from the
  host.
- Set the default container entrypoint to drop directly into an interactive Bash
  shell (`/bin/bash`).

## Capabilities (The Core Contract)

### New Capabilities

- `cm-interactive-sandbox`: Interactive Docker container environment
  pre-equipped with CodeMender CLI, state persistence mounts, and ADC auth. Maps
  to `openspec/specs/runner/cm-docker-container/spec.md`.

### Modified Capabilities

- None.

## Impact

- Delivers a lean, immediately testable interactive environment in `cm-connect`
  for running CodeMender commands (`cm init`, `cm find`, `cm verify`, `cm fix`).
