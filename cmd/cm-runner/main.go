package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"cm-connect/pkg/cmrunner"
)

// isTerminal returns true if the given reader is an interactive character device terminal (TTY).
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0009
func isTerminal(r io.Reader) bool {
	if f, ok := r.(*os.File); ok {
		var t syscall.Termios
		_, _, err := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&t)))
		return err == 0
	}
	return false
}

// printUsage emits the standard usage reference guide to the given writer.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0003
func printUsage(w io.Writer) {
	usage := `CodeMender Runner (cm-runner) - Headless Container Entrypoint

Usage:
  cm-runner find [path] [-- [flags]]     Run CodeMender vulnerability scan on full repo or sub-path
  cm-runner shell                        Launch interactive /bin/bash shell in /workspace (requires -it)

Arguments:
  [path]               Scans repository at /workspace (default: '.') or scoped sub-path.
  [-- [flags...]]      Optional flags forwarded directly to CodeMender CLI.
                       Defaults to '--format json' on stdout unless overridden (-f, --format, --help).
                       Diagnostics and progress logs are routed to stderr.

Exit Codes:
  0    Scan completed successfully with 0 findings
  1    Scan completed with findings detected
  2    CLI usage error, missing subcommand, invalid target path, or missing TTY on shell
  >2   Fatal tooling, execution, or authentication error
`
	fmt.Fprint(w, usage)
}

// run parses arguments and orchestrates execution of the target subcommand.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0001, REQ-0003, REQ-0005, REQ-0009
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, workspaceDir, cmPath string) int {
	cleanArgs := stripCMPrefix(args)
	if len(cleanArgs) == 0 {
		fmt.Fprintf(stderr, "Error: %v\n", errMissingSubcommand)
		printUsage(stderr)
		return cmrunner.ExitUsage
	}

	subcommand := cleanArgs[0]
	if subcommand == "shell" {
		if !isTerminal(stdin) {
			fmt.Fprintln(stderr, "Error: 'shell' subcommand requires an interactive terminal. Please run with 'docker run -it <image> shell'")
			return cmrunner.ExitUsage
		}

		cmd := exec.Command("/bin/bash")
		cmd.Dir = workspaceDir
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return exitErr.ExitCode()
			}
			return cmrunner.ExitError
		}
		return cmrunner.ExitClean
	}

	cmd, err := parseArgs(workspaceDir, cleanArgs)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		printUsage(stderr)
		return cmrunner.ExitUsage
	}

	runner := cmrunner.NewRunner(
		cmrunner.WithExecutable(cmPath),
		cmrunner.WithWorkspace(workspaceDir),
	)

	ctx := context.Background()
	code, _ := runner.RunFind(ctx, cmd, stdin, stdout, stderr)
	return code
}

func main() {
	code := run(
		os.Args[1:],
		os.Stdin,
		os.Stdout,
		os.Stderr,
		cmrunner.DefaultWorkspace,
		cmrunner.DefaultExecutable,
	)
	os.Exit(code)
}
