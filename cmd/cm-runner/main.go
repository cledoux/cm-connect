package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"cm-connect/pkg/cmrunner"
)

// printUsage emits the standard usage reference guide to the given writer.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0003
func printUsage(w io.Writer) {
	usage := `CodeMender Runner (cm-runner) - Headless Container Entrypoint

Usage:
  cm-runner find [path] [-- [flags]]     Run CodeMender vulnerability scan on full repo or sub-path

Arguments:
  [path]               Scans repository at /workspace (default: '.') or scoped sub-path.
  [-- [flags...]]      Optional flags forwarded to CodeMender CLI.
                       Outputs structured machine-readable findings ('--format=json' by default) directly on stdout.
                       All scanning diagnostics, progress spinners, and logs are routed to stderr.

Exit Codes:
  0    Scan completed successfully with 0 findings
  1    Scan completed with findings detected
  2    CLI usage error, missing subcommand, or invalid target path
  >2   Fatal tooling, execution, or authentication error
`
	fmt.Fprint(w, usage)
}

// run parses arguments and orchestrates execution of the target command via Runner.Run.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0001, REQ-0003, REQ-0005
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, workspaceDir, cmPath string) int {
	cmd, err := parseArgs(workspaceDir, args)
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
	code, _ := runner.Run(ctx, cmd, stdin, stdout, stderr)
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
