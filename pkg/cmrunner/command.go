package cmrunner

import (
	"fmt"
	"strings"
)

// FindCommand encapsulates parameters for the 'cm find' vulnerability scan phase.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0004, REQ-0005
type FindCommand struct {
	TargetPath string
	Flags      []string
}

// NewFindCommand constructs a FindCommand with a target path (default: ".").
func NewFindCommand(targetPath string) *FindCommand {
	trimmed := strings.TrimSpace(targetPath)
	if trimmed == "" {
		trimmed = "."
	}
	return &FindCommand{
		TargetPath: trimmed,
	}
}

// SetArgs sets scanner flags for FindCommand.
func (c *FindCommand) SetArgs(args ...string) ([]string, error) {
	c.Flags = append(c.Flags, args...)
	return nil, nil
}

// Cmd returns the complete command argument vector for 'cm find'.
func (c *FindCommand) Cmd() []string {
	target := c.TargetPath
	if strings.TrimSpace(target) == "" {
		target = "."
	}
	args := []string{"find", target}
	args = append(args, c.Flags...)
	return args
}

// ReportCommand encapsulates parameters for the 'cm report' output synthesis phase.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0005, REQ-0006
type ReportCommand struct {
	Format string
	Flags  []string
}

// NewReportCommand constructs a ReportCommand with optional output format (default: "json").
func NewReportCommand(format ...string) *ReportCommand {
	f := "json"
	if len(format) > 0 && strings.TrimSpace(format[0]) != "" {
		f = strings.TrimSpace(format[0])
	}
	return &ReportCommand{
		Format: f,
	}
}

// SetArgs parses CLI flags, extracting recognized format flags and returning unrecognized flags.
func (c *ReportCommand) SetArgs(args ...string) ([]string, error) {
	var unknown []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-f" || arg == "--format" || arg == "-format" {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag needs an argument: %s", arg)
			}
			c.Format = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--format=") {
			c.Format = strings.TrimPrefix(arg, "--format=")
		} else if strings.HasPrefix(arg, "-format=") {
			c.Format = strings.TrimPrefix(arg, "-format=")
		} else if strings.HasPrefix(arg, "-f=") {
			c.Format = strings.TrimPrefix(arg, "-f=")
		} else {
			unknown = append(unknown, arg)
		}
	}
	if strings.TrimSpace(c.Format) == "" {
		c.Format = "json"
	}
	return unknown, nil
}

// Cmd returns the complete command argument vector for 'cm report'.
func (c *ReportCommand) Cmd() []string {
	f := c.Format
	if strings.TrimSpace(f) == "" {
		f = "json"
	}
	args := []string{"report", fmt.Sprintf("--format=%s", f)}
	args = append(args, c.Flags...)
	return args
}
