# cm-connect Project Instructions & Context

`cm-connect` provides an unprivileged, userspace Docker sandbox environment for running the CodeMender (`cm`) CLI against codebases with state persistence and Google Cloud ADC authentication.

## Architecture & Structure

- `docker/Dockerfile`: Ubuntu 24.04 image with `cm` CLI installed, non-root userspace execution (`codemender` user with dynamic host UID/GID).
- `Makefile`: Build automation (`make build` passing `--build-arg USER_UID=$(id -u) --build-arg USER_GID=$(id -g)`).
- `bin/cm-sandbox`: Executable host runner script that launches the prebuilt image with interactive volume mounts (`/workspace`, `/home/codemender/.codemender`, `/home/codemender/.config/gcloud:ro`).
- `openspec/`: OpenSpec capability specifications (`spec.md` and `design.md` under `openspec/specs/runner/cm-docker-container/`).

## Development Rules

- **Markdown Formatting**: Always run `mdformat --wrap 80 <file.md>` after creating or editing Markdown files.
- **Commits**: Use the `/commit` workflow. Every commit must be a single logical unit of work (containing as much as it needs and no more) with explicit Why and What sections.
- **Pre-Commit Safety**: Check for loose threads before committing, ask if any remain, and never touch foreign working copy changes without permission.
