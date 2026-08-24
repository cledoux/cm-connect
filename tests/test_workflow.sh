#!/usr/bin/env bash
# tests/test_workflow.sh - CodeMender GitHub Actions CI/CD Workflow Test Runner
# Governing: REQ-0008, REQ-TEST.5
set -euo pipefail

# ANSI color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
SETUP_WIF_SCRIPT="${REPO_ROOT}/github-actions/scripts/setup-wif.sh"

PASSED_TESTS=0
FAILED_TESTS=0

run_test() {
  local test_name="$1"
  local test_cmd="$2"
  printf "Running test: %-50s ... " "${test_name}"
  if eval "${test_cmd}" >/dev/null 2>&1; then
    printf "${GREEN}PASS${NC}\n"
    PASSED_TESTS=$((PASSED_TESTS + 1))
  else
    printf "${RED}FAIL${NC}\n"
    FAILED_TESTS=$((FAILED_TESTS + 1))
  fi
}

test_setup_wif_script() {
  echo -e "\n${BLUE}========================================${NC}"
  echo -e "${BLUE} Suite: Workload Identity Federation Script (setup-wif.sh)${NC}"
  echo -e "${BLUE}========================================${NC}"

  # 1. Script existence and executable permission
  run_test "Script exists and is executable" \
    "[ -f '${SETUP_WIF_SCRIPT}' ] && [ -x '${SETUP_WIF_SCRIPT}' ]"

  # 2. Bash syntax validation
  run_test "Script passes bash syntax check (bash -n)" \
    "bash -n '${SETUP_WIF_SCRIPT}'"

  # 3. Help flags
  run_test "Help flag --help returns code 0 and usage" \
    "output=\$('${SETUP_WIF_SCRIPT}' --help 2>&1) && echo \"\$output\" | grep -q -i 'usage:'"

  run_test "Help flag -h returns code 0 and usage" \
    "output=\$('${SETUP_WIF_SCRIPT}' -h 2>&1) && echo \"\$output\" | grep -q -i 'usage:'"

  # 4. Mandatory argument validations
  run_test "Fails when no arguments provided" \
    "! '${SETUP_WIF_SCRIPT}' 2>/dev/null"

  run_test "Fails when only project is provided" \
    "! '${SETUP_WIF_SCRIPT}' --project=my-gcp-project 2>/dev/null"

  run_test "Fails when only repo is provided" \
    "! '${SETUP_WIF_SCRIPT}' --repo=my-org/my-repo 2>/dev/null"

  run_test "Fails on unknown flag" \
    "! '${SETUP_WIF_SCRIPT}' --unknown-flag 2>/dev/null"

  # 5. Long flags in --dry-run mode
  run_test "Executes with long flags in --dry-run mode" \
    "'${SETUP_WIF_SCRIPT}' --project=my-project-123 --repo=my-org/my-repo --pool=custom-pool --provider=custom-provider --sa=custom-sa --dry-run"

  # 6. Short flags in --dry-run mode
  run_test "Executes with short flags in --dry-run mode" \
    "'${SETUP_WIF_SCRIPT}' -p my-project-123 -r my-org/my-repo --dry-run"

  # 7. Positional arguments in --dry-run mode
  run_test "Executes with positional arguments in --dry-run mode" \
    "'${SETUP_WIF_SCRIPT}' --dry-run my-project-123 my-org/my-repo custom-pool custom-provider custom-sa"

  # 8. Environment variables in --dry-run mode
  run_test "Executes with environment variables" \
    "PROJECT_ID=my-project-123 GITHUB_REPO=my-org/my-repo DRY_RUN=true '${SETUP_WIF_SCRIPT}'"

  # 9. Default values verification in dry-run output
  run_test "Applies default pool, provider, and SA names" \
    "out=\$('${SETUP_WIF_SCRIPT}' -p my-proj -r my-org/my-repo --dry-run) && \
     echo \"\$out\" | grep -q 'codemender-pool' && \
     echo \"\$out\" | grep -q 'codemender-provider' && \
     echo \"\$out\" | grep -q 'codemender-runner'"

  # 10. REQ-0008.7: Pool creation command emitted
  run_test "Emits gcloud iam workload-identity-pools create [REQ-0008.7]" \
    "out=\$('${SETUP_WIF_SCRIPT}' -p my-proj -r my-org/my-repo --dry-run) && \
     echo \"\$out\" | grep -q 'gcloud iam workload-identity-pools create codemender-pool'"

  # 11. REQ-0008.8: Provider creation command with OIDC issuer and attribute mapping
  run_test "Emits gcloud iam workload-identity-pools providers create-oidc [REQ-0008.8]" \
    "out=\$('${SETUP_WIF_SCRIPT}' -p my-proj -r my-org/my-repo --dry-run) && \
     echo \"\$out\" | grep -q 'gcloud iam workload-identity-pools providers create-oidc codemender-provider' && \
     echo \"\$out\" | grep -q 'https://token.actions.githubusercontent.com' && \
     echo \"\$out\" | grep -q 'google.subject=assertion.sub,attribute.repository=assertion.repository'"

  # 12. REQ-0008.9: Service account creation command emitted
  run_test "Emits gcloud iam service-accounts create [REQ-0008.9]" \
    "out=\$('${SETUP_WIF_SCRIPT}' -p my-proj -r my-org/my-repo --dry-run) && \
     echo \"\$out\" | grep -q 'gcloud iam service-accounts create codemender-runner'"

  # 13. REQ-0008.10: Project IAM binding for roles/aiplatform.user
  run_test "Emits project IAM binding for roles/aiplatform.user [REQ-0008.10]" \
    "out=\$('${SETUP_WIF_SCRIPT}' -p my-proj -r my-org/my-repo --dry-run) && \
     echo \"\$out\" | grep -q 'gcloud projects add-iam-policy-binding my-proj' && \
     echo \"\$out\" | grep -q 'roles/aiplatform.user' && \
     echo \"\$out\" | grep -q 'serviceAccount:codemender-runner@my-proj.iam.gserviceaccount.com'"

  # 14. REQ-0008.11: Workload identity user binding on service account
  run_test "Emits SA IAM binding for roles/iam.workloadIdentityUser with principalSet [REQ-0008.11]" \
    "out=\$('${SETUP_WIF_SCRIPT}' -p my-proj -r my-org/my-repo --dry-run) && \
     echo \"\$out\" | grep -q 'gcloud iam service-accounts add-iam-policy-binding' && \
     echo \"\$out\" | grep -q 'roles/iam.workloadIdentityUser' && \
     echo \"\$out\" | grep -q 'principalSet://iam.googleapis.com/projects/' && \
     echo \"\$out\" | grep -q '/locations/global/workloadIdentityPools/codemender-pool/attribute.repository/my-org/my-repo'"

  # 15. REQ-0008.12: GitHub Secrets instructions output
  run_test "Prints exact GitHub Secrets instructions [REQ-0008.12]" \
    "out=\$('${SETUP_WIF_SCRIPT}' -p my-proj -r my-org/my-repo --dry-run) && \
     echo \"\$out\" | grep -q 'GCP_WIF_PROVIDER: projects/' && \
     echo \"\$out\" | grep -q '/locations/global/workloadIdentityPools/codemender-pool/providers/codemender-provider' && \
     echo \"\$out\" | grep -q 'GCP_SERVICE_ACCOUNT: codemender-runner@my-proj.iam.gserviceaccount.com'"
}

test_terraform_module() {
  echo -e "\n${BLUE}========================================${NC}"
  echo -e "${BLUE} Suite: Workload Identity Federation Terraform Module${NC}"
  echo -e "${BLUE}========================================${NC}"

  local tf_dir="${REPO_ROOT}/github-actions/terraform"

  # 1. Module directory and file existence
  run_test "Terraform directory exists" \
    "[ -d '${tf_dir}' ]"

  run_test "Terraform main.tf exists and is non-empty" \
    "[ -s '${tf_dir}/main.tf' ]"

  run_test "Terraform variables.tf exists and is non-empty" \
    "[ -s '${tf_dir}/variables.tf' ]"

  run_test "Terraform outputs.tf exists and is non-empty" \
    "[ -s '${tf_dir}/outputs.tf' ]"

  run_test "Terraform versions.tf exists and is non-empty" \
    "[ -s '${tf_dir}/versions.tf' ]"

  run_test "Terraform README.md exists and is non-empty" \
    "[ -s '${tf_dir}/README.md' ]"

  # 2. Key resources in main.tf
  run_test "main.tf defines google_iam_workload_identity_pool" \
    "grep -q 'resource \"google_iam_workload_identity_pool\" \"pool\"' '${tf_dir}/main.tf'"

  run_test "main.tf defines google_iam_workload_identity_pool_provider with OIDC" \
    "grep -q 'resource \"google_iam_workload_identity_pool_provider\" \"provider\"' '${tf_dir}/main.tf' && \
     grep -q 'https://token.actions.githubusercontent.com' '${tf_dir}/main.tf' && \
     grep -q '\"google.subject\"' '${tf_dir}/main.tf' && \
     grep -q '\"attribute.repository\"' '${tf_dir}/main.tf'"

  run_test "main.tf defines google_service_account" \
    "grep -q 'resource \"google_service_account\" \"runner\"' '${tf_dir}/main.tf'"

  run_test "main.tf defines roles/aiplatform.user binding" \
    "grep -q 'resource \"google_project_iam_member\" \"aiplatform_user\"' '${tf_dir}/main.tf' && \
     grep -q 'roles/aiplatform.user' '${tf_dir}/main.tf'"

  run_test "main.tf defines roles/iam.workloadIdentityUser principalSet binding" \
    "grep -q 'resource \"google_service_account_iam_member\" \"wif_user\"' '${tf_dir}/main.tf' && \
     grep -q 'roles/iam.workloadIdentityUser' '${tf_dir}/main.tf' && \
     grep -q 'principalSet://iam.googleapis.com/' '${tf_dir}/main.tf'"

  # 3. Outputs export
  run_test "outputs.tf exports gcp_wif_provider" \
    "grep -q 'output \"gcp_wif_provider\"' '${tf_dir}/outputs.tf'"

  run_test "outputs.tf exports gcp_service_account" \
    "grep -q 'output \"gcp_service_account\"' '${tf_dir}/outputs.tf'"
}

TARGET_TEST="all"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --test=*)
      TARGET_TEST="${1#*=}"
      shift
      ;;
    --test)
      TARGET_TEST="$2"
      shift 2
      ;;
    --all)
      TARGET_TEST="all"
      shift
      ;;
    -h|--help)
      echo "Usage: $0 [--test=<name>|--all]"
      echo "Supported tests:"
      echo "  setup_wif_script  - Test Google Cloud WIF setup helper script"
      echo "  terraform_module  - Test Google Cloud WIF Terraform module"
      echo "  all               - Run all test suites"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

case "${TARGET_TEST}" in
  setup_wif_script)
    test_setup_wif_script
    ;;
  terraform_module)
    test_terraform_module
    ;;
  all)
    test_setup_wif_script
    test_terraform_module
    ;;
  *)
    echo "Unknown test target: ${TARGET_TEST}" >&2
    exit 1
    ;;
esac

echo -e "\n----------------------------------------"
echo -e "Test Summary: ${GREEN}${PASSED_TESTS} passed${NC}, ${RED}${FAILED_TESTS} failed${NC}"
echo -e "----------------------------------------"

if [ "${FAILED_TESTS}" -gt 0 ]; then
  exit 1
fi
exit 0

