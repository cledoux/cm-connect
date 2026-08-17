# CodeMender Connect (`cm-connect`)

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

## 1. Build the Runner Image

Build the Docker image using the `Makefile`:

```bash
make build
```

This compiles the `cm-runner:latest` image, installs runtime dependencies,
configures the unprivileged `codemender` user (UID 1000), and provisions the
official `cm` CLI.

______________________________________________________________________

## 2. Using `cm-runner` and `cm-shell`

### Interactive Shell (`bin/cm-shell`)

To drop into an interactive `/bin/bash` shell in `/workspace` as the
`codemender` user:

```bash
/path/to/cm-connect/bin/cm-shell
```

> **Tip:** Add `cm-connect/bin` to your system `$PATH` or create an alias:
>
> ```bash
> alias cm-shell="/path/to/cm-connect/bin/cm-shell"
> ```

### Headless & Batch Scanning (`bin/cm-runner`)

Use `bin/cm-runner` for automated, non-interactive execution and CI/CD
pipelines:

```bash
# Scan full workspace (default target '.')
./bin/cm-runner find

# Scan scoped sub-path
./bin/cm-runner find src/auth

# Forward CodeMender flags
./bin/cm-runner find -- --format=sarif -y
```

______________________________________________________________________

## 3. How Volume Mounts & State Persistence Work

When `bin/cm-runner` or `bin/cm-shell` runs, it mounts host directories into the
container:

| Host Path                | Container Path                    | Permissions | Purpose                                             |
| :----------------------- | :-------------------------------- | :---------- | :-------------------------------------------------- |
| `$(pwd)`                 | `/workspace`                      | `rw`        | Target source code to analyze and patch.            |
| `${HOME}/.config/gcloud` | `/home/codemender/.config/gcloud` | `ro`        | Application Default Credentials (ADC) for Vertex AI |

### Non-Root Userspace Isolation

- The container runs as user `codemender` (`UID=1000`, `GID=1000`).
- Any files or patches created in `/workspace` are owned by your standard user
  on the host, avoiding `root:root` permission issues.

______________________________________________________________________

## 4. Environment Overrides

| Variable        | Default            | Description                             |
| :-------------- | :----------------- | :-------------------------------------- |
| `CM_IMAGE_NAME` | `cm-runner:latest` | Custom Docker image name/tag to execute |
