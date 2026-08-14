package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"golang.org/x/term"
)

// IsTerminalFunc is a function type for determining if a file descriptor is a terminal.
type IsTerminalFunc func(fd int) bool

// DefaultIsTerminal checks whether fd is attached to a terminal using golang.org/x/term.
func DefaultIsTerminal(fd int) bool {
	return term.IsTerminal(fd)
}

// CommandRunner is a function type for executing a command in foreground without setpgid.
type CommandRunner func(ctx context.Context, executable string, env []string, dir string, stdin io.Reader, stdout, stderr io.Writer) error

// DefaultCommandRunner runs an interactive process in the foreground.
func DefaultCommandRunner(ctx context.Context, executable string, env []string, dir string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, executable)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// ExecuteShell checks for an interactive terminal on stdin. If missing, it emits
// the required error message to stderr and returns exit code 2. If present, it
// launches bash in the workspace directory in the foreground process group.
func ExecuteShell(
	ctx context.Context,
	bashPath string,
	workspaceRoot string,
	stdin *os.File,
	stdout io.Writer,
	stderr io.Writer,
	env []string,
	isTerm IsTerminalFunc,
	runCmd CommandRunner,
) int {
	if isTerm == nil {
		isTerm = DefaultIsTerminal
	}

	if stdin == nil || !isTerm(int(stdin.Fd())) {
		fmt.Fprintln(stderr, "Error: 'shell' subcommand requires an interactive terminal. Please run with 'docker run -it <image> shell'")
		return ExitUsage
	}

	if runCmd == nil {
		runCmd = DefaultCommandRunner
	}

	if err := runCmd(ctx, bashPath, env, workspaceRoot, stdin, stdout, stderr); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(stderr, "Error: failed to execute %s: %v\n", bashPath, err)
		return ExitError
	}
	return ExitClean
}
