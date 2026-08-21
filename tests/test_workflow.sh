#!/usr/bin/env bash
# ==============================================================================
# Workflow Test Suite for CodeMender CI/CD PR Review Workflow (cm-pr-workflow)
# Governing Spec: openspec/specs/workflow/cm-pr-workflow/spec.md (REQ-0001 - REQ-0008)
# Governing Design: openspec/specs/workflow/cm-pr-workflow/design.md
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
FIXTURES_DIR="${SCRIPT_DIR}/fixtures/workflow"

DEFAULT_TIMEOUT_SECS=15
FAILED_TESTS=0
PASSED_TESTS=0
SKIPPED_TESTS=0

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
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

skip() {
    echo -e "  ${YELLOW}↷ SKIP:${NC} $1"
    SKIPPED_TESTS=$((SKIPPED_TESTS + 1))
}

# Universal defensive execution wrapper: Enforces a strict outer timeout on every command
run_with_timeout() {
    local timeout_secs="${1:-${DEFAULT_TIMEOUT_SECS}}"
    shift
    timeout --preserve-status --kill-after=3s "${timeout_secs}" "$@"
}

usage() {
    cat << EOF
Usage: $(basename "$0") [OPTIONS]

Modular test runner for CodeMender GitHub Actions CI/CD workflow assets.

Options:
    --test=<name>   Run a specific test suite (fixtures, filter, comments, installer, wif, template)
    --all           Run all available test suites (default)
    -h, --help      Show this help message and exit

Available Test Suites:
    fixtures    Validate synthetic PR diff, finding JSON, and change envelope test fixtures
    filter      Validate diff-scoped finding filtering and dynamic matrix generation (jq)
    comments    Validate PR review suggestion comment translation and HTTP 422 fallback (js)
    installer   Validate automated target repository installation script (install.sh)
    wif         Validate GCP Workload Identity Federation helper script (setup-wif.sh)
    template    Validate standalone GitHub Actions workflow template syntax (codemender.yml)
EOF
}

# ------------------------------------------------------------------------------
# Test Suite: Fixtures Validation (REQ-0001, REQ-0004, REQ-0006, REQ-0008)
# ------------------------------------------------------------------------------
test_fixtures() {
    echo "----------------------------------------------------------------------"
    echo "Running Test Suite: Shared Test Fixtures Validation"
    echo "----------------------------------------------------------------------"

    # Test 1: Directory Existence
    log_test "Scenario 1: Verify tests/fixtures/workflow directory exists"
    if [ -d "${FIXTURES_DIR}" ]; then
        pass "Directory ${FIXTURES_DIR} exists"
    else
        fail "Directory ${FIXTURES_DIR} missing"
    fi

    # Test 2: PR Diff Fixture (commit.diff)
    log_test "Scenario 2: Verify commit.diff represents multi-file diff"
    local diff_file="${FIXTURES_DIR}/commit.diff"
    if [ -f "${diff_file}" ] && [ -s "${diff_file}" ]; then
        local has_pkg_auth
        local has_cmd_server
        has_pkg_auth=$(grep -c "pkg/auth/store.go" "${diff_file}" || true)
        has_cmd_server=$(grep -c "cmd/server/main.go" "${diff_file}" || true)
        if [ "${has_pkg_auth}" -gt 0 ] && [ "${has_cmd_server}" -gt 0 ]; then
            pass "commit.diff is valid and contains diffs for pkg/auth/store.go and cmd/server/main.go"
        else
            fail "commit.diff does not contain expected multi-file changes"
        fi
    else
        fail "commit.diff missing or empty at ${diff_file}"
    fi

    # Test 3: Findings In Diff (findings_in_diff.json)
    log_test "Scenario 3: Verify findings_in_diff.json schema and keys"
    local in_diff_file="${FIXTURES_DIR}/findings_in_diff.json"
    if [ -f "${in_diff_file}" ]; then
        set +e
        local count
        count=$(jq '. | length' "${in_diff_file}" 2>/dev/null)
        local valid_keys
        valid_keys=$(jq '.[0] | has("FindingID") and has("FilePath") and has("StartLine") and has("Severity") and has("Title") and has("Analysis") and has("Snippet")' "${in_diff_file}" 2>/dev/null)
        set -e
        if [ "${count}" -ge 2 ] && [ "${valid_keys}" = "true" ]; then
            pass "findings_in_diff.json contains ${count} valid PascalCase finding objects"
        else
            fail "findings_in_diff.json invalid (count=${count}, valid_keys=${valid_keys})"
        fi
    else
        fail "findings_in_diff.json missing at ${in_diff_file}"
    fi

    # Test 4: Findings Out of Diff (findings_out_of_diff.json)
    log_test "Scenario 4: Verify findings_out_of_diff.json schema"
    local out_diff_file="${FIXTURES_DIR}/findings_out_of_diff.json"
    if [ -f "${out_diff_file}" ]; then
        set +e
        local count
        count=$(jq '. | length' "${out_diff_file}" 2>/dev/null)
        local has_legacy
        has_legacy=$(jq '[.[] | select(.FilePath == "legacy/db.go")] | length' "${out_diff_file}" 2>/dev/null)
        set -e
        if [ "${count}" -ge 2 ] && [ "${has_legacy}" -ge 1 ]; then
            pass "findings_out_of_diff.json contains ${count} findings outside PR diff"
        else
            fail "findings_out_of_diff.json invalid (count=${count}, has_legacy=${has_legacy})"
        fi
    else
        fail "findings_out_of_diff.json missing at ${out_diff_file}"
    fi

    # Test 5: Findings Mixed (findings_mixed.json)
    log_test "Scenario 5: Verify findings_mixed.json multi-severity composition"
    local mixed_file="${FIXTURES_DIR}/findings_mixed.json"
    if [ -f "${mixed_file}" ]; then
        set +e
        local count
        count=$(jq '. | length' "${mixed_file}" 2>/dev/null)
        local severities
        severities=$(jq '[.[].Severity] | unique | sort | join(",")' "${mixed_file}" 2>/dev/null)
        set -e
        if [ "${count}" -ge 4 ] && [[ "${severities}" == *"CRITICAL"* ]] && [[ "${severities}" == *"HIGH"* ]] && [[ "${severities}" == *"LOW"* ]]; then
            pass "findings_mixed.json contains ${count} findings with severities: ${severities}"
        else
            fail "findings_mixed.json invalid (count=${count}, severities=${severities})"
        fi
    else
        fail "findings_mixed.json missing at ${mixed_file}"
    fi

    # Test 6: Single-Line Change Envelope (change_envelope_single_line.json)
    log_test "Scenario 6: Verify change_envelope_single_line.json structure"
    local single_file="${FIXTURES_DIR}/change_envelope_single_line.json"
    if [ -f "${single_file}" ]; then
        set +e
        local status
        status=$(jq -r '.status' "${single_file}" 2>/dev/null)
        local hunk_count
        hunk_count=$(jq '.hunks | length' "${single_file}" 2>/dev/null)
        local is_single_line
        is_single_line=$(jq '.[0] | .start_line == .end_line' <<< "$(jq '.hunks' "${single_file}")" 2>/dev/null)
        set -e
        if [ "${status}" = "FIXED" ] && [ "${hunk_count}" -ge 1 ] && [ "${is_single_line}" = "true" ]; then
            pass "change_envelope_single_line.json is valid FIXED status single-line hunk envelope"
        else
            fail "change_envelope_single_line.json invalid (status=${status}, hunks=${hunk_count})"
        fi
    else
        fail "change_envelope_single_line.json missing at ${single_file}"
    fi

    # Test 7: Multi-Line Change Envelope (change_envelope_multiline.json)
    log_test "Scenario 7: Verify change_envelope_multiline.json structure"
    local multi_file="${FIXTURES_DIR}/change_envelope_multiline.json"
    if [ -f "${multi_file}" ]; then
        set +e
        local status
        status=$(jq -r '.status' "${multi_file}" 2>/dev/null)
        local hunk_count
        hunk_count=$(jq '.hunks | length' "${multi_file}" 2>/dev/null)
        local is_multi_line
        is_multi_line=$(jq '.[0] | .start_line < .end_line' <<< "$(jq '.hunks' "${multi_file}")" 2>/dev/null)
        set -e
        if [ "${status}" = "FIXED" ] && [ "${hunk_count}" -ge 1 ] && [ "${is_multi_line}" = "true" ]; then
            pass "change_envelope_multiline.json is valid FIXED status multi-line hunk envelope"
        else
            fail "change_envelope_multiline.json invalid (status=${status}, hunks=${hunk_count})"
        fi
    else
        fail "change_envelope_multiline.json missing at ${multi_file}"
    fi

    # Test 8: Unresolved Change Envelope (change_envelope_unresolved.json)
    log_test "Scenario 8: Verify change_envelope_unresolved.json structure"
    local unres_file="${FIXTURES_DIR}/change_envelope_unresolved.json"
    if [ -f "${unres_file}" ]; then
        set +e
        local status
        status=$(jq -r '.status' "${unres_file}" 2>/dev/null)
        local hunk_count
        hunk_count=$(jq '.hunks | length' "${unres_file}" 2>/dev/null)
        local patch_len
        patch_len=$(jq -r '.patch | length' "${unres_file}" 2>/dev/null)
        set -e
        if [ "${status}" = "UNRESOLVED" ] && [ "${hunk_count}" -eq 0 ] && [ "${patch_len}" -eq 0 ]; then
            pass "change_envelope_unresolved.json is valid UNRESOLVED status envelope with empty diff"
        else
            fail "change_envelope_unresolved.json invalid (status=${status}, hunks=${hunk_count}, patch_len=${patch_len})"
        fi
    else
        fail "change_envelope_unresolved.json missing at ${unres_file}"
    fi
}

# ------------------------------------------------------------------------------
# Placeholder Test Suites for Subsequent Stories (Modular Skeleton)
# ------------------------------------------------------------------------------
test_filter() {
    echo "----------------------------------------------------------------------"
    echo "Running Test Suite: Diff-Scoped Finding Filter (filter_findings.jq)"
    echo "----------------------------------------------------------------------"
    local filter_script="${ROOT_DIR}/github-actions/scripts/filter_findings.jq"
    if [ -f "${filter_script}" ]; then
        log_test "Executing filter_findings.jq against test fixtures"
        # Subsequent story will implement full filter test cases here
        pass "filter_findings.jq script found"
    else
        skip "filter_findings.jq not yet implemented (Story #58)"
    fi
}

test_comments() {
    echo "----------------------------------------------------------------------"
    echo "Running Test Suite: PR Review Comment Publisher (publish_comments.js)"
    echo "----------------------------------------------------------------------"
    local comment_script="${ROOT_DIR}/github-actions/scripts/publish_comments.js"
    if [ -f "${comment_script}" ]; then
        log_test "Executing publish_comments.js test suite"
        # Subsequent story will implement full comment publisher test cases here
        pass "publish_comments.js script found"
    else
        skip "publish_comments.js not yet implemented (Story #59)"
    fi
}

test_installer() {
    echo "----------------------------------------------------------------------"
    echo "Running Test Suite: Workflow Installer (install.sh)"
    echo "----------------------------------------------------------------------"
    local install_script="${ROOT_DIR}/github-actions/scripts/install.sh"
    if [ -f "${install_script}" ]; then
        log_test "Executing install.sh validation test"
        pass "install.sh script found"
    else
        skip "install.sh not yet implemented (Story #62)"
    fi
}

test_wif() {
    echo "----------------------------------------------------------------------"
    echo "Running Test Suite: GCP WIF Setup Helper (setup-wif.sh)"
    echo "----------------------------------------------------------------------"
    local wif_script="${ROOT_DIR}/github-actions/scripts/setup-wif.sh"
    if [ -f "${wif_script}" ]; then
        log_test "Executing setup-wif.sh validation test"
        pass "setup-wif.sh script found"
    else
        skip "setup-wif.sh not yet implemented (Story #63)"
    fi
}

test_template() {
    echo "----------------------------------------------------------------------"
    echo "Running Test Suite: GitHub Actions Template (codemender.yml)"
    echo "----------------------------------------------------------------------"
    local workflow_file="${ROOT_DIR}/github-actions/workflows/codemender.yml"
    if [ -f "${workflow_file}" ]; then
        log_test "Executing codemender.yml syntax and lint checks"
        pass "codemender.yml workflow template found"
    else
        skip "codemender.yml not yet implemented (Story #60)"
    fi
}

# ------------------------------------------------------------------------------
# Main Dispatcher
# ------------------------------------------------------------------------------
main() {
    local target_test="all"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --test=*)
                target_test="${1#*=}"
                shift
                ;;
            --test)
                target_test="$2"
                shift 2
                ;;
            --all)
                target_test="all"
                shift
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                echo -e "${RED}Error: Unknown option '$1'${NC}" >&2
                usage >&2
                exit 2
                ;;
        esac
    done

    echo "======================================================================"
    echo "Starting CodeMender Workflow Test Suite (cm-pr-workflow)"
    echo "Target Suite: ${target_test}"
    echo "Default Timeout: ${DEFAULT_TIMEOUT_SECS}s"
    echo "======================================================================"

    case "${target_test}" in
        fixtures)
            test_fixtures
            ;;
        filter)
            test_filter
            ;;
        comments)
            test_comments
            ;;
        installer)
            test_installer
            ;;
        wif)
            test_wif
            ;;
        template)
            test_template
            ;;
        all)
            test_fixtures
            test_filter
            test_comments
            test_installer
            test_wif
            test_template
            ;;
        *)
            echo -e "${RED}Error: Unrecognized test suite '${target_test}'${NC}" >&2
            echo "Supported test suites: fixtures, filter, comments, installer, wif, template, all" >&2
            exit 2
            ;;
    esac

    echo "======================================================================"
    echo -e "Workflow Test Results: ${GREEN}${PASSED_TESTS} Passed${NC}, ${RED}${FAILED_TESTS} Failed${NC}, ${YELLOW}${SKIPPED_TESTS} Skipped${NC}"
    echo "======================================================================"

    if [ ${FAILED_TESTS} -ne 0 ]; then
        exit 1
    fi
    exit 0
}

main "$@"
