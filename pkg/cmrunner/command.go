package cmrunner

import (
	"bytes"
	"context"
	"io"
	"strings"
)

// Command represents an executable CodeMender command or multi-stage execution pipeline.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0001, REQ-0005
type Command interface {
	// Subcommand returns the primary subcommand identifier (e.g. "find", "verify", "fix").
	Subcommand() string
	// Execute executes the command against the provided runner environment.
	Execute(ctx context.Context, r *Runner, stdin io.Reader, stdout, stderr io.Writer) (int, error)
}

// FindCommand encapsulates parameters for executing the two-phase CodeMender 'find' scan and report pipeline.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0004, REQ-0005, REQ-0006
type FindCommand struct {
	TargetPath   string
	ScanFlags    []string
	ReportFormat string
}

// NewFindCommand constructs a FindCommand with target scan path and raw forwarded flags.
// It automatically extracts output format flags (-f, --format) for Phase 2, and forwards
// all other flags to Phase 1 (cm find).
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0004, REQ-0005, REQ-0006
func NewFindCommand(targetPath string, rawFlags ...string) *FindCommand {
	if targetPath == "" {
		targetPath = "."
	}
	cmd := &FindCommand{
		TargetPath:   targetPath,
		ReportFormat: "json", // default format
	}
	cmd.classifyFlags(rawFlags)
	return cmd
}

// Subcommand returns "find".
func (c *FindCommand) Subcommand() string {
	return "find"
}

// classifyFlags parses raw CLI flags, extracting the report format flag and collecting scanner flags.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0005, REQ-0006
func (c *FindCommand) classifyFlags(flags []string) {
	for i := 0; i < len(flags); i++ {
		flag := flags[i]

		// Format flag handling
		if flag == "--format" || flag == "-f" {
			if i+1 < len(flags) && !strings.HasPrefix(flags[i+1], "-") {
				c.ReportFormat = flags[i+1]
				i++
			}
			continue
		}
		if strings.HasPrefix(flag, "--format=") {
			c.ReportFormat = strings.TrimPrefix(flag, "--format=")
			continue
		}
		if strings.HasPrefix(flag, "-f=") {
			c.ReportFormat = strings.TrimPrefix(flag, "-f=")
			continue
		}

		// Scanner-specific flags (e.g. -c, --context, -y, --yes, --unrestricted, etc.)
		if flag == "-c" || flag == "--context" {
			c.ScanFlags = append(c.ScanFlags, flag)
			if i+1 < len(flags) && !strings.HasPrefix(flags[i+1], "-") {
				c.ScanFlags = append(c.ScanFlags, flags[i+1])
				i++
			}
			continue
		}
		c.ScanFlags = append(c.ScanFlags, flag)
	}
}

// HasFormatFlag returns true if explicit format or help flags are present.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0006
func (c *FindCommand) HasFormatFlag() bool {
	return c.ReportFormat != "" && c.ReportFormat != "json"
}

// FindArgs constructs the arguments to pass to 'cm find' (Phase 1).
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0004, REQ-0005
func (c *FindCommand) FindArgs() []string {
	target := c.TargetPath
	if target == "" {
		target = "."
	}

	// If help is requested, pass find --help
	for _, flag := range c.ScanFlags {
		if flag == "--help" || flag == "-h" {
			return []string{"find", flag}
		}
	}

	args := []string{"find", target}
	args = append(args, c.ScanFlags...)
	return args
}

// ReportArgs constructs the arguments to pass to 'cm report' (Phase 2).
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0005, REQ-0006
func (c *FindCommand) ReportArgs() []string {
	fmt := c.ReportFormat
	if fmt == "" {
		fmt = "json"
	}
	return []string{"report", "--format=" + fmt}
}

// Args returns FindArgs for backwards compatibility.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0001
func (c *FindCommand) Args() []string {
	return c.FindArgs()
}

// Execute coordinates the two-phase scan & report pipeline for FindCommand:
// Phase 1: executes 'cm find <target> [scan_flags]' with tool steps & logs routed to stderr.
// Phase 2: executes 'cm report --format=<fmt>' with clean findings emitted on stdout.
// Phase 3: evaluates findings count to return exit code 0 (clean) or 1 (findings detected).
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0005, REQ-0006, REQ-0007, REQ-0010, REQ-0013
func (c *FindCommand) Execute(
	ctx context.Context,
	r *Runner,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	// 1. If help was requested, pass find --help directly to stdout/stderr
	for _, flag := range c.ScanFlags {
		if flag == "--help" || flag == "-h" {
			return r.RunSubprocess(ctx, c.FindArgs(), stdin, stdout, stderr)
		}
	}

	// 2. Phase 1: Execute cm find (routing stdout and stderr to stderr for diagnostics)
	phase1Code, phase1Err := r.RunSubprocess(ctx, c.FindArgs(), stdin, stderr, stderr)
	if phase1Err != nil || phase1Code != ExitClean {
		if phase1Code != ExitClean {
			return phase1Code, phase1Err
		}
		return ExitError, phase1Err
	}

	// 3. Phase 2: Execute cm report (capturing output for finding evaluation while writing to stdout)
	var reportBuf bytes.Buffer
	reportOut := io.MultiWriter(stdout, &reportBuf)

	phase2Code, phase2Err := r.RunSubprocess(ctx, c.ReportArgs(), nil, reportOut, stderr)
	if phase2Err != nil || phase2Code != ExitClean {
		if phase2Code > ExitUsage {
			return phase2Code, phase2Err
		}
		return ExitError, phase2Err
	}

	// 4. Phase 3: Finding-aware exit code evaluation
	return evaluateReportExitCode(c.ReportFormat, reportBuf.Bytes()), nil
}
