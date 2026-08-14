package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"cm-connect/pkg/cmrunner"
)

func printUsage(w io.Writer) {
	usage := `CodeMender Runner (cm-runner) - Headless Container Entrypoint

Usage:
  cm-runner find [path] [-- [flags]]     Run CodeMender vulnerability scan on full repo or sub-path

Arguments:
  [path]               Scans repository at /workspace (default: '.') or scoped sub-path.
  [-- [flags...]]      Optional flags forwarded directly to CodeMender CLI.
                       Defaults to '--format json' on stdout unless overridden (-f, --format, --help).
                       Diagnostics and progress logs are routed to stderr.

Exit Codes:
  0    Scan completed successfully with 0 findings
  1    Scan completed with findings detected
  2    CLI usage error, missing subcommand, or invalid target path
  >2   Fatal tooling, execution, or authentication error
`
	fmt.Fprint(w, usage)
}

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
