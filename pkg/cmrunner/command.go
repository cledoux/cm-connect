package cmrunner

import (
	"strings"
)

// FindCommand encapsulates parameters for executing the CodeMender 'find' scan.
type FindCommand struct {
	TargetPath string
	Flags      []string
}

// NewFindCommand constructs a FindCommand with the target scan path and optional forwarded flags.
func NewFindCommand(targetPath string, flags ...string) *FindCommand {
	if targetPath == "" {
		targetPath = "."
	}
	return &FindCommand{
		TargetPath: targetPath,
		Flags:      flags,
	}
}

// HasFormatFlag returns true if explicit format or help flags are present in the command flags.
func (c *FindCommand) HasFormatFlag() bool {
	for _, flag := range c.Flags {
		if flag == "--format" || flag == "-f" || flag == "--help" || flag == "-h" {
			return true
		}
		if strings.HasPrefix(flag, "--format=") || strings.HasPrefix(flag, "-f=") {
			return true
		}
	}
	return false
}

// Args builds the complete argument list to pass to the CodeMender binary, ensuring
// that '--format json' is injected by default unless explicitly overridden.
func (c *FindCommand) Args() []string {
	target := c.TargetPath
	if target == "" {
		target = "."
	}

	flags := c.Flags
	if !c.HasFormatFlag() {
		flagsWithFormat := make([]string, len(flags), len(flags)+2)
		copy(flagsWithFormat, flags)
		flags = append(flagsWithFormat, "--format", "json")
	}

	args := make([]string, 0, 2+len(flags))
	args = append(args, "find", target)
	args = append(args, flags...)
	return args
}
