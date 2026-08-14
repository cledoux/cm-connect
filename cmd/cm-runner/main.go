package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

const (
	ExitClean    = 0
	ExitFindings = 1
	ExitUsage    = 2
	ExitError    = 3

	DefaultWorkspaceDir   = "/workspace"
	DefaultCMExecutable   = "/usr/local/bin/cm"
	DefaultBashExecutable = "/bin/bash"
)

// PrintUsage outputs the command-line usage guide to the specified writer.
func PrintUsage(w io.Writer) {
	usage := `CodeMender Runner (cm-runner) - Headless Container Entrypoint

Usage:
  cm-runner find [path] [flags]     Run CodeMender vulnerability scan on full repo or sub-path
  cm-runner shell                   Launch interactive debug shell in /workspace

Subcommands:
  find [path] [flags]
      Scans repository at /workspace (default: '.') or scoped sub-path.
      Defaults to '--format json' on stdout unless overridden (-f or --format).
      Diagnostics and progress logs are routed to stderr.

  shell
      Launches /bin/bash in /workspace. Requires an interactive pseudo-TTY (-it).

Exit Codes:
  0    Scan completed successfully with 0 findings
  1    Scan completed with findings detected
  2    CLI usage error, invalid subcommand, missing path, or non-interactive shell
  >2   Fatal tooling, execution, or authentication error
`
	fmt.Fprint(w, usage)
}

// resolveExecutable checks if preferredPath exists; if not, falls back to PATH lookup.
func resolveExecutable(preferredPath, fallbackName string) string {
	if _, err := os.Stat(preferredPath); err == nil {
		return preferredPath
	}
	if path, err := exec.LookPath(fallbackName); err == nil {
		return path
	}
	return preferredPath
}

// Run executes the cm-runner logic with injected parameters for testing.
func Run(args []string, stdin *os.File, stdout, stderr io.Writer, workspaceDir, cmPath, bashPath string) int {
	plan, err := DispatchCommand(workspaceDir, args)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		PrintUsage(stderr)
		return ExitUsage
	}

	ctx := context.Background()
	baseEnv := os.Environ()

	switch plan.Type {
	case CmdTypeFind:
		flags := InjectFormatFlags(plan.Flags)
		env := ConfigureEnvironment(baseEnv)

		resolvedCM := resolveExecutable(cmPath, "cm")
		cmdArgs := make([]string, 0, 2+len(flags))
		cmdArgs = append(cmdArgs, "find", plan.TargetPath)
		cmdArgs = append(cmdArgs, flags...)

		code, _ := ExecuteProcess(ctx, resolvedCM, cmdArgs, env, workspaceDir, stdin, stdout, stderr)
		return code

	case CmdTypeShell:
		resolvedBash := resolveExecutable(bashPath, "bash")
		return ExecuteShell(ctx, resolvedBash, workspaceDir, stdin, stdout, stderr, baseEnv, DefaultIsTerminal, DefaultCommandRunner)

	default:
		fmt.Fprintf(stderr, "Error: unknown subcommand '%s'\n", plan.Type)
		PrintUsage(stderr)
		return ExitUsage
	}
}

func main() {
	code := Run(
		os.Args[1:],
		os.Stdin,
		os.Stdout,
		os.Stderr,
		DefaultWorkspaceDir,
		DefaultCMExecutable,
		DefaultBashExecutable,
	)
	os.Exit(code)
}
