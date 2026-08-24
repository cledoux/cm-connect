#!/usr/bin/env bash
# tests/test_workflow.sh - CodeMender GitHub Actions CI/CD Workflow Test Runner
# Governing: REQ-0001, REQ-0002, REQ-0004, REQ-0005, REQ-0006, REQ-0007, REQ-0008, REQ-TEST.7
set -euo pipefail

# ANSI color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
FIXTURES_DIR="${REPO_ROOT}/tests/fixtures/workflow"
WORKFLOW_TEMPLATE="${REPO_ROOT}/github-actions/workflows/codemender.yml"
INSTALL_SCRIPT="${REPO_ROOT}/github-actions/scripts/install.sh"
FILTER_JQ_SCRIPT="${REPO_ROOT}/github-actions/scripts/filter_findings.jq"
PUBLISH_SCRIPT="${REPO_ROOT}/github-actions/scripts/publish_comments.py"
SETUP_WIF_SCRIPT="${REPO_ROOT}/github-actions/scripts/setup-wif.sh"
TERRAFORM_DIR="${REPO_ROOT}/github-actions/terraform"

PASSED_TESTS=0
FAILED_TESTS=0

run_with_timeout() {
  local timeout_duration="$1"
  shift
  if command -v timeout >/dev/null 2>&1; then
    timeout "${timeout_duration}" "$@"
  else
    "$@"
  fi
}

run_test() {
  local test_name="$1"
  local test_cmd="$2"
  printf "Running test: %-60s ... " "${test_name}"
  if run_with_timeout 15s bash -c "${test_cmd}" >/dev/null 2>&1; then
    printf "${GREEN}PASS${NC}\n"
    PASSED_TESTS=$((PASSED_TESTS + 1))
  else
    printf "${RED}FAIL${NC}\n"
    FAILED_TESTS=$((FAILED_TESTS + 1))
  fi
}

test_workflow_template() {
  echo -e "\n${BLUE}========================================${NC}"
  echo -e "${BLUE} Suite: Workflow Template (codemender.yml)${NC}"
  echo -e "${BLUE}========================================${NC}"

  # 1. File existence and non-empty
  run_test "Workflow template file exists and is non-empty [REQ-0008]" \
    "[ -s '${WORKFLOW_TEMPLATE}' ]"

  # 2. YAML syntax validation
  run_test "Workflow template is valid YAML syntax [REQ-0008.19]" \
    "python3 -c \"import yaml; yaml.safe_load(open('${WORKFLOW_TEMPLATE}'))\""

  # 3. PR triggers and target branch
  run_test "Triggers on pull_request (opened, synchronize, reopened) to main [REQ-0001]" \
    "python3 -c \"
import yaml, sys
d = yaml.safe_load(open('${WORKFLOW_TEMPLATE}'))
on_block = d.get('on') or d.get(True) or {}
pr = on_block.get('pull_request') or {}
types = set(pr.get('types', []))
branches = pr.get('branches', [])
if {'opened', 'synchronize', 'reopened'}.issubset(types) and 'main' in branches:
    sys.exit(0)
sys.exit(1)
\""

  # 4. Workflow permissions
  run_test "Declares required permissions (contents, id-token, pull-requests) [REQ-0002]" \
    "python3 -c \"
import yaml, sys
d = yaml.safe_load(open('${WORKFLOW_TEMPLATE}'))
p = d.get('permissions', {})
if p.get('contents') == 'read' and p.get('id-token') == 'write' and p.get('pull-requests') == 'write':
    sys.exit(0)
sys.exit(1)
\""

  # 5. Scan job definition and steps
  run_test "Scan job checks out with fetch-depth: 0 [REQ-0001]" \
    "grep -q 'fetch-depth: 0' '${WORKFLOW_TEMPLATE}'"

  run_test "Scan job authenticates via google-github-actions/auth@v2 [REQ-0002]" \
    "grep -q 'google-github-actions/auth@v2' '${WORKFLOW_TEMPLATE}'"

  run_test "Scan job extracts diff to commit.diff [REQ-0001]" \
    "grep -q 'commit.diff' '${WORKFLOW_TEMPLATE}'"

  run_test "Scan job runs container scanner [REQ-0003]" \
    "grep -q 'cm-runner' '${WORKFLOW_TEMPLATE}'"

  run_test "Scan job generates dynamic matrix via filter_findings.jq [REQ-0004]" \
    "grep -q 'filter_findings.jq' '${WORKFLOW_TEMPLATE}'"

  # 6. Fix job definition and matrix strategy
  run_test "Fix job defines dynamic matrix strategy with fail-fast: false [REQ-0005]" \
    "grep -q 'strategy:' '${WORKFLOW_TEMPLATE}' && grep -q 'fail-fast: false' '${WORKFLOW_TEMPLATE}' && grep -q 'findings_matrix' '${WORKFLOW_TEMPLATE}'"

  run_test "Fix job mounts FUSE device and read-only workspace [REQ-0005, REQ-0007]" \
    "grep -q '/dev/fuse' '${WORKFLOW_TEMPLATE}' && grep -q '/workspace-ro:ro' '${WORKFLOW_TEMPLATE}'"

  run_test "Fix job publishes comments via publish_comments.py [REQ-0006]" \
    "grep -q 'publish_comments.py' '${WORKFLOW_TEMPLATE}'"
}

test_installer_script() {
  echo -e "\n${BLUE}========================================${NC}"
  echo -e "${BLUE} Suite: Target Repository Installer (install.sh)${NC}"
  echo -e "${BLUE}========================================${NC}"

  # 1. Script existence and executable
  run_test "Script exists and is executable [REQ-0008]" \
    "[ -f '${INSTALL_SCRIPT}' ] && [ -x '${INSTALL_SCRIPT}' ]"

  # 2. Bash syntax check
  run_test "Script passes bash syntax check (bash -n)" \
    "bash -n '${INSTALL_SCRIPT}'"

  # 3. Help flags
  run_test "Help flag --help returns code 0 and usage" \
    "out=\$('${INSTALL_SCRIPT}' --help 2>&1) && echo \"\$out\" | grep -q -i 'usage:'"

  run_test "Help flag -h returns code 0 and usage" \
    "out=\$('${INSTALL_SCRIPT}' -h 2>&1) && echo \"\$out\" | grep -q -i 'usage:'"

  # 4. Mandatory argument validations
  run_test "Fails when no arguments provided (exit code 1)" \
    "! '${INSTALL_SCRIPT}' 2>/dev/null"

  run_test "Fails when non-existent target directory provided" \
    "! '${INSTALL_SCRIPT}' /non/existent/target/path/$$ 2>/dev/null"

  run_test "Fails when target is a regular file" \
    "tmpfile=\$(mktemp) && ! '${INSTALL_SCRIPT}' \"\$tmpfile\" 2>/dev/null; rm -f \"\$tmpfile\""

  # 5. Full installation to temporary directory
  local tmp_install_dir
  tmp_install_dir=$(mktemp -d)

  run_test "Installs workflow and scripts to temporary directory [REQ-0008.20]" \
    "'${INSTALL_SCRIPT}' '${tmp_install_dir}'"

  run_test "Installed codemender.yml exists and matches source checksum" \
    "[ -s '${tmp_install_dir}/.github/workflows/codemender.yml' ] && diff -q '${WORKFLOW_TEMPLATE}' '${tmp_install_dir}/.github/workflows/codemender.yml'"

  run_test "Installed setup-wif.sh exists, is executable, and matches checksum" \
    "[ -x '${tmp_install_dir}/.github/scripts/setup-wif.sh' ] && diff -q '${SETUP_WIF_SCRIPT}' '${tmp_install_dir}/.github/scripts/setup-wif.sh'"

  run_test "Installed filter_findings.jq exists and matches source checksum" \
    "[ -s '${tmp_install_dir}/.github/scripts/filter_findings.jq' ] && diff -q '${FILTER_JQ_SCRIPT}' '${tmp_install_dir}/.github/scripts/filter_findings.jq'"

  run_test "Installed publish_comments.py exists and matches source checksum" \
    "[ -s '${tmp_install_dir}/.github/scripts/publish_comments.py' ] && diff -q '${PUBLISH_SCRIPT}' '${tmp_install_dir}/.github/scripts/publish_comments.py'"

  run_test "Installed terraform module exists and matches source checksums" \
    "[ -s '${tmp_install_dir}/.github/terraform/main.tf' ] && diff -q '${TERRAFORM_DIR}/main.tf' '${tmp_install_dir}/.github/terraform/main.tf'"

  run_test "Idempotent re-installation overwrites cleanly without error" \
    "'${INSTALL_SCRIPT}' '${tmp_install_dir}'"

  rm -rf "${tmp_install_dir}"
}

test_filter_findings_jq() {
  echo -e "\n${BLUE}========================================${NC}"
  echo -e "${BLUE} Suite: Diff Finding Filter & Matrix Generator (filter_findings.jq)${NC}"
  echo -e "${BLUE}========================================${NC}"

  # 1. Existence and non-empty
  run_test "Filter script exists and is non-empty [REQ-0008]" \
    "[ -s '${FILTER_JQ_SCRIPT}' ]"

  # 2. jq syntax check
  run_test "Filter script is valid jq syntax" \
    "jq -n --rawfile diff '${FIXTURES_DIR}/commit.diff' -f '${FILTER_JQ_SCRIPT}' '${FIXTURES_DIR}/findings_in_diff.json' >/dev/null"

  # 3. In-diff retention and schema formatting
  run_test "Retains all in-diff findings with matrix schema [REQ-0004.6]" \
    "out=\$(jq --rawfile diff '${FIXTURES_DIR}/commit.diff' -f '${FILTER_JQ_SCRIPT}' '${FIXTURES_DIR}/findings_in_diff.json') && \
     [ \$(echo \"\$out\" | jq 'length') -eq 2 ] && \
     [ \"\$(echo \"\$out\" | jq -r '.[0].file_path')\" = 'cmd/server/main.go' ] && \
     [ \$(echo \"\$out\" | jq '.[0].start_line') -eq 81 ] && \
     [ \"\$(echo \"\$out\" | jq -r '.[0].severity')\" = 'CRITICAL' ] && \
     [ \"\$(echo \"\$out\" | jq -r '.[1].file_path')\" = 'pkg/auth/store.go' ] && \
     [ \$(echo \"\$out\" | jq '.[1].start_line') -eq 42 ] && \
     [ \"\$(echo \"\$out\" | jq -r '.[1].severity')\" = 'HIGH' ] && \
     [ -n \"\$(echo \"\$out\" | jq -r '.[0].payload.FilePath')\" ]"

  # 4. Out-of-diff exclusion
  run_test "Excludes all out-of-diff findings, returning [] [REQ-0004.6]" \
    "out=\$(jq --rawfile diff '${FIXTURES_DIR}/commit.diff' -f '${FILTER_JQ_SCRIPT}' '${FIXTURES_DIR}/findings_out_of_diff.json') && \
     [ \"\$out\" = '[]' ]"

  # 5. Mixed severity sorting
  run_test "Sorts mixed in-diff findings descending by severity (CRITICAL > HIGH > MEDIUM) [REQ-0004.6]" \
    "out=\$(jq --rawfile diff '${FIXTURES_DIR}/commit.diff' -f '${FILTER_JQ_SCRIPT}' '${FIXTURES_DIR}/findings_mixed.json') && \
     [ \$(echo \"\$out\" | jq 'length') -eq 3 ] && \
     [ \"\$(echo \"\$out\" | jq -r '.[0].severity')\" = 'CRITICAL' ] && \
     [ \"\$(echo \"\$out\" | jq -r '.[1].severity')\" = 'HIGH' ] && \
     [ \"\$(echo \"\$out\" | jq -r '.[2].severity')\" = 'MEDIUM' ]"

  # 6. Max concurrency throttling
  run_test "Throttles findings array to max limit (--argjson max 2) [REQ-0004.6]" \
    "out=\$(jq --rawfile diff '${FIXTURES_DIR}/commit.diff' --argjson max 2 -f '${FILTER_JQ_SCRIPT}' '${FIXTURES_DIR}/findings_mixed.json') && \
     [ \$(echo \"\$out\" | jq 'length') -eq 2 ] && \
     [ \"\$(echo \"\$out\" | jq -r '.[0].severity')\" = 'CRITICAL' ] && \
     [ \"\$(echo \"\$out\" | jq -r '.[1].severity')\" = 'HIGH' ]"

  # 7. Empty findings handling
  run_test "Handles empty findings input cleanly, returning []" \
    "out=\$(echo '[]' | jq --rawfile diff '${FIXTURES_DIR}/commit.diff' -f '${FILTER_JQ_SCRIPT}') && \
     [ \"\$out\" = '[]' ]"

  # 8. Empty diff handling
  run_test "Handles empty diff input cleanly, returning []" \
    "empty_diff=\$(mktemp) && \
     out=\$(jq --rawfile diff \"\$empty_diff\" -f '${FILTER_JQ_SCRIPT}' '${FIXTURES_DIR}/findings_mixed.json') && \
     [ \"\$out\" = '[]' ]; rm -f \"\$empty_diff\""
}

test_publish_comments() {
  echo -e "\n${BLUE}========================================${NC}"
  echo -e "${BLUE} Suite: PR Review Comment Publisher (publish_comments.py)${NC}"
  echo -e "${BLUE}========================================${NC}"

  # 1. Existence and executable
  run_test "Script exists and is executable [REQ-0008]" \
    "[ -f '${PUBLISH_SCRIPT}' ] && [ -x '${PUBLISH_SCRIPT}' ]"

  # 2. Python syntax check
  run_test "Script passes Python syntax check (py_compile)" \
    "python3 -m py_compile '${PUBLISH_SCRIPT}'"

  # 3. Help flags
  run_test "Help flag --help displays usage and returns code 0" \
    "python3 '${PUBLISH_SCRIPT}' --help | grep -q -i 'usage:'"

  run_test "Help flag -h displays usage and returns code 0" \
    "python3 '${PUBLISH_SCRIPT}' -h | grep -q -i 'usage:'"

  # 4. Mandatory argument error
  run_test "Fails when invoked with no arguments or env vars (exit code 1)" \
    "! python3 '${PUBLISH_SCRIPT}' 2>/dev/null"

  # 5. Single-line review suggestion formatting
  run_test "Translates single-line hunk into review suggestion format [REQ-0006]" \
    "python3 -c \"import json, sys; sys.path.insert(0, '${REPO_ROOT}/github-actions/scripts'); from publish_comments import format_review_comment_body; env=json.load(open('${FIXTURES_DIR}/change_envelope_single_line.json')); body=format_review_comment_body(env, env['hunks'][0]); assert 'CodeMender Auto-Fix' in body and 'suggestion' in body and 'query :=' in body\""

  # 6. Multi-line review suggestion formatting
  run_test "Translates multi-line hunk into review suggestion format [REQ-0006]" \
    "python3 -c \"import json, sys; sys.path.insert(0, '${REPO_ROOT}/github-actions/scripts'); from publish_comments import format_review_comment_body; env=json.load(open('${FIXTURES_DIR}/change_envelope_multiline.json')); body=format_review_comment_body(env, env['hunks'][0]); assert 'CodeMender Auto-Fix' in body and 'suggestion' in body and 'row :=' in body\""

  # 7. Fallback issue comment formatting on HTTP 422
  run_test "Formats fallback top-level issue comment for out-of-diff findings [REQ-0007.4]" \
    "python3 -c \"import json, sys; sys.path.insert(0, '${REPO_ROOT}/github-actions/scripts'); from publish_comments import format_fallback_issue_comment_body; env=json.load(open('${FIXTURES_DIR}/change_envelope_single_line.json')); body=format_fallback_issue_comment_body(env, env['hunks'][0]); assert 'CodeMender Security Finding (Outside PR Diff):' in body and 'diff' in body\""

  # 8. Unresolved finding handling
  run_test "Handles unresolved finding without review comments [REQ-0005, REQ-TEST.2]" \
    "out=\$(python3 '${PUBLISH_SCRIPT}' '${FIXTURES_DIR}/change_envelope_unresolved.json') && \
     [ \"\$(echo \"\$out\" | jq -r .status)\" = 'UNRESOLVED' ] && \
     [ \$(echo \"\$out\" | jq .review_comments_posted) -eq 0 ]"

  # 9. Step summary card generation
  run_test "Generates markdown status card in GITHUB_STEP_SUMMARY [REQ-TEST.2]" \
    "summary_file=\$(mktemp) && \
     GITHUB_STEP_SUMMARY=\"\$summary_file\" python3 '${PUBLISH_SCRIPT}' '${FIXTURES_DIR}/change_envelope_single_line.json' >/dev/null && \
     [ -s \"\$summary_file\" ] && \
     grep -q '### 🛡️ CodeMender Remediation Summary' \"\$summary_file\" && \
     grep -q 'SQL Injection' \"\$summary_file\"; rm -f \"\$summary_file\""
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

  local tf_dir="${TERRAFORM_DIR}"

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
      echo "  workflow_template  - Test GitHub Actions codemender.yml workflow template"
      echo "  installer_script   - Test automated target repository install.sh script"
      echo "  filter_findings_jq - Test diff-scoped finding filter and matrix generator"
      echo "  publish_comments   - Test PR review comment publisher script"
      echo "  setup_wif_script   - Test Google Cloud WIF setup helper script"
      echo "  terraform_module   - Test Google Cloud WIF Terraform module"
      echo "  all                - Run all test suites"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

case "${TARGET_TEST}" in
  workflow_template)
    test_workflow_template
    ;;
  installer_script)
    test_installer_script
    ;;
  filter_findings_jq)
    test_filter_findings_jq
    ;;
  publish_comments)
    test_publish_comments
    ;;
  setup_wif_script)
    test_setup_wif_script
    ;;
  terraform_module)
    test_terraform_module
    ;;
  all)
    test_workflow_template
    test_installer_script
    test_filter_findings_jq
    test_publish_comments
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

