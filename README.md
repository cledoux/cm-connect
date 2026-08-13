# CodeMender Interactive Sandbox (`cm-connect`)

`cm-connect` provides an unprivileged, userspace Docker sandbox for running the
[CodeMender](https://docs.cloud.google.com/gemini-enterprise-agent-platform/codemender/set-up-environment)
CLI (`cm`) across local repositories.

The sandbox isolates dynamic exploit verification and tool execution while
mounting your local workspace, preserving state in `~/.codemender`, and
authenticating seamlessly via host Application Default Credentials (ADC).

______________________________________________________________________

## Prerequisites

1. **Docker**: Docker Engine running locally.
1. **Google Cloud ADC**: Authenticated on the host:
   ```bash
   gcloud auth application-default login
   ```
1. **IAM Permissions**: Your GCP account must have the **Vertex AI User**
   (`roles/aiplatform.user`) role on the target project with the Vertex AI API
   (`aiplatform.googleapis.com`) enabled.

______________________________________________________________________

## 1. Build the Sandbox Image

Build the Docker image using the `Makefile`:

```bash
make build
```

This compiles the `cm-sandbox:latest` image, installs build dependencies,
configures the unprivileged `codemender` user (UID 1000), and provisions the
official `cm` CLI from Artifact Registry.

______________________________________________________________________

## 2. Launching the Sandbox

Use `bin/cm-sandbox` to run the container.

### Interactive Shell (Default)

From any codebase directory you want to scan:

```bash
/path/to/cm-connect/bin/cm-sandbox
```

This drops you directly into an interactive `/bin/bash` shell in `/workspace` as
the `codemender` user.

> **Tip:** Add `cm-connect/bin` to your system `$PATH` or create an alias:
>
> ```bash
> alias cm-sandbox="/path/to/cm-connect/bin/cm-sandbox"
> ```

### Direct Command Execution

You can also pass arguments directly through to the container without entering
an interactive shell:

```bash
# Verify cloud connectivity and initialize workspace
./bin/cm-sandbox cm init --verify

# Scan codebase for security vulnerabilities
./bin/cm-sandbox cm find

# Run dynamic verification on findings
./bin/cm-sandbox cm verify

# Generate security patches
./bin/cm-sandbox cm fix

# Export findings report to JSON
./bin/cm-sandbox cm report -f json > report.json
```

______________________________________________________________________

## 3. How Volume Mounts & State Persistence Work

When `bin/cm-sandbox` runs, it mounts three host directories into the container:

| Host Path | Container Path | Permissions | Purpose |
| :--- | :--- | :--- | :--- |
| `$(pwd)` | `/workspace` | `rw` | Target source code to analyze and patch. |
| `${HOME}/.codemender` | `/home/codemender/.codemender` | `rw` | Persistent state, session tokens, and configurations. |
| `${HOME}/.config/gcloud` | `/home/codemender/.config/gcloud` | `ro` | Application Default Credentials (ADC) for Vertex AI. |

### Non-Root Userspace Isolation

- The container runs as user `codemender` (`UID=1000`, `GID=1000`).
- Any files or patches created in `/workspace` or `~/.codemender` are owned by
  your standard user on the host, avoiding `root:root` permission issues.

---

## 4. Environment Overrides

| Variable | Default | Description |
| :--- | :--- | :--- |
| `CM_IMAGE_NAME` | `cm-sandbox:latest` | Custom Docker image name/tag to execute. |

