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
// Automatically injects -y (auto-approve / skip update check) for unattended batch execution.
func (c *FindCommand) Cmd() []string {
	target := c.TargetPath
	if strings.TrimSpace(target) == "" {
		target = "."
	}
	args := []string{"find", target}
	if !isHelpRequested(c.Flags) && !hasYesFlag(c.Flags) {
		args = append(args, "-y")
	}
	args = append(args, c.Flags...)
	return args
}

func isHelpRequested(flags []string) bool {
	for _, f := range flags {
		if f == "--help" || f == "-h" {
			return true
		}
	}
	return false
}

func hasYesFlag(flags []string) bool {
	for _, f := range flags {
		if f == "-y" || f == "--yes" {
			return true
		}
	}
	return false
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

// FixCommand encapsulates parameters for the 'cm fix' remediation phase.
// Governing: ADR-0002, ADR-0005, SPEC-cm-fix-runner, REQ-0005, REQ-0006
type FixCommand struct {
	FindingID string
	Flags     []string
}

// NewFixCommand constructs a FixCommand for a specific finding ID.
func NewFixCommand(findingID string, flags ...string) *FixCommand {
	id := strings.TrimSpace(findingID)
	if id == "" {
		id = "unknown"
	}
	return &FixCommand{
		FindingID: id,
		Flags:     flags,
	}
}

// SetArgs appends unowned passthrough flags to FixCommand.
func (c *FixCommand) SetArgs(args ...string) ([]string, error) {
	c.Flags = append(c.Flags, args...)
	return nil, nil
}

// Cmd returns the complete command argument vector for 'cm fix'.
// Automatically injects -y and --unrestricted for unattended headless remediation.
func (c *FixCommand) Cmd() []string {
	id := c.FindingID
	if strings.TrimSpace(id) == "" {
		id = "unknown"
	}
	args := []string{"fix", id}
	if !hasYesFlag(c.Flags) {
		args = append(args, "-y")
	}
	if !hasUnrestrictedFlag(c.Flags) {
		args = append(args, "--unrestricted")
	}
	args = append(args, c.Flags...)
	return args
}

func hasUnrestrictedFlag(flags []string) bool {
	for _, f := range flags {
		if f == "--unrestricted" {
			return true
		}
	}
	return false
}

// ImportCommand encapsulates parameters for seeding findings via 'cm report import'.
// Governing: ADR-0005, SPEC-cm-fix-runner, REQ-0004
type ImportCommand struct {
	ImportFile   string
	WorkspaceDir string
}

// NewImportCommand constructs an ImportCommand with specified import file and workspace dir.
func NewImportCommand(importFile, workspaceDir string) *ImportCommand {
	f := strings.TrimSpace(importFile)
	if f == "" {
		f = "/tmp/cm-import.json"
	}
	ws := strings.TrimSpace(workspaceDir)
	if ws == "" {
		ws = "/workspace"
	}
	return &ImportCommand{
		ImportFile:   f,
		WorkspaceDir: ws,
	}
}

// Cmd returns the argument vector for 'cm report import -f <file> -p <workspace>'.
func (c *ImportCommand) Cmd() []string {
	f := c.ImportFile
	if strings.TrimSpace(f) == "" {
		f = "/tmp/cm-import.json"
	}
	ws := c.WorkspaceDir
	if strings.TrimSpace(ws) == "" {
		ws = "/workspace"
	}
	return []string{"report", "import", "-f", f, "-p", ws}
}
