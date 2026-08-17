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

// Runner is responsible for executing CodeMender commands in isolated process groups.
type Runner struct {
	Executable string
	Workspace  string
	Env        []string
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

	execCmd := exec.CommandContext(ctx, r.Executable, args...)
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
