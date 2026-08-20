package cmrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"cm-connect/pkg/cmfinding"
	"cm-connect/pkg/cmpatch"
)

const (
	// ExitClean indicates a successful scan with 0 findings.
	ExitClean = 0
	// ExitFindings indicates a successful scan where findings were detected.
	ExitFindings = 1
	// ExitUsage indicates invalid CLI usage, bad arguments, or non-existent paths.
	ExitUsage = 2
	// ExitError indicates fatal execution, tooling, or authentication errors.
	ExitError = 3

	DefaultExecutable = "/usr/local/bin/cm"
	DefaultWorkspace  = "/workspace"
)

// Command represents an executable command consumed by Runner.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0005, REQ-0006
type Command interface {
	Cmd() []string
}

// Option configures a Runner instance.
type Option func(*Runner)

// WithExecutable sets the path to the CodeMender binary.
func WithExecutable(executable string) Option {
	return func(r *Runner) {
		if executable != "" {
			r.Executable = executable
		}
	}
}

// WithWorkspace sets the working directory for command execution.
func WithWorkspace(workspace string) Option {
	return func(r *Runner) {
		if workspace != "" {
			r.Workspace = workspace
		}
	}
}

// WithEnv sets the environment variables for execution.
func WithEnv(env []string) Option {
	return func(r *Runner) {
		r.Env = env
	}
}

// WithGlobalFlags sets root/global CLI flags (e.g. --sandbox=false) prepended to all command executions.
func WithGlobalFlags(flags ...string) Option {
	return func(r *Runner) {
		r.GlobalFlags = flags
	}
}

// Runner is responsible for executing CodeMender commands in isolated process groups.
type Runner struct {
	Executable  string
	Workspace   string
	Env         []string
	GlobalFlags []string
}

// NewRunner instantiates a Runner with sensible defaults and functional options.
func NewRunner(opts ...Option) *Runner {
	r := &Runner{
		Executable: resolveExecutable(DefaultExecutable, "cm"),
		Workspace:  DefaultWorkspace,
		Env:        os.Environ(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func resolveExecutable(preferredPath, fallbackName string) string {
	if _, err := os.Stat(preferredPath); err == nil {
		return preferredPath
	}
	if path, err := exec.LookPath(fallbackName); err == nil {
		return path
	}
	return preferredPath
}

// Run executes a single Command in an isolated process group with signal forwarding.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0005, REQ-0010, REQ-0012
func (r *Runner) Run(
	ctx context.Context,
	cmd Command,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	if cmd == nil {
		return ExitUsage, fmt.Errorf("command cannot be nil")
	}
	return r.execSubprocess(ctx, cmd.Cmd(), stdin, stdout, stderr)
}

// RunSequence executes a sequence of Commands in order.
// Intermediate scan commands have their output routed to stderr to keep stdout clean.
// When a ReportCommand runs, its structured output is emitted to stdout and evaluated for findings.
// If any command fails (non-zero exit code or error), the sequence terminates immediately.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0005, REQ-0006, REQ-0007, REQ-0010, REQ-0012, REQ-0013
func (r *Runner) RunSequence(
	ctx context.Context,
	cmds []Command,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	if len(cmds) == 0 {
		return ExitClean, nil
	}

	for i, cmd := range cmds {
		isLast := (i == len(cmds)-1)
		_, isReport := cmd.(*ReportCommand)

		var cmdStdout io.Writer
		var reportBuf *bytes.Buffer

		if isReport {
			reportBuf = &bytes.Buffer{}
			if stdout != nil {
				cmdStdout = io.MultiWriter(stdout, reportBuf)
			} else {
				cmdStdout = reportBuf
			}
		} else if isLast {
			cmdStdout = stdout
		} else {
			// Intermediate command (e.g. find scan phase): route stdout to stderr
			// so progress spinners and logs do not pollute the data stream.
			cmdStdout = stderr
		}

		code, err := r.execSubprocess(ctx, cmd.Cmd(), stdin, cmdStdout, stderr)
		if err != nil || code != ExitClean {
			return code, err
		}

		if isReport && reportBuf != nil {
			evalCode := EvaluateReportExitCode(reportBuf.String())
			return evalCode, nil
		}
	}

	return ExitClean, nil
}

// execSubprocess handles process execution with process group isolation and signal forwarding.
func (r *Runner) execSubprocess(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	if len(args) == 0 {
		return ExitUsage, fmt.Errorf("empty command arguments")
	}

	var finalArgs []string
	finalArgs = append(finalArgs, r.GlobalFlags...)
	finalArgs = append(finalArgs, args...)

	execCmd := exec.CommandContext(ctx, r.Executable, finalArgs...)
	execCmd.Dir = r.Workspace
	execCmd.Env = r.Env
	execCmd.Stdin = stdin
	execCmd.Stdout = stdout
	execCmd.Stderr = stderr
	execCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	execCmd.Cancel = func() error {
		if execCmd.Process != nil && execCmd.Process.Pid > 0 {
			return syscall.Kill(-execCmd.Process.Pid, syscall.SIGTERM)
		}
		return nil
	}

	if err := execCmd.Start(); err != nil {
		fmt.Fprintf(stderr, "Error: failed to execute %s: %v\n", r.Executable, err)
		return ExitError, err
	}

	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case sig := <-sigChan:
				if execCmd.Process != nil && execCmd.Process.Pid > 0 {
					if sysSig, ok := sig.(syscall.Signal); ok {
						_ = syscall.Kill(-execCmd.Process.Pid, sysSig)
					}
				}
			case <-ctx.Done():
				if execCmd.Process != nil && execCmd.Process.Pid > 0 {
					_ = syscall.Kill(-execCmd.Process.Pid, syscall.SIGTERM)
				}
				return
			case <-done:
				return
			}
		}
	}()

	waitErr := execCmd.Wait()
	signal.Stop(sigChan)
	close(done)

	if waitErr == nil {
		return ExitClean, nil
	}

	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		return exitErr.ExitCode(), waitErr
	}

	return ExitError, waitErr
}

// EvaluateReportExitCode inspects a report output string (JSON or SARIF) to determine findings status.
// Returns ExitFindings (1) if findings >= 1, or ExitClean (0) if 0 findings.
func EvaluateReportExitCode(reportOutput string) int {
	trimmed := strings.TrimSpace(reportOutput)
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return ExitClean
	}

	// Case 1: JSON array of findings
	if strings.HasPrefix(trimmed, "[") {
		var list []any
		if err := json.Unmarshal([]byte(trimmed), &list); err == nil {
			if len(list) > 0 {
				return ExitFindings
			}
			return ExitClean
		}
	}

	// Case 2: SARIF v2.1.0 document
	if strings.HasPrefix(trimmed, "{") {
		var sarifDoc struct {
			Runs []struct {
				Results []any `json:"results"`
			} `json:"runs"`
		}
		if err := json.Unmarshal([]byte(trimmed), &sarifDoc); err == nil {
			totalResults := 0
			for _, run := range sarifDoc.Runs {
				totalResults += len(run.Results)
			}
			if totalResults > 0 {
				return ExitFindings
			}
			return ExitClean
		}
	}

	return ExitClean
}

// RunFixPipeline executes the 5-stage stateless remediation workflow.
// Governing: ADR-0002, ADR-0003, ADR-0005, SPEC-cm-fix-runner, REQ-0003, REQ-0004, REQ-0005, REQ-0006, REQ-0008, REQ-0009, REQ-0010
func (r *Runner) RunFixPipeline(
	ctx context.Context,
	rawFindingJSON []byte,
	passthroughFlags []string,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	if stderr == nil {
		stderr = io.Discard
	}
	if stdout == nil {
		stdout = io.Discard
	}

	// Stage 1: Ingestion & Schema Normalization (pkg/cmfinding)
	importBytes, imported, err := cmfinding.ImportFinding(rawFindingJSON, r.Workspace)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to normalize finding JSON: %v\n", err)
		return ExitUsage, err
	}

	// Write to temporary file for cm report import
	tmpFile, err := os.CreateTemp("", "cm-import-*.json")
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to create temporary import file: %v\n", err)
		return ExitError, err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(importBytes); err != nil {
		tmpFile.Close()
		fmt.Fprintf(stderr, "Error: failed to write to temporary import file: %v\n", err)
		return ExitError, err
	}
	tmpFile.Close()

	// Stage 2: State Seeding via cm report import (Subprocess)
	importCmd := NewImportCommand(tmpFile.Name(), r.Workspace)
	code, err := r.execSubprocess(ctx, importCmd.Cmd(), nil, stderr, stderr)
	if err != nil || code != ExitClean {
		if code == ExitClean {
			code = ExitError
		}
		return code, fmt.Errorf("failed to import finding into ephemeral state: %w", err)
	}

	// Stage 3: Finding ID Resolution via cm report --format=json (Subprocess)
	reportCmd := NewReportCommand("json")
	var reportBuf bytes.Buffer
	code, err = r.execSubprocess(ctx, reportCmd.Cmd(), nil, &reportBuf, stderr)
	if err != nil || code != ExitClean {
		if code == ExitClean {
			code = ExitError
		}
		return code, fmt.Errorf("failed to query ephemeral findings from cm report: %w", err)
	}

	findingID, err := extractFindingID(reportBuf.Bytes())
	if err != nil || findingID == "" {
		if err != nil {
			return ExitError, fmt.Errorf("failed to resolve FindingID from cm report: %w", err)
		}
		findingID = "unknown"
	}

	// Stage 4: Fix Execution via cm fix <FindingID> -y --unrestricted [passthrough flags]
	fixCmd := NewFixCommand(findingID, passthroughFlags...)
	_, _ = r.execSubprocess(ctx, fixCmd.Cmd(), nil, stderr, stderr)

	// Stage 5: Patch Extraction & Change Envelope Synthesis (pkg/cmpatch)
	diffStr, err := cmpatch.ExtractPatch(ctx, r.Workspace, "/workspace-ro")
	if err != nil {
		fmt.Fprintf(stderr, "Warning: error extracting patch: %v\n", err)
	}

	envelope, err := cmpatch.SynthesizeEnvelope(findingID, imported.VulnType, imported.Title, imported.Message, diffStr)
	if err != nil {
		return ExitError, fmt.Errorf("failed to synthesize change envelope: %w", err)
	}

	envBytes, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return ExitError, fmt.Errorf("failed to marshal change envelope JSON: %w", err)
	}

	fmt.Fprintln(stdout, string(envBytes))

	if envelope.Status == "FIXED" {
		return ExitClean, nil
	}
	return ExitFindings, nil
}

func extractFindingID(reportJSON []byte) (string, error) {
	trimmed := bytes.TrimSpace(reportJSON)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("empty report JSON output")
	}

	type findingIDHolder struct {
		FindingID    string `json:"FindingID"`
		FindingIDAlt string `json:"finding_id"`
		ID           string `json:"ID"`
		IDAlt        string `json:"id"`
	}

	getID := func(f findingIDHolder) string {
		if f.FindingID != "" {
			return f.FindingID
		}
		if f.FindingIDAlt != "" {
			return f.FindingIDAlt
		}
		if f.ID != "" {
			return f.ID
		}
		return f.IDAlt
	}

	if trimmed[0] == '[' {
		var list []findingIDHolder
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return "", err
		}
		if len(list) > 0 {
			return getID(list[0]), nil
		}
		return "", fmt.Errorf("no findings returned by report")
	} else if trimmed[0] == '{' {
		var single findingIDHolder
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return "", err
		}
		return getID(single), nil
	}

	return "", fmt.Errorf("invalid report JSON format")
}
