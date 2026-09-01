# Proposal: CodeMender In-Band Token Telemetry and Host-Mapped Results Directory Protocol

**Change ID:** `cm-token-reporting` \
**Status:** In Review \
**Author:** Charles LeDoux \
**Target Specs:**
- `openspec/specs/runner/cm-batch-runner/spec.md`
- `openspec/specs/runner/cm-fix-runner/spec.md`
- `openspec/specs/workflow/cm-pr-workflow/spec.md` \
**Governing ADRs:**
- `adrs/ADR-0001.md`
- `adrs/ADR-0002.md`
- `adrs/ADR-0005.md`
- `adrs/ADR-0009.md` \
**Bug/Epic:** N/A

## Why

Automated CI/CD gating workflows (e.g. GitHub Actions PR review bots) and developers running CodeMender (`cm`) commands locally lack direct, actionable visibility into LLM token consumption, prompt cache hit rates, thinking ratios, and execution durations. Furthermore, upcoming multi-run benchmarking and analytical reporting tools require a standardized host-mapped results directory to aggregate structured run artifacts without cluttering primary data streams.

By pairing in-band token reporting directly inside the primary JSON payloads (`stdout`) with a dedicated host-mapped results directory for detailed analysis artifacts, we provide immediate telemetry to developers and CI bots while establishing a durable foundation for future reporting commands.

## What Changes

- **In-Band JSON Token Telemetry**:
  - For `find` and `find-diff`: Transition the top-level `stdout` payload from a raw findings array `[...]` to a structured **Scan Envelope** object: `{"findings": [...], "tokens": {...}}`.
  - For `fix`: Enrich the existing `ChangeEnvelope` JSON object with a dedicated `"tokens"` block.
- **Diagnostic Console Telemetry (`stderr`)**:
  - At the completion of `find`, `find-diff`, and `fix`, extract the session ID from execution output or `~/.codemender/state.db`, query `/usr/local/bin/cm stats --session <id> --json`, and format a human-readable telemetry summary banner to `stderr` with metric notation (e.g. `78.4k`, `1.2M`, cache hit %).
- **Host-Mapped Results & Artifacts Directory Protocol**:
  - Add deterministic results directory resolution across `--out-dir` / `--results-dir` CLI flags, `CM_OUTPUT_DIR` / `CM_RESULTS_DIR` environment variables, standard container volume mount `/results`, and fallback to `<workspace>/.codemender-out`.
  - Reserve the results directory for detailed run artifacts (raw session logs, SQLite state snapshots, CSV ledgers, multi-run benchmarks).
- **Downstream CI Backward Compatibility & PR Review Integration**:
  - Update `github-actions/scripts/filter_findings.jq` to transparently accept either the new Scan Envelope object (`.findings`) or legacy raw arrays.
  - Update `github-actions/scripts/publish_comments.py` in `--mode=summary` to extract token telemetry directly from the scan payload and publish an executive **Token Ledger** table in PR comments and `$GITHUB_STEP_SUMMARY`.

## Capabilities (The Core Contract)

### New Capabilities

- None.

### Modified Capabilities

- `cm-batch-runner`: Extends `find` and `find-diff` to synthesize top-level Scan Envelope JSON with in-band token telemetry, resolves the host-mapped results directory, and prints a diagnostic token summary banner to `stderr`. Maps to `openspec/specs/runner/cm-batch-runner/spec.md`.
- `cm-fix-runner`: Extends `fix` to enrich `ChangeEnvelope` with a `"tokens"` telemetry block, resolves the host-mapped results directory, and prints a diagnostic token summary banner to `stderr`. Maps to `openspec/specs/runner/cm-fix-runner/spec.md`.
- `cm-pr-workflow`: Extends `filter_findings.jq` to accept Scan Envelope objects and updates `publish_comments.py` to format and publish token metrics in PR summary comments and step summaries. Maps to `openspec/specs/workflow/cm-pr-workflow/spec.md`.

## Impact

- **Affected Packages**: `cmd/cm-runner`, `pkg/cmrunner`, new `pkg/cmtelemetry`, `github-actions/scripts/filter_findings.jq`, `github-actions/scripts/publish_comments.py`.
- **Breaking Changes**: For `find` and `find-diff`, `stdout` output transitions from a JSON array to a JSON object with `"findings"` and `"tokens"` fields. Downstream tools in `github-actions/` are updated to support both representations transparently.
