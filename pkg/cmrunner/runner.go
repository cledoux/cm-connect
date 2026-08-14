package cmrunner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
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

// RunFind executes a FindCommand in an isolated process group with signal forwarding.
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

	execCmd := exec.CommandContext(ctx, r.Executable, cmd.Args()...)
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
