---
archetype: capability
status: draft
category: runner
name: cm-docker-container
governing_proposal: codemender-interactive-sandbox
---

# CodeMender Interactive Sandbox Container Specification

## Overview

The `cm-interactive-sandbox` capability defines a lean, non-root interactive
Docker container environment for CodeMender (`cm`). The container runs strictly
in userspace with an unprivileged user, provides an interactive Bash shell with
pre-installed `cm` CLI tooling, state/configuration volume persistence across
runs, and Google Cloud ADC authentication.

## Requirements

### REQ-0001: CodeMender CLI Binary Installation

The container image MUST download and install the official CodeMender CLI (`cm`)
stable binary for Linux amd64 (`cm-linux-amd64.zip`) from Google Cloud Artifact
Registry into the system `$PATH` (`/usr/local/bin/cm`) with world-executable
permissions.

#### Scenario: Verify CodeMender binary availability

- **Given** a built container image
- **When** executing `cm --version`
- **Then** the command MUST exit with code 0 and output the installed version
  string.

### REQ-0002: Base Linux Runtime Environment

The container MUST provide a standard Ubuntu 24.04 environment containing core
developer and build utilities, including `/bin/bash`, `git`, `curl`, `unzip`,
`ca-certificates`, `python3`, `build-essential`, `make`, and `jq`.

#### Scenario: Verify runtime tools

- **Given** the running container environment
- **When** checking for developer toolchains
- **Then** `bash`, `git`, `curl`, `python3`, and `make` MUST be executable on
  `$PATH`.

### REQ-0003: Non-Root Userspace Execution

The container MUST execute as an unprivileged, non-root user (`codemender`, UID
1000, GID 1000) by default. The container MUST NOT run processes or the default
interactive shell as `root`. All user configuration, state directories, and home
directory structures MUST be rooted under `/home/codemender`.

#### Scenario: Verify unprivileged userspace execution

- **Given** a running container instance
- **When** executing `id`
- **Then** the current UID MUST NOT be 0 (root) and MUST match the unprivileged
  user `codemender` (UID 1000).

### REQ-0004: Workspace Volume Binding

The container MUST mount the target source code repository from the host to
`/workspace`, and set `/workspace` as the default working directory (`WORKDIR`).
The unprivileged user MUST have read and write permissions to `/workspace`.

#### Scenario: Inspect mounted workspace permissions

- **Given** a host source directory mounted at `/workspace`
- **When** launching the interactive container as `codemender`
- **Then** the current working directory MUST be `/workspace` with read and
  write access to source files.

### REQ-0005: Configuration and State Volume Persistence

The container MUST support mounting the host CodeMender configuration and state
directory (such as `~/.codemender` on the host) to
`/home/codemender/.codemender` inside the container, ensuring that initialized
session state, tokens, and configuration persist across container restarts.

#### Scenario: Persist initialization state across container runs

- **Given** a host directory mounted at `/home/codemender/.codemender`
- **When** running `cm init` in a container session and exiting
- **Then** state and configuration files MUST persist in the host directory and
  remain available in subsequent container invocations.

### REQ-0006: Google Cloud Authentication (ADC) Binding

The container MUST support Google Cloud Application Default Credentials (ADC) by
mounting the host's gcloud credential directory (`$HOME/.config/gcloud`) to
`/home/codemender/.config/gcloud:ro` or by passing
`GOOGLE_APPLICATION_CREDENTIALS` / `CLOUDSDK_AUTH_ACCESS_TOKEN`.

#### Scenario: Verify ADC authentication in userspace

- **Given** a host gcloud ADC directory mounted at
  `/home/codemender/.config/gcloud:ro`
- **When** executing `cm init --verify` as user `codemender`
- **Then** CodeMender MUST authenticate with Vertex AI
  (`aiplatform.googleapis.com`) successfully.

### REQ-0007: Interactive Shell Default Entrypoint

The container default entrypoint and command MUST launch an interactive Bash
shell (`/bin/bash`) as the unprivileged user, dropping the developer directly
into `/workspace` ready to execute `cm` commands manually.

#### Scenario: Drop into interactive non-root shell

- **Given** a `docker run -it` invocation without extra arguments
- **When** the container starts
- **Then** it MUST present an interactive `/bin/bash` prompt in `/workspace`
  running as user `codemender`.

### REQ-0008: Initial Unfiltered Outbound Network Access

The container MUST allow unconstrained outbound network access to allow direct
communication with `aiplatform.googleapis.com`,
`cloudresourcemanager.googleapis.com`, and package managers.

#### Scenario: Direct outbound API connectivity

- **Given** the container environment with active network
- **When** reaching `https://aiplatform.googleapis.com`
- **Then** HTTPS network requests MUST succeed directly.

### REQ-0009: Build Makefile and Runtime Script

1. **Build Automation (`Makefile`)**: The repository MUST provide a `Makefile` with a `build` target that builds the Docker image (`docker build -t cm-sandbox:latest -f docker/Dockerfile .`).
2. **Runtime Script (`bin/cm-sandbox`)**: The repository MUST provide an executable launcher script (`bin/cm-sandbox`) dedicated to running the prebuilt container image in userspace with `-it`, mounting the workspace (`$(pwd)` -> `/workspace`), state (`~/.codemender` -> `/home/codemender/.codemender`), and Google Cloud ADC (`~/.config/gcloud` -> `/home/codemender/.config/gcloud:ro`).

#### Scenario: Build and run workflow
- **Given** Docker running on the host system
- **When** executing `make build`
- **Then** the `cm-sandbox:latest` image MUST build successfully.
- **When** executing `./bin/cm-sandbox`
- **Then** the container MUST launch and drop into an interactive `/bin/bash` shell running as user `codemender`.



