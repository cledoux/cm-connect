#!/usr/bin/env bash
# Usage: ./github-actions/scripts/install.sh <path-to-target-repo>
set -euo pipefail

TARGET_REPO="${1:-}"
if [ -z "${TARGET_REPO}" ] || [ ! -d "${TARGET_REPO}" ]; then
  echo "Error: Target repository directory '${TARGET_REPO}' does not exist or is not a directory." >&2
  echo "Usage: $0 <path-to-target-repo>" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKFLOW_SRC_DIR="$(cd "${SCRIPT_DIR}/../workflows" && pwd)"

mkdir -p "${TARGET_REPO}/.github/workflows"
mkdir -p "${TARGET_REPO}/.github/scripts"

if [ -f "${WORKFLOW_SRC_DIR}/codemender.yml" ]; then
  cp "${WORKFLOW_SRC_DIR}/codemender.yml" "${TARGET_REPO}/.github/workflows/codemender.yml"
fi

if [ -f "${SCRIPT_DIR}/filter_findings.jq" ]; then
  cp "${SCRIPT_DIR}/filter_findings.jq" "${TARGET_REPO}/.github/scripts/filter_findings.jq"
fi

if [ -f "${SCRIPT_DIR}/publish_comments.py" ]; then
  cp "${SCRIPT_DIR}/publish_comments.py" "${TARGET_REPO}/.github/scripts/publish_comments.py"
  chmod +x "${TARGET_REPO}/.github/scripts/publish_comments.py"
fi

echo "✓ Successfully installed CodeMender workflow to ${TARGET_REPO}/.github/workflows/codemender.yml"
echo "Next steps:"
echo "1. Run ./github-actions/scripts/setup-wif.sh to configure Google Cloud IAM & WIF"
echo "2. Add GCP_WIF_PROVIDER and GCP_SERVICE_ACCOUNT secrets to your GitHub repository"
