package cmrunner

import (
	"fmt"
	"strings"
)

// FindCommand encapsulates parameters for the 'cm find' vulnerability scan phase.
// Governing: ADR-0001, ADR-0002, SPEC-cm-batch-runner, REQ-0004, REQ-0005, REQ-0006
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
// Governing: ADR-0001, ADR-0002, SPEC-cm-batch-runner, REQ-0005, REQ-0006
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

// SetArgs sets extra flags for ReportCommand.
func (c *ReportCommand) SetArgs(args ...string) ([]string, error) {
	c.Flags = append(c.Flags, args...)
	return nil, nil
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
