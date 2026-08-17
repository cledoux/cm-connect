#!/usr/bin/env bash
set -euo pipefail

# scripts/check_coverage.sh: Runs Go unit tests, generates a coverage profile,
# and enforces a minimum total statement coverage threshold (default: 90.0%).

THRESHOLD="${1:-90.0}"
COVERAGE_FILE="coverage.out"
TIMEOUT="${TIMEOUT:-60s}"

echo "============================================================"
echo " Running Go Unit Tests & Race Detector (timeout: ${TIMEOUT}) "
echo "============================================================"

# Enforce defensive timeout on test execution
timeout "${TIMEOUT}" go test -v -race -coverprofile="${COVERAGE_FILE}" -covermode=atomic -timeout="${TIMEOUT}" ./...

if [ ! -f "${COVERAGE_FILE}" ]; then
  echo "Error: Coverage file ${COVERAGE_FILE} was not generated." >&2
  exit 1
fi

echo ""
echo "============================================================"
echo " Evaluating Test Coverage against Threshold (>= ${THRESHOLD}%) "
echo "============================================================"

# Print per-function coverage report
go tool cover -func="${COVERAGE_FILE}"

# Extract total statement coverage percentage
TOTAL_COV=$(go tool cover -func="${COVERAGE_FILE}" | awk '/^total:/ {print $3}' | tr -d '%')

if [ -z "${TOTAL_COV}" ]; then
  echo "Error: Failed to parse total coverage from ${COVERAGE_FILE}" >&2
  exit 1
fi

# Compare float coverage against threshold using awk
PASS=$(awk -v cov="${TOTAL_COV}" -v thresh="${THRESHOLD}" 'BEGIN { if (cov + 0 >= thresh + 0) print "true"; else print "false" }')

# Append to GITHUB_STEP_SUMMARY if running in GitHub Actions
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    echo "### 📊 Go Test & Coverage Summary"
    echo ""
    echo "| Metric | Value |"
    echo "| :--- | :--- |"
    echo "| **Total Statement Coverage** | \`${TOTAL_COV}%\` |"
    echo "| **Required Minimum** | \`${THRESHOLD}%\` |"
    if [ "${PASS}" = "true" ]; then
      echo "| **Status** | ✅ **PASSED** |"
    else
      echo "| **Status** | ❌ **FAILED** (Below ${THRESHOLD}%) |"
    fi
    echo ""
    echo "<details><summary>Detailed Coverage Breakdown</summary>"
    echo ""
    echo '```'
    go tool cover -func="${COVERAGE_FILE}"
    echo '```'
    echo "</details>"
  } >> "${GITHUB_STEP_SUMMARY}"
fi

echo ""
if [ "${PASS}" = "true" ]; then
  echo "✅ SUCCESS: Total statement coverage is ${TOTAL_COV}% (>= ${THRESHOLD}% threshold)."
  exit 0
else
  echo "❌ FAILURE: Total statement coverage is ${TOTAL_COV}%, which is below the required ${THRESHOLD}% threshold." >&2
  exit 1
fi
