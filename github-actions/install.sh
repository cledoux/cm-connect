#!/usr/bin/env bash
# github-actions/install.sh - CodeMender GitHub Actions CI/CD PR Review Workflow Installer
# Governing Spec: openspec/specs/workflow/cm-pr-workflow/spec.md (REQ-0008)
# Governing Design: openspec/specs/workflow/cm-pr-workflow/design.md (Section 7)
set -euo pipefail

show_help() {
  cat << 'EOF'
Usage: ./github-actions/install.sh [OPTIONS] <path-to-target-repo>

Automates onboarding and installation of CodeMender GitHub Actions PR review workflow:
- Auto-discovers target repository slug (owner/repo) from git remote
- Authenticates to GitHub Container Registry (ghcr.io) via GitHub CLI (gh)
- Builds cm-runner container image locally with OCI image source label
- Pushes container image to target repository's GHCR namespace
- Templates .github/workflows/codemender.yml with resolved image tag
- Copies companion scripts (filter_findings.jq, publish_comments.py, setup-wif.sh, terraform)

Options:
  --skip-build                  Skip local Docker build and push to GHCR
  --image <tag>, --image=<tag>  Override container image reference in templated workflow
  --repo <slug>, --repo=<slug>  Explicitly specify target repository slug (owner/repo)
  -h, --help                    Show this help message and exit

Examples:
  ./github-actions/install.sh /path/to/target-repo
  ./github-actions/install.sh --skip-build /path/to/target-repo
  ./github-actions/install.sh --image custom-registry.io/org/cm:v1 /path/to/target-repo
  ./github-actions/install.sh --repo my-org/my-app /path/to/target-repo
EOF
}

SKIP_BUILD="false"
CUSTOM_IMAGE=""
TARGET_REPO_SLUG=""
POSITIONAL_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      show_help
      exit 0
      ;;
    --skip-build)
      SKIP_BUILD="true"
      shift
      ;;
    --image)
      if [ -z "${2:-}" ]; then
        echo "Error: --image requires an argument." >&2
        echo "Usage: $0 [OPTIONS] <path-to-target-repo>" >&2
        exit 1
      fi
      CUSTOM_IMAGE="$2"
      shift 2
      ;;
    --image=*)
      CUSTOM_IMAGE="${1#*=}"
      shift
      ;;
    --repo)
      if [ -z "${2:-}" ]; then
        echo "Error: --repo requires an argument." >&2
        echo "Usage: $0 [OPTIONS] <path-to-target-repo>" >&2
        exit 1
      fi
      TARGET_REPO_SLUG="$2"
      shift 2
      ;;
    --repo=*)
      TARGET_REPO_SLUG="${1#*=}"
      shift
      ;;
    -*)
      echo "Error: Unknown option '$1'" >&2
      echo "Usage: $0 [OPTIONS] <path-to-target-repo>" >&2
      exit 1
      ;;
    *)
      POSITIONAL_ARGS+=("$1")
      shift
      ;;
  esac
done

if [ ${#POSITIONAL_ARGS[@]} -lt 1 ]; then
  echo "Error: Missing target repository path." >&2
  echo "Usage: $0 [OPTIONS] <path-to-target-repo>" >&2
  exit 1
fi

TARGET_REPO="${POSITIONAL_ARGS[0]}"
if [ ! -d "${TARGET_REPO}" ]; then
  echo "Error: Target repository directory '${TARGET_REPO}' does not exist or is not a directory." >&2
  echo "Usage: $0 [OPTIONS] <path-to-target-repo>" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORKFLOW_SRC_DIR="${SCRIPT_DIR}/workflows"
SCRIPTS_SRC_DIR="${SCRIPT_DIR}/scripts"
TERRAFORM_SRC_DIR="${SCRIPT_DIR}/terraform"
DOCKERFILE_PATH="${REPO_ROOT}/docker/Dockerfile"

# Auto-discover target repository slug if not explicitly passed
if [ -z "${TARGET_REPO_SLUG}" ]; then
  REMOTE_URL="$(git -C "${TARGET_REPO}" config --get remote.origin.url 2>/dev/null || true)"
  if [ -n "${REMOTE_URL}" ]; then
    # Strip protocols and prefixes: git@github.com:, https://github.com/, ssh://git@github.com/, git://github.com/
    SLUG="${REMOTE_URL}"
    SLUG="${SLUG#*github.com[:/]}"
    SLUG="${SLUG#*github.com/}"
    SLUG="${SLUG%.git}"
    SLUG="${SLUG%/}"
    TARGET_REPO_SLUG="${SLUG}"
  fi
fi

# Fallback slug if could not be auto-discovered
if [ -z "${TARGET_REPO_SLUG}" ]; then
  TARGET_REPO_SLUG="cledoux/cm-runner"
fi

# Resolve image tag
if [ -n "${CUSTOM_IMAGE}" ]; then
  RESOLVED_IMAGE="${CUSTOM_IMAGE}"
else
  RESOLVED_IMAGE="ghcr.io/${TARGET_REPO_SLUG}/cm-runner:latest"
fi

# Pre-flight checks and GHCR build & push if --skip-build is not requested
if [ "${SKIP_BUILD}" != "true" ]; then
  # 1. Validate Docker daemon
  if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
    echo "Error: Docker daemon is unavailable or not running." >&2
    echo "Please ensure Docker is installed and running, or pass --skip-build to skip local image build and push." >&2
    exit 1
  fi

  # 2. Validate GitHub CLI (gh) authentication and scopes
  if ! command -v gh >/dev/null 2>&1; then
    echo "Error: GitHub CLI (gh) is not installed." >&2
    echo "Please install gh or pass --skip-build to skip local image build and push." >&2
    exit 1
  fi

  GH_STATUS_OUT="$(gh auth status 2>&1 || true)"
  if ! gh auth status >/dev/null 2>&1; then
    echo "Error: GitHub CLI (gh) is not authenticated." >&2
    echo "Please authenticate with 'gh auth login' (with write:packages scope), or pass --skip-build to skip local image build and push." >&2
    exit 1
  fi

  if [[ "${RESOLVED_IMAGE}" =~ ^ghcr\.io/ ]] && ! echo "${GH_STATUS_OUT}" | grep -q "write:packages"; then
    echo "Warning: GitHub CLI (gh) token may be missing the 'write:packages' scope required for ghcr.io." >&2
    echo "If pushing fails, run: gh auth refresh -s write:packages" >&2
  fi

  echo "==> Authenticating Docker to GitHub Container Registry (ghcr.io)..."
  GH_TOKEN="$(gh auth token 2>/dev/null || true)"
  if [ -n "${GH_TOKEN}" ]; then
    echo "${GH_TOKEN}" | docker login ghcr.io -u USER --password-stdin
  fi

  echo "==> Building cm-runner container image (${RESOLVED_IMAGE})..."
  docker build \
    --label "org.opencontainers.image.source=https://github.com/${TARGET_REPO_SLUG}" \
    -t "${RESOLVED_IMAGE}" \
    -f "${DOCKERFILE_PATH}" \
    "${REPO_ROOT}"

  echo "==> Pushing container image to ${RESOLVED_IMAGE}..."
  if ! docker push "${RESOLVED_IMAGE}"; then
    echo "Error: Failed to push container image to ${RESOLVED_IMAGE}." >&2
    if [[ "${RESOLVED_IMAGE}" =~ ^ghcr\.io/ ]]; then
      echo "This usually happens when your GitHub CLI token lacks the 'write:packages' scope." >&2
      echo "To grant the required permission, run:" >&2
      echo "  gh auth refresh -s write:packages" >&2
      echo "Then re-run this installation script, or pass --skip-build to skip building and pushing." >&2
    fi
    exit 1
  fi
fi

echo "==> Installing CodeMender workflow and companion assets to ${TARGET_REPO}..."
mkdir -p "${TARGET_REPO}/.github/workflows"
mkdir -p "${TARGET_REPO}/.github/scripts"

# Template codemender.yml with the resolved image tag
if [ -f "${WORKFLOW_SRC_DIR}/codemender.yml" ]; then
  sed "s|ghcr.io/cledoux/cm-runner:latest|${RESOLVED_IMAGE}|g" "${WORKFLOW_SRC_DIR}/codemender.yml" > "${TARGET_REPO}/.github/workflows/codemender.yml"
fi

# Copy companion scripts
if [ -f "${SCRIPTS_SRC_DIR}/filter_findings.jq" ]; then
  cp "${SCRIPTS_SRC_DIR}/filter_findings.jq" "${TARGET_REPO}/.github/scripts/filter_findings.jq"
fi

if [ -f "${SCRIPTS_SRC_DIR}/publish_comments.py" ]; then
  cp "${SCRIPTS_SRC_DIR}/publish_comments.py" "${TARGET_REPO}/.github/scripts/publish_comments.py"
  chmod +x "${TARGET_REPO}/.github/scripts/publish_comments.py"
fi

if [ -f "${SCRIPTS_SRC_DIR}/setup-wif.sh" ]; then
  cp "${SCRIPTS_SRC_DIR}/setup-wif.sh" "${TARGET_REPO}/.github/scripts/setup-wif.sh"
  chmod +x "${TARGET_REPO}/.github/scripts/setup-wif.sh"
fi

# Copy Terraform module if available (including dotfiles like .gitignore)
if [ -d "${TERRAFORM_SRC_DIR}" ]; then
  mkdir -p "${TARGET_REPO}/.github/terraform"
  cp -R "${TERRAFORM_SRC_DIR}"/. "${TARGET_REPO}/.github/terraform/"
fi

echo "✓ Successfully installed CodeMender workflow to ${TARGET_REPO}/.github/workflows/codemender.yml"
echo "Next steps:"
echo "1. Provision Google Cloud WIF & IAM via Terraform (.github/terraform) or helper script (.github/scripts/setup-wif.sh)"
echo "2. Add GCP_WIF_PROVIDER and GCP_SERVICE_ACCOUNT secrets to your GitHub repository"
