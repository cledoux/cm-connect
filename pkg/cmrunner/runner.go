package cmrunner

import (
	"context"
	"encoding/json"
	"errors"
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
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0001, REQ-0005, REQ-0010, REQ-0013
type Runner struct {
	Executable string
	Workspace  string
	Env        []string
}

// NewRunner instantiates a Runner with sensible defaults and functional options.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0001
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

// Run executes a Command implementation using the configured Runner environment.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0001, REQ-0005, REQ-0010, REQ-0013
func (r *Runner) Run(
	ctx context.Context,
	cmd Command,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	if cmd == nil {
		return ExitUsage, errors.New("cmrunner: command cannot be nil")
	}
	return cmd.Execute(ctx, r, stdin, stdout, stderr)
}

// RunFind executes a FindCommand via Run for backwards compatibility.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0005
func (r *Runner) RunFind(
	ctx context.Context,
	cmd *FindCommand,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	if cmd == nil {
		cmd = NewFindCommand(".")
	}
	return r.Run(ctx, cmd, stdin, stdout, stderr)
}

// RunSubprocess executes a single command in an isolated process group with signal forwarding.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0010, REQ-0013
func (r *Runner) RunSubprocess(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	return r.runSubprocess(ctx, args, stdin, stdout, stderr)
}

func (r *Runner) runSubprocess(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	execCmd := exec.CommandContext(ctx, r.Executable, args...)
	execCmd.Dir = r.Workspace
	execCmd.Env = r.Env
	execCmd.Stdin = stdin
	execCmd.Stdout = stdout
	execCmd.Stderr = stderr
	execCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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

// evaluateReportExitCode inspects the report payload to return ExitClean (0) or ExitFindings (1).
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0013
func evaluateReportExitCode(format string, data []byte) int {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return ExitClean
	}

	formatLower := strings.ToLower(format)
	if formatLower == "sarif" {
		var sarifDoc struct {
			Runs []struct {
				Results []any `json:"results"`
			} `json:"runs"`
		}
		if err := json.Unmarshal(data, &sarifDoc); err == nil {
			for _, run := range sarifDoc.Runs {
				if len(run.Results) > 0 {
					return ExitFindings
				}
			}
			return ExitClean
		}
	}

	// Default JSON array parsing
	var findings []any
	if err := json.Unmarshal(data, &findings); err == nil {
		if len(findings) > 0 {
			return ExitFindings
		}
		return ExitClean
	}

	// Fallback for text/table/markdown
	if len(trimmed) > 0 {
		return ExitFindings
	}
	return ExitClean
}
