package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CommandType represents the type of subcommand to execute.
type CommandType string

const (
	CmdTypeFind  CommandType = "find"
	CmdTypeShell CommandType = "shell"
)

// ExecPlan encapsulates the resolved execution plan.
type ExecPlan struct {
	Type       CommandType
	TargetPath string
	Flags      []string
}

// StripCMPrefix strips any leading "cm" token from the argument list.
func StripCMPrefix(args []string) []string {
	if len(args) > 0 && args[0] == "cm" {
		return args[1:]
	}
	return args
}

// NormalizePath validates that the target path exists within workspaceRoot
// and returns a normalized relative path. If targetPath is empty or ".",
// it returns ".". If targetPath does not exist or escapes workspaceRoot,
// it returns an error formatted according to REQ-0004.
func NormalizePath(workspaceRoot string, targetPath string) (string, error) {
	if targetPath == "" || targetPath == "." || targetPath == "./" {
		return ".", nil
	}

	cleaned := filepath.Clean(targetPath)
	if cleaned == "." {
		return ".", nil
	}

	var relPath string
	if filepath.IsAbs(cleaned) {
		rel, err := filepath.Rel(workspaceRoot, cleaned)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			return "", fmt.Errorf("scan target path '%s' does not exist in %s", targetPath, workspaceRoot)
		}
		relPath = rel
	} else {
		if strings.HasPrefix(cleaned, "..") || cleaned == ".." {
			return "", fmt.Errorf("scan target path '%s' does not exist in %s", targetPath, workspaceRoot)
		}
		relPath = cleaned
	}

	fullPath := filepath.Join(workspaceRoot, relPath)
	if _, err := os.Stat(fullPath); err != nil {
		return "", fmt.Errorf("scan target path '%s' does not exist in %s", targetPath, workspaceRoot)
	}

	return relPath, nil
}

// isFlagWithArg returns true if the flag expects an argument as the next token.
func isFlagWithArg(flag string) bool {
	switch flag {
	case "--format", "-f",
		"--model", "-m",
		"--output", "-o",
		"--config", "-c",
		"--db",
		"--target",
		"--profile",
		"--prompt",
		"--rule", "-r",
		"--log-level":
		return true
	default:
		return false
	}
}

// DispatchCommand parses CLI arguments, strips any "cm" prefix, validates the
// subcommand and target path against workspaceRoot, and produces an ExecPlan.
func DispatchCommand(workspaceRoot string, rawArgs []string) (*ExecPlan, error) {
	args := StripCMPrefix(rawArgs)
	if len(args) == 0 {
		return nil, fmt.Errorf("missing subcommand: cm-runner requires an explicit subcommand (e.g., 'find', 'shell')")
	}

	subcommand := args[0]
	subArgs := args[1:]

	switch subcommand {
	case "find":
		plan := &ExecPlan{
			Type:       CmdTypeFind,
			TargetPath: ".",
			Flags:      make([]string, 0),
		}

		var targetFound bool
		i := 0
		for i < len(subArgs) {
			arg := subArgs[i]
			if strings.HasPrefix(arg, "-") {
				plan.Flags = append(plan.Flags, arg)
				// If this flag takes a parameter and is not in --flag=val format, consume next arg if available
				if isFlagWithArg(arg) && !strings.Contains(arg, "=") && i+1 < len(subArgs) && !strings.HasPrefix(subArgs[i+1], "-") {
					i++
					plan.Flags = append(plan.Flags, subArgs[i])
				}
			} else if !targetFound {
				normPath, err := NormalizePath(workspaceRoot, arg)
				if err != nil {
					return nil, err
				}
				plan.TargetPath = normPath
				targetFound = true
			} else {
				// Additional positional argument, treat as flag or pass through
				plan.Flags = append(plan.Flags, arg)
			}
			i++
		}

		return plan, nil

	case "shell":
		return &ExecPlan{
			Type:  CmdTypeShell,
			Flags: subArgs,
		}, nil

	default:
		return nil, fmt.Errorf("unrecognized subcommand '%s'. Valid subcommands are 'find', 'shell'", subcommand)
	}
}
