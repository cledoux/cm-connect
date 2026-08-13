FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV CM_TELEMETRY_OPT_OUT=1

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

# Create unprivileged userspace user and mount directories matching host UID/GID
ARG USER_UID=1000
ARG USER_GID=1000

RUN if id "ubuntu" >/dev/null 2>&1; then userdel -r ubuntu 2>/dev/null || true; fi \
    && if getent group "ubuntu" >/dev/null 2>&1; then groupdel ubuntu 2>/dev/null || true; fi \
    && if ! getent group "${USER_GID}" >/dev/null 2>&1; then groupadd -g "${USER_GID}" codemender; fi \
    && if ! id -u "${USER_UID}" >/dev/null 2>&1; then useradd -u "${USER_UID}" -g "${USER_GID}" -m -s /bin/bash codemender; fi \
    && mkdir -p /workspace /home/codemender/.codemender /home/codemender/.config/gcloud \
    && chmod -R 777 /home/codemender \
    && chown -R "${USER_UID}:${USER_GID}" /workspace /home/codemender

# Switch to unprivileged userspace user
USER codemender
WORKDIR /workspace

# Drop directly into interactive shell
CMD ["/bin/bash"]
