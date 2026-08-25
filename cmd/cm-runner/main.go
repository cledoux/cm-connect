package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
// Governing: ADR-0001, ADR-0007, SPEC-cm-batch-runner, REQ-0002, REQ-0003, REQ-0014
func printUsage(w io.Writer) {
	usage := `CodeMender Runner (cm-runner) - Headless Container Entrypoint

Usage:
  cm-runner find [path] [-- [flags]]          Run CodeMender vulnerability scan on full repo or sub-path
  cm-runner find-diff [git-diff-args...] [-- [flags]] Run diff-scoped CodeMender vulnerability scan
  cm-runner fix <finding.json | -> [-- [flags]] Remediate finding and emit JSON change envelope to stdout
  cm-runner shell [path]                      Launch interactive /bin/bash shell in /workspace (requires -it)
  cm-runner init                              Pre-seed and apply headless configuration defaults in-place

Arguments:
  [path]               For find/shell: Scans repository at /workspace (default: '.') or scoped sub-path.
  [git-diff-args...]   For find-diff: Revisions, commit SHAs, or ranges forwarded to git diff (default: HEAD).
  <finding.json | ->   For fix: Path to finding JSON artifact, or '-' to read from standard input.
  [-- [flags...]]      Optional flags forwarded directly to CodeMender CLI.
                       Emits structured JSON findings or change envelope on stdout.
                       Diagnostics and progress logs are routed to stderr.

Exit Codes:
  0    Remediation succeeded / patch generated, clean scan, empty diff, or init/help completed
  1    Remediation unresolved / no patch generated, or findings detected
  2    CLI usage error, git diff error, invalid target, malformed finding JSON, or missing TTY on shell
  >2   Fatal tooling, execution, or authentication error
`
	fmt.Fprint(w, usage)
}

// execGitDiffFn executes git diff within the specified directory (pluggable for testing).
// Governing: ADR-0007, ADR-0008, REQ-0014, SPEC-cm-batch-runner
var execGitDiffFn = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"-c", "safe.directory=*", "--no-pager", "diff"}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"GIT_LFS_SKIP_SMUDGE=1",
	)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return outBuf.Bytes(), nil
}

// runFindDiff executes the diff-scoped scanning workflow.
// Governing: ADR-0007, REQ-0014, SPEC-cm-batch-runner
func runFindDiff(ctx context.Context, plan DispatchPlan, stdin io.Reader, stdout, stderr io.Writer, workspaceDir, cmPath string) int {
	cfg, err := cmconfig.LoadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to register .diff extension in configuration: %v\n", err)
		return cmrunner.ExitError
	}
	if err := cfg.AppendExtension(".diff"); err != nil {
		fmt.Fprintf(stderr, "Error: failed to register .diff extension in configuration: %v\n", err)
		return cmrunner.ExitError
	}
	_ = cfg.ApplyOverrides(map[string]any{
		"vcs.commands.reset": "true",
	})
	if err := cfg.Write(); err != nil {
		fmt.Fprintf(stderr, "Error: failed to register .diff extension in configuration: %v\n", err)
		return cmrunner.ExitError
	}

	diffBytes, err := execGitDiffFn(ctx, workspaceDir, plan.GitDiffArgs...)
	if err != nil {
		fmt.Fprintf(stderr, "Error: git diff failed: %v\nHint: Verify git revision syntax or configure fetch-depth: 0 in CI.\n", err)
		return cmrunner.ExitUsage
	}

	if len(bytes.TrimSpace(diffBytes)) == 0 {
		fmt.Fprintln(stdout, "[]")
		return cmrunner.ExitClean
	}

	diffPath := filepath.Join(workspaceDir, cmrunner.DefaultDiffFilename)
	if err := os.WriteFile(diffPath, diffBytes, 0o600); err != nil {
		fmt.Fprintf(stderr, "Error: failed to write staged diff file %q: %v\n", diffPath, err)
		return cmrunner.ExitError
	}
	defer os.Remove(diffPath)

	findDiffCmd := cmrunner.NewFindDiffCommand(diffPath)
	findDiffCmd.Flags = plan.PassthroughFlags

	reportCmd := cmrunner.NewReportCommand("json")

	runner := cmrunner.NewRunner(
		cmrunner.WithExecutable(cmPath),
		cmrunner.WithWorkspace(workspaceDir),
		cmrunner.WithGlobalFlags("--sandbox=false"),
	)

	code, _ := runner.RunSequence(ctx, []cmrunner.Command{findDiffCmd, reportCmd}, stdin, stdout, stderr)
	return code
}

// run parses arguments and orchestrates execution of the target subcommand.
// Governing: ADR-0001, ADR-0002, ADR-0005, ADR-0007, SPEC-cm-batch-runner, SPEC-cm-fix-runner, REQ-0001, REQ-0002, REQ-0003, REQ-0005, REQ-0008, REQ-0009, REQ-0010, REQ-0014
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, workspaceDir, cmPath string) int {
	plan, err := parseArgs(workspaceDir, args, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		printUsage(stderr)
		return cmrunner.ExitUsage
	}

	ctx := context.Background()

	switch plan.Action {
	case ActionHelp:
		printUsage(stdout)
		return cmrunner.ExitClean

	case ActionInit:
		if err := cmconfig.ApplyDefaultOverrides(); err != nil {
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
			cmrunner.WithGlobalFlags("--sandbox=false"),
		)

		code, _ := runner.RunSequence(ctx, plan.Commands, stdin, stdout, stderr)
		return code

	case ActionFindDiff:
		return runFindDiff(ctx, plan, stdin, stdout, stderr, workspaceDir, cmPath)

	case ActionFix:
		runner := cmrunner.NewRunner(
			cmrunner.WithExecutable(cmPath),
			cmrunner.WithWorkspace(workspaceDir),
		)

		ctx := context.Background()
		code, _ := runner.RunFixPipeline(ctx, plan.RawFinding, plan.PassthroughFlags, stdout, stderr)
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
