# Governing: REQ-0001, REQ-0002, REQ-0010, REQ-0011

# Stage 1: Build Go runner binary
FROM golang:1.24-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum* /app/
COPY cmd/cm-runner /app/cmd/cm-runner
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /app/bin/cm-runner ./cmd/cm-runner

# Stage 2: Runtime image
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Install core build and developer tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    unzip \
    git \
    python3 \
    python3-pip \
    build-essential \
    make \
    jq \
    && rm -rf /var/lib/apt/lists/*

# Download and install CodeMender CLI from Artifact Registry
ARG CM_DOWNLOAD_URL="https://artifactregistry.googleapis.com/download/v1/projects/cmoc-prod/locations/us/repositories/codemender-cli-production/files/cm%3Astable%3Acm-linux-amd64.zip:download?alt=media"

RUN curl -fsSL -o /tmp/cm-linux-amd64.zip "${CM_DOWNLOAD_URL}" \
    && unzip /tmp/cm-linux-amd64.zip -d /tmp/cm-bin \
    && chmod +x /tmp/cm-bin/cm \
    && mv /tmp/cm-bin/cm /usr/local/bin/cm \
    && rm -rf /tmp/cm-linux-amd64.zip /tmp/cm-bin

# Install compiled Go entrypoint runner from builder stage
COPY --from=builder /app/bin/cm-runner /usr/local/bin/cm-runner
RUN chmod +x /usr/local/bin/cm-runner

# Create unprivileged userspace user and directories
ARG USER_UID=1000
ARG USER_GID=1000

RUN if id "ubuntu" >/dev/null 2>&1; then userdel -r ubuntu 2>/dev/null || true; fi \
    && if getent group "ubuntu" >/dev/null 2>&1; then groupdel ubuntu 2>/dev/null || true; fi \
    && if ! getent group "${USER_GID}" >/dev/null 2>&1; then groupadd -g "${USER_GID}" codemender; fi \
    && if ! id -u "${USER_UID}" >/dev/null 2>&1; then useradd -u "${USER_UID}" -g "${USER_GID}" -m -s /bin/bash codemender; fi \
    && mkdir -p /workspace /home/codemender/.codemender /home/codemender/.config/gcloud \
    && chown -R "${USER_UID}:${USER_GID}" /workspace /home/codemender

# Switch to unprivileged userspace user
USER codemender
WORKDIR /workspace

# Run build-time initialization to pre-seed default config
RUN cm init --dry-run || true

ENTRYPOINT ["/usr/local/bin/cm-runner"]
