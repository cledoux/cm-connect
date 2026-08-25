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
// Automatically injects -y (auto-approve / skip update check) after the target path
// for unattended batch execution unless --help or -h is requested or -y/--yes is already provided.
func (c *FindCommand) Cmd() []string {
	target := c.TargetPath
	if strings.TrimSpace(target) == "" {
		target = "."
	}
	args := []string{"find", target}
	if !isHelpRequested(c.Flags) {
		if !hasYesFlag(c.Flags) {
			args = append(args, "-y")
		}
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

// DefaultDiffFilename defines the default staging file name in the workspace root.
// Governing: ADR-0007, SPEC-cm-batch-runner, REQ-0014
const DefaultDiffFilename = "pull-request.diff"

// DefaultDiffPath defines the default staging path in /workspace.
// Governing: ADR-0007, SPEC-cm-batch-runner, REQ-0014
const DefaultDiffPath = "/workspace/pull-request.diff"

// BaseDiffContext defines the default context prompt grounding CodeMender in diff scanning.
// Governing: ADR-0007, SPEC-cm-batch-runner, REQ-0014
const BaseDiffContext = "You are evaluating a change request. The target is the unified diff containing the change. You are executing in the root directory of the repository and so have access to any repo files you need for context."

// ConsolidateContext merges the base diff context prompt with any user-supplied
// context flags (-c or --context), stripping standalone user context flags from the
// remaining passthrough slice.
// Governing: ADR-0007, SPEC-cm-batch-runner, REQ-0014
func ConsolidateContext(flags []string) []string {
	var userContexts []string
	var remaining []string

	for i := 0; i < len(flags); i++ {
		arg := flags[i]
		if arg == "-c" || arg == "--context" {
			if i+1 < len(flags) {
				i++
				val := strings.TrimSpace(flags[i])
				if val != "" {
					userContexts = append(userContexts, val)
				}
			}
			continue
		}
		if strings.HasPrefix(arg, "-c=") {
			val := strings.TrimSpace(strings.TrimPrefix(arg, "-c="))
			if val != "" {
				userContexts = append(userContexts, val)
			}
			continue
		}
		if strings.HasPrefix(arg, "--context=") {
			val := strings.TrimSpace(strings.TrimPrefix(arg, "--context="))
			if val != "" {
				userContexts = append(userContexts, val)
			}
			continue
		}
		remaining = append(remaining, arg)
	}

	fullContext := BaseDiffContext
	if len(userContexts) > 0 {
		joined := strings.Join(userContexts, " ")
		if joined != "" {
			fullContext = fmt.Sprintf("%s %s", BaseDiffContext, joined)
		}
	}

	result := make([]string, 0, 1+len(remaining))
	result = append(result, fmt.Sprintf("--context=%s", fullContext))
	result = append(result, remaining...)
	return result
}

// FindDiffCommand encapsulates parameters for the 'cm find' diff scan phase.
// Governing: ADR-0007, SPEC-cm-batch-runner, REQ-0014
type FindDiffCommand struct {
	DiffPath string
	Flags    []string
}

// NewFindDiffCommand constructs a FindDiffCommand with a diff path (default: "/tmp/cm-diff.diff")
// and optional initial scanner flags.
func NewFindDiffCommand(diffPath string, flags ...string) *FindDiffCommand {
	trimmed := strings.TrimSpace(diffPath)
	if trimmed == "" {
		trimmed = DefaultDiffPath
	}
	return &FindDiffCommand{
		DiffPath: trimmed,
		Flags:    flags,
	}
}

// SetArgs sets unowned scanner flags for FindDiffCommand.
func (c *FindDiffCommand) SetArgs(args ...string) ([]string, error) {
	c.Flags = append(c.Flags, args...)
	return nil, nil
}

// Cmd returns the complete command argument vector for 'cm find' against a diff patch.
// Automatically injects -y (or preserves --yes) and the consolidated --context flag
// unless --help or -h is requested.
func (c *FindDiffCommand) Cmd() []string {
	diffPath := c.DiffPath
	if strings.TrimSpace(diffPath) == "" {
		diffPath = DefaultDiffPath
	}
	args := []string{"find", diffPath}
	if isHelpRequested(c.Flags) {
		args = append(args, c.Flags...)
		return args
	}

	yesFlag := "-y"
	if hasExactFlag(c.Flags, "--yes") {
		yesFlag = "--yes"
	}
	args = append(args, yesFlag)

	filteredFlags := filterYesFlags(c.Flags)
	args = append(args, ConsolidateContext(filteredFlags)...)
	return args
}

func filterYesFlags(flags []string) []string {
	var filtered []string
	for _, f := range flags {
		if f != "-y" && f != "--yes" {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

func hasExactFlag(flags []string, target string) bool {
	for _, f := range flags {
		if f == target {
			return true
		}
	}
	return false
}
