package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"cm-connect/pkg/cmconfig"
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

// isTerminalFn is the function used to detect TTY (pluggable for testing).
var isTerminalFn = isTerminal

// execShellFn is the function used to exec the shell process (pluggable for testing).
var execShellFn = syscall.Exec

// printUsage emits the standard usage reference guide to the given writer.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0002, REQ-0003
func printUsage(w io.Writer) {
	usage := `CodeMender Runner (cm-runner) - Headless Container Entrypoint

Usage:
  cm-runner find [path] [-- [flags]]     Run CodeMender vulnerability scan on full repo or sub-path
  cm-runner shell [path]                 Launch interactive /bin/bash shell in /workspace (requires -it)
  cm-runner init                         Pre-seed and apply headless configuration defaults in-place

Arguments:
  [path]               For find/shell: Scans repository at /workspace (default: '.') or scoped sub-path.
  [-- [flags...]]      Optional flags forwarded directly to CodeMender CLI.
                       Emits structured JSON findings on stdout.
                       Diagnostics and progress logs are routed to stderr.

Exit Codes:
  0    Scan completed successfully with 0 findings, or init/help completed
  1    Scan completed with findings detected
  2    CLI usage error, missing subcommand, invalid target path, or missing TTY on shell
  >2   Fatal tooling, execution, or authentication error
`
	fmt.Fprint(w, usage)
}

// run parses arguments and orchestrates execution of the target subcommand.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0001, REQ-0002, REQ-0003, REQ-0005, REQ-0008, REQ-0009
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, workspaceDir, cmPath string) int {
	plan, err := parseArgs(workspaceDir, args)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		printUsage(stderr)
		return cmrunner.ExitUsage
	}

	switch plan.Action {
	case ActionHelp:
		printUsage(stdout)
		return cmrunner.ExitClean

	case ActionInit:
		configPath, err := cmconfig.DefaultConfigPath()
		if err != nil {
			fmt.Fprintf(stderr, "Error: failed to determine default config path: %v\n", err)
			return cmrunner.ExitError
		}
		if err := cmconfig.MutateConfigFile(configPath); err != nil {
			fmt.Fprintf(stderr, "Error: failed to initialize configuration: %v\n", err)
			return cmrunner.ExitError
		}
		return cmrunner.ExitClean

	case ActionShell:
		if !isTerminalFn(stdin) {
			fmt.Fprintln(stderr, "Error: 'shell' subcommand requires an interactive terminal. Please run with 'docker run -it <image> shell'")
			return cmrunner.ExitUsage
		}

		if err := os.Chdir(plan.TargetDir); err != nil {
			fmt.Fprintf(stderr, "Error: failed to change directory to %s: %v\n", plan.TargetDir, err)
			return cmrunner.ExitError
		}

		bashPath, err := exec.LookPath("bash")
		if err != nil {
			bashPath = "/bin/bash"
		}

		if err := execShellFn(bashPath, []string{"bash"}, os.Environ()); err != nil {
			fmt.Fprintf(stderr, "Error: failed to execute interactive shell %s: %v\n", bashPath, err)
			return cmrunner.ExitError
		}
		return cmrunner.ExitClean

	case ActionRunSequence:
		runner := cmrunner.NewRunner(
			cmrunner.WithExecutable(cmPath),
			cmrunner.WithWorkspace(workspaceDir),
		)

		ctx := context.Background()
		code, _ := runner.RunSequence(ctx, plan.Commands, stdin, stdout, stderr)
		return code

	default:
		return cmrunner.ExitUsage
	}
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
