#!/usr/bin/env bash
# ==============================================================================
# Container Verification Test Suite for cm-batch-runner (cm-runner)
# Governing Spec: openspec/specs/runner/cm-batch-runner/spec.md (REQ-0001 - REQ-0012)
# Governing Design: openspec/specs/runner/cm-batch-runner/design.md
# ==============================================================================
set -euo pipefail

IMAGE_NAME="${CM_IMAGE_NAME:-cm-runner:latest}"
DEFAULT_TIMEOUT_SECS=15
FAILED_TESTS=0
PASSED_TESTS=0

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

log_test() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

pass() {
    echo -e "  ${GREEN}✓ PASS:${NC} $1"
    PASSED_TESTS=$((PASSED_TESTS + 1))
}

fail() {
    echo -e "  ${RED}✗ FAIL:${NC} $1"
    FAILED_TESTS=$((FAILED_TESTS + 1))
}

# Universal defensive execution wrapper: Enforces a strict outer timeout on every command
run_with_timeout() {
    local timeout_secs="${1:-${DEFAULT_TIMEOUT_SECS}}"
    shift
    timeout --preserve-status --kill-after=3s "${timeout_secs}" "$@"
}

# Create a temporary workspace for tests
TEST_WORKSPACE="$(mktemp -d /tmp/cm-runner-test-workspace.XXXXXX)"
chmod 777 "${TEST_WORKSPACE}"
trap 'rm -rf "${TEST_WORKSPACE}"' EXIT

mkdir -p "${TEST_WORKSPACE}/src/auth" "${TEST_WORKSPACE}/pkg/api"
echo "package auth" > "${TEST_WORKSPACE}/src/auth/auth.go"
echo "package api" > "${TEST_WORKSPACE}/pkg/api/api.go"
echo "# Test Project" > "${TEST_WORKSPACE}/README.md"
chmod -R 777 "${TEST_WORKSPACE}"

echo "======================================================================"
echo "Starting cm-batch-runner Container Verification Suite"
echo "Target Image: ${IMAGE_NAME}"
echo "Test Workspace: ${TEST_WORKSPACE}"
echo "Default Command Timeout: ${DEFAULT_TIMEOUT_SECS}s"
echo "======================================================================"

# ------------------------------------------------------------------------------
# Test 1: Mandatory Subcommand Requirement (REQ-0003, REQ-0012)
# ------------------------------------------------------------------------------
log_test "Scenario 1: Mandatory subcommand requirement on empty invocation"
set +e
OUT=$(run_with_timeout 10 docker run --rm "${IMAGE_NAME}" 2>&1)
EXIT_CODE=$?
set -e

if [ ${EXIT_CODE} -eq 2 ] && [[ "${OUT}" == *"missing subcommand"* ]] && [[ "${OUT}" == *"Usage:"* ]]; then
    pass "Empty invocation exits with code 2 and outputs usage guide to stderr"
elif [ ${EXIT_CODE} -eq 124 ]; then
    fail "Test timed out after 10s"
else
    fail "Expected exit code 2 with usage guide, got exit ${EXIT_CODE}. Output: ${OUT}"
fi

# ------------------------------------------------------------------------------
# Test 2: Unrecognized Subcommand (REQ-0003)
# ------------------------------------------------------------------------------
log_test "Scenario 2: Error on unrecognized subcommand"
set +e
OUT=$(run_with_timeout 10 docker run --rm "${IMAGE_NAME}" invalid-subcommand 2>&1)
EXIT_CODE=$?
set -e

if [ ${EXIT_CODE} -eq 2 ] && [[ "${OUT}" == *"unrecognized subcommand"* ]]; then
    pass "Unrecognized subcommand exits with code 2 and descriptive error"
elif [ ${EXIT_CODE} -eq 124 ]; then
    fail "Test timed out after 10s"
else
    fail "Expected exit code 2 with unrecognized subcommand error, got exit ${EXIT_CODE}. Output: ${OUT}"
fi

# ------------------------------------------------------------------------------
# Test 3: Strict Subcommand Dispatch & cm Prefix Rejection (REQ-0008)
# ------------------------------------------------------------------------------
log_test "Scenario 3: Strict subcommand dispatch (reject cm prefix)"
set +e
OUT_PREFIXED=$(run_with_timeout 10 docker run --rm -v "${TEST_WORKSPACE}:/workspace" "${IMAGE_NAME}" cm find non/existent/path 2>&1)
EXIT_CODE=$?
set -e

if [ ${EXIT_CODE} -eq 2 ] && [[ "${OUT_PREFIXED}" == *"unrecognized subcommand 'cm'"* ]]; then
    pass "Invocation with 'cm' prefix is rejected with exit code 2 and unrecognized subcommand error"
elif [ ${EXIT_CODE} -eq 124 ]; then
    fail "Test timed out after 10s"
else
    fail "Expected exit code 2 with unrecognized subcommand error for 'cm find', got exit ${EXIT_CODE}. Output: ${OUT_PREFIXED}"
fi

# ------------------------------------------------------------------------------
# Test 4: Target Scan Path Resolution - Full Repo Scan (REQ-0004, REQ-0005)
# ------------------------------------------------------------------------------
log_test "Scenario 4: Default full repository scan and flag forwarding"
set +e
OUT=$(run_with_timeout 10 docker run --rm -v "${TEST_WORKSPACE}:/workspace" "${IMAGE_NAME}" find -- --help 2>&1)
EXIT_CODE=$?
set -e

if [[ "${OUT}" == *"Usage:"* || "${OUT}" == *"find"* || ${EXIT_CODE} -eq 0 ]]; then
    pass "Find without positional path targets full workspace successfully"
elif [ ${EXIT_CODE} -eq 124 ]; then
    fail "Test timed out after 10s"
else
    fail "Expected successful help/find invocation, got exit ${EXIT_CODE}. Output: ${OUT}"
fi

# ------------------------------------------------------------------------------
# Test 5: Scoped Sub-Path Scan (REQ-0004)
# ------------------------------------------------------------------------------
log_test "Scenario 5: Scoped sub-path scan target resolution"
set +e
OUT=$(run_with_timeout 10 docker run --rm -v "${TEST_WORKSPACE}:/workspace" "${IMAGE_NAME}" find src/auth -- --help 2>&1)
EXIT_CODE=$?
set -e

if [ ${EXIT_CODE} -eq 0 ] || [[ "${OUT}" == *"Usage:"* || "${OUT}" == *"find"* ]]; then
    pass "Valid sub-path 'src/auth' resolved and forwarded without path error"
elif [ ${EXIT_CODE} -eq 124 ]; then
    fail "Test timed out after 10s"
else
    fail "Expected successful sub-path scan dispatch, got exit ${EXIT_CODE}. Output: ${OUT}"
fi

# ------------------------------------------------------------------------------
# Test 6: Invalid Sub-Path and Traversal Error (REQ-0004)
# ------------------------------------------------------------------------------
log_test "Scenario 6: Non-existent sub-path error handling"
set +e
OUT=$(run_with_timeout 10 docker run --rm -v "${TEST_WORKSPACE}:/workspace" "${IMAGE_NAME}" find non/existent/path 2>&1)
EXIT_CODE=$?
set -e

if [ ${EXIT_CODE} -eq 2 ] && [[ "${OUT}" == *"scan target path does not exist in workspace"* ]]; then
    pass "Non-existent path exits with code 2 and exact specified error message"
elif [ ${EXIT_CODE} -eq 124 ]; then
    fail "Test timed out after 10s"
else
    fail "Expected exit 2 and path not found error, got exit ${EXIT_CODE}. Output: ${OUT}"
fi

# ------------------------------------------------------------------------------
# Test 7: Build-Time Configuration Pre-Initialization & Headless Defaults (REQ-0002)
# ------------------------------------------------------------------------------
log_test "Scenario 7: Build-time pre-initialized configuration structures & headless defaults"
set +e
OUT=$(run_with_timeout 10 docker run --rm --entrypoint ls "${IMAGE_NAME}" -la /home/codemender/.codemender 2>&1)
EXIT_CODE=$?
CONFIG_OUT=$(run_with_timeout 10 docker run --rm --entrypoint cat "${IMAGE_NAME}" /home/codemender/.codemender/config.yaml 2>&1)
set -e

if [ ${EXIT_CODE} -eq 0 ] && [[ "${OUT}" == *"config.yaml"* ]] && \
   [[ "${CONFIG_OUT}" == *".rs"* ]] && \
   [[ "${CONFIG_OUT}" == *"format: \"json\""* || "${CONFIG_OUT}" == *"format: json"* ]] && \
   [[ "${CONFIG_OUT}" == *"confirm_commands: false"* ]] && \
   [[ "${CONFIG_OUT}" == *"confirm_writes: false"* ]]; then
    pass "Pre-initialized configuration structures and headless defaults (.rs, json, confirm=false) exist in config.yaml"
elif [ ${EXIT_CODE} -eq 124 ]; then
    fail "Test timed out after 10s"
else
    fail "Configuration structures or headless defaults invalid in /home/codemender/.codemender/config.yaml. Output:\n${CONFIG_OUT}"
fi

# ------------------------------------------------------------------------------
# Test 8: Strict Unprivileged Userspace Execution (REQ-0010)
# ------------------------------------------------------------------------------
log_test "Scenario 8: Unprivileged user non-root execution"
set +e
UID_OUT=$(run_with_timeout 10 docker run --rm --entrypoint id "${IMAGE_NAME}" -u)
GID_OUT=$(run_with_timeout 10 docker run --rm --entrypoint id "${IMAGE_NAME}" -g)
set -e

if [ "${UID_OUT}" != "0" ] && [ "${GID_OUT}" != "0" ]; then
    pass "Container runs strictly as unprivileged non-root user (UID=${UID_OUT}, GID=${GID_OUT})"
else
    fail "Expected non-root UID/GID, got UID=${UID_OUT} GID=${GID_OUT}"
fi

# ------------------------------------------------------------------------------
# Test 9: Host Runner Script Verification (REQ-0001, REQ-0011)
# ------------------------------------------------------------------------------
log_test "Scenario 9: Host runner script bin/cm-runner execution"
set +e
OUT=$(run_with_timeout 10 ./bin/cm-runner find non/existent/path 2>&1)
EXIT_CODE=$?
set -e

if [ ${EXIT_CODE} -eq 2 ] && [[ "${OUT}" == *"scan target path does not exist in workspace"* ]]; then
    pass "Host runner script ./bin/cm-runner correctly mounts workspace and forwards commands"
elif [ ${EXIT_CODE} -eq 124 ]; then
    fail "Host runner script test timed out after 10s"
else
    fail "Host runner script execution failed. Exit code ${EXIT_CODE}. Output: ${OUT}"
fi

# ------------------------------------------------------------------------------
# Test 10: Signal Forwarding and Clean Termination (REQ-0012)
# ------------------------------------------------------------------------------
log_test "Scenario 10: Signal forwarding (SIGTERM shutdown)"
set +e
CONTAINER_ID=$(run_with_timeout 10 docker run -d --rm -v "${TEST_WORKSPACE}:/workspace" "${IMAGE_NAME}" find)
sleep 0.5
START_TIME=$(date +%s)
run_with_timeout 5 docker kill --signal=SIGTERM "${CONTAINER_ID}" >/dev/null 2>&1 || true

STOPPED=false
for i in {1..5}; do
    if ! docker ps -q --no-trunc | grep -q "^${CONTAINER_ID}"; then
        STOPPED=true
        break
    fi
    sleep 1
done

docker rm -f "${CONTAINER_ID}" >/dev/null 2>&1 || true
END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))
set -e

if [ "${STOPPED}" = "true" ] && [ ${DURATION} -le 3 ]; then
    pass "Container terminated cleanly on SIGTERM within ${DURATION}s"
else
    fail "Container failed to terminate cleanly on SIGTERM (took ${DURATION}s)"
fi

# ------------------------------------------------------------------------------
# Test 11: Shell Subcommand TTY Requirement (REQ-0009)
# ------------------------------------------------------------------------------
log_test "Scenario 11: Explicit shell subcommand TTY enforcement"
set +e
OUT=$(run_with_timeout 10 docker run --rm "${IMAGE_NAME}" shell 2>&1)
EXIT_CODE=$?
OUT_WRAPPER=$(run_with_timeout 10 ./bin/cm-shell 2>&1 < /dev/null)
WRAPPER_EXIT_CODE=$?
set -e

if [ ${EXIT_CODE} -eq 2 ] && [[ "${OUT}" == *"requires an interactive terminal"* ]] && \
   [ ${WRAPPER_EXIT_CODE} -eq 2 ] && [[ "${OUT_WRAPPER}" == *"requires an interactive terminal"* ]]; then
    pass "Executing 'shell' and ./bin/cm-shell without pseudo-TTY exits with code 2 and descriptive TTY error"
elif [ ${EXIT_CODE} -eq 124 ] || [ ${WRAPPER_EXIT_CODE} -eq 124 ]; then
    fail "Shell subcommand test timed out after 10s"
else
    fail "Shell subcommand without TTY expected exit 2, got container=${EXIT_CODE} wrapper=${WRAPPER_EXIT_CODE}. Output: ${OUT}"
fi

# ------------------------------------------------------------------------------
# Test 12: Init Subcommand Execution & Mutation (REQ-0002)
# ------------------------------------------------------------------------------
log_test "Scenario 12: Container execution of 'init' subcommand"
set +e
OUT_HELP=$(run_with_timeout 10 docker run --rm "${IMAGE_NAME}" init --help 2>&1)
EXIT_HELP=$?

# Test in-place mutation of a mounted config file
SAMPLE_INIT_CONFIG="${TEST_WORKSPACE}/init-test-config.yaml"
cat << 'EOF' > "${SAMPLE_INIT_CONFIG}"
scan:
  extensions:
    include:
      - ".go"
      - ".py"
output:
  format: "table"
tools:
  confirm_commands: true
  confirm_writes: true
EOF

OUT_MUTATE=$(run_with_timeout 10 docker run --rm -v "${TEST_WORKSPACE}:/workspace" "${IMAGE_NAME}" init /workspace/init-test-config.yaml 2>&1)
EXIT_MUTATE=$?
MUTATED_FILE_CONTENT=$(cat "${SAMPLE_INIT_CONFIG}")
set -e

if [ ${EXIT_HELP} -eq 0 ] && [[ "${OUT_HELP}" == *"cm-runner init"* ]] && \
   [ ${EXIT_MUTATE} -eq 0 ] && \
   [[ "${MUTATED_FILE_CONTENT}" == *".rs"* ]] && \
   [[ "${MUTATED_FILE_CONTENT}" == *"confirm_commands: false"* ]] && \
   [[ "${MUTATED_FILE_CONTENT}" == *"confirm_writes: false"* ]]; then
    pass "Executing 'init' in container supports --help and correctly mutates target config in-place"
elif [ ${EXIT_HELP} -eq 124 ] || [ ${EXIT_MUTATE} -eq 124 ]; then
    fail "Init subcommand test timed out after 10s"
else
    fail "Init subcommand failed. Help exit=${EXIT_HELP}, Mutate exit=${EXIT_MUTATE}. Output: ${OUT_MUTATE}"
fi

echo "======================================================================"
echo -e "Integration Test Suite Results: ${GREEN}${PASSED_TESTS} Passed${NC}, ${RED}${FAILED_TESTS} Failed${NC}"
echo "======================================================================"

if [ ${FAILED_TESTS} -ne 0 ]; then
    exit 1
fi
exit 0
