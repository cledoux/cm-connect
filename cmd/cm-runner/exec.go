package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// ExecuteProcess starts a command with process group isolation (Setpgid: true),
// forwards OS signals (SIGINT, SIGTERM) to the process group, streams stdout/stderr,
// and returns the exit status code of the child process.
func ExecuteProcess(
	ctx context.Context,
	executable string,
	args []string,
	env []string,
	dir string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "Error: failed to execute %s: %v\n", executable, err)
		return ExitError, err
	}

	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case sig := <-sigChan:
				if cmd.Process != nil && cmd.Process.Pid > 0 {
					if sysSig, ok := sig.(syscall.Signal); ok {
						_ = syscall.Kill(-cmd.Process.Pid, sysSig)
					}
				}
			case <-done:
				return
			}
		}
	}()

	waitErr := cmd.Wait()
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
