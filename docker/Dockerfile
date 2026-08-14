# Stage 1: Statically compile Go runner
FROM golang:1.24-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum* /app/
COPY pkg/ /app/pkg/
COPY cmd/ /app/cmd/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /app/bin/cm-runner ./cmd/cm-runner

# Stage 2: Runtime image (Debian Bookworm Slim)
FROM debian:bookworm-slim

ARG USER_UID=1000
ARG USER_GID=1000

ENV DEBIAN_FRONTEND=noninteractive
ENV NO_COLOR=1
ENV TERM=dumb

# Install runtime dependencies and CodeMender CLI
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    unzip \
    git \
    python3 \
    make \
    jq \
    && rm -rf /var/lib/apt/lists/*

ARG CM_DOWNLOAD_URL="https://artifactregistry.googleapis.com/download/v1/projects/cmoc-prod/locations/us/repositories/codemender-cli-production/files/cm%3Astable%3Acm-linux-amd64.zip:download?alt=media"

RUN curl -fsSL -o /tmp/cm-linux-amd64.zip "${CM_DOWNLOAD_URL}" \
    && unzip /tmp/cm-linux-amd64.zip -d /tmp/cm-bin \
    && chmod +x /tmp/cm-bin/cm \
    && mv /tmp/cm-bin/cm /usr/local/bin/cm \
    && rm -rf /tmp/cm-linux-amd64.zip /tmp/cm-bin

# Copy statically compiled cm-runner entrypoint
COPY --from=builder /app/bin/cm-runner /usr/local/bin/cm-runner
RUN chmod +x /usr/local/bin/cm-runner

# Create unprivileged codemender user and directories matching host UID/GID
RUN if ! getent group "${USER_GID}" >/dev/null 2>&1; then groupadd -g "${USER_GID}" codemender; fi \
    && if ! id -u "${USER_UID}" >/dev/null 2>&1; then useradd -u "${USER_UID}" -g "${USER_GID}" -m -s /bin/bash codemender; fi \
    && mkdir -p /workspace /home/codemender/.codemender \
    && chown -R "${USER_UID}:${USER_GID}" /workspace /home/codemender

USER codemender
WORKDIR /workspace

# Pre-initialize CodeMender configuration structures at build-time
RUN cm init

ENTRYPOINT ["/usr/local/bin/cm-runner"]
