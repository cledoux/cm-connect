package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cm-connect/pkg/cmrunner"
)

// ActionType represents the execution action determined by argument parsing.
// Governing: ADR-0001, ADR-0002, SPEC-cm-batch-runner
type ActionType int

const (
	ActionNone ActionType = iota
	ActionHelp
	ActionInit
	ActionShell
	ActionRunSequence
	ActionFix
)

// DispatchPlan encapsulates the routing decision and associated execution parameters.
// Governing: ADR-0001, ADR-0002, ADR-0005, SPEC-cm-batch-runner, SPEC-cm-fix-runner
type DispatchPlan struct {
	Action           ActionType
	Commands         []cmrunner.Command
	TargetDir        string
	RawFinding       []byte
	PassthroughFlags []string
}

var (
	errMissingSubcommand = errors.New("missing subcommand: specify 'find', 'shell', or 'init'")
	errInvalidSubcommand = errors.New("unrecognized subcommand")
	errPathNotFound      = errors.New("scan target path does not exist in workspace")
	errPathTraversal     = errors.New("scan target path escapes workspace boundary")
)

// partitionDash splits raw arguments into tokens before '--' and tokens after '--'.
// Governing: ADR-0002, SPEC-cm-batch-runner, REQ-0006
func partitionDash(args []string) (beforeDash, afterDash []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

// normalizePath verifies that targetPath exists within workspaceRoot and prevents path traversal.
// Governing: ADR-0001, ADR-0002, SPEC-cm-batch-runner, REQ-0004
func normalizePath(workspaceRoot, targetPath string) (string, error) {
	trimmed := strings.TrimSpace(targetPath)
	if trimmed == "" || trimmed == "." || trimmed == "./" {
		return ".", nil
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == "." {
		return ".", nil
	}

	var relPath string
	if filepath.IsAbs(cleaned) {
		rel, err := filepath.Rel(workspaceRoot, cleaned)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			return "", fmt.Errorf("%w: %s escapes %s", errPathTraversal, targetPath, workspaceRoot)
		}
		relPath = rel
	} else {
		if strings.HasPrefix(cleaned, "..") || cleaned == ".." {
			return "", fmt.Errorf("%w: %s escapes %s", errPathTraversal, targetPath, workspaceRoot)
		}
		relPath = cleaned
	}

	fullPath := filepath.Join(workspaceRoot, relPath)
	if _, err := os.Stat(fullPath); err != nil {
		return "", fmt.Errorf("%w: %s in %s", errPathNotFound, targetPath, workspaceRoot)
	}

	return relPath, nil
}

func isHelpRequested(flags []string) bool {
	for _, f := range flags {
		if f == "--help" || f == "-h" {
			return true
		}
	}
	return false
}

// parseShellArgs processes CLI arguments for the interactive 'shell' subcommand.
func parseShellArgs(workspaceRoot string, args []string) (DispatchPlan, error) {
	if isHelpRequested(args) {
		return DispatchPlan{Action: ActionHelp}, nil
	}
	target := "."
	if len(args) > 0 {
		target = args[0]
	}
	relPath, err := normalizePath(workspaceRoot, target)
	if err != nil {
		return DispatchPlan{}, err
	}
	return DispatchPlan{
		Action:    ActionShell,
		TargetDir: filepath.Join(workspaceRoot, relPath),
	}, nil
}

// parseFindArgs processes CLI arguments for the two-phase 'find' scan and report pipeline.
func parseFindArgs(workspaceRoot string, args []string) (DispatchPlan, error) {
	beforeDash, afterDash := partitionDash(args)

	if isHelpRequested(beforeDash) || isHelpRequested(afterDash) {
		return DispatchPlan{Action: ActionHelp}, nil
	}

	target := "."
	if len(beforeDash) > 0 {
		target = beforeDash[0]
	}

	normalizedTarget, err := normalizePath(workspaceRoot, target)
	if err != nil {
		return DispatchPlan{}, err
	}

	findCmd := cmrunner.NewFindCommand(normalizedTarget)
	findCmd.Flags = afterDash

	reportCmd := cmrunner.NewReportCommand("json")
	return DispatchPlan{
		Action:   ActionRunSequence,
		Commands: []cmrunner.Command{findCmd, reportCmd},
	}, nil
}

// parseInitArgs processes CLI arguments for the 'init' configuration mutation subcommand.
// CodeMender always uses $HOME/.codemender/config.yaml, so init takes no positional path.
// Governing: REQ-0002, SPEC-cm-batch-runner
func parseInitArgs(args []string) (DispatchPlan, error) {
	if isHelpRequested(args) {
		return DispatchPlan{Action: ActionHelp}, nil
	}
	if len(args) > 0 {
		return DispatchPlan{}, fmt.Errorf("unexpected arguments for 'init': 'init' takes no arguments (always mutates $HOME/.codemender/config.yaml)")
	}
	return DispatchPlan{Action: ActionInit}, nil
}

// parseArgs parses raw CLI arguments into a DispatchPlan.
// Enforces exact os.Args[1] subcommand dispatch and '--' passthrough partitioning.
// Governing: ADR-0001, ADR-0002, ADR-0005, SPEC-cm-batch-runner, SPEC-cm-fix-runner, REQ-0001, REQ-0002, REQ-0003, REQ-0004, REQ-0005, REQ-0006, REQ-0008, REQ-0009
func parseArgs(workspaceRoot string, rawArgs []string, stdin ...io.Reader) (DispatchPlan, error) {
	if len(rawArgs) == 0 {
		return DispatchPlan{}, errMissingSubcommand
	}

	var in io.Reader
	if len(stdin) > 0 {
		in = stdin[0]
	}

	subcommand := rawArgs[0]
	switch subcommand {
	case "shell":
		return parseShellArgs(workspaceRoot, rawArgs[1:])
	case "find":
		return parseFindArgs(workspaceRoot, rawArgs[1:])
	case "init":
		return parseInitArgs(rawArgs[1:])
	case "fix":
		return parseFixArgs(workspaceRoot, rawArgs[1:], in)
	default:
		return DispatchPlan{}, fmt.Errorf("%w '%s'", errInvalidSubcommand, subcommand)
	}
}

func parseFixArgs(workspaceRoot string, args []string, stdin io.Reader) (DispatchPlan, error) {
	beforeDash, afterDash := partitionDash(args)

	if isHelpRequested(beforeDash) || isHelpRequested(afterDash) {
		return DispatchPlan{Action: ActionHelp}, nil
	}

	if len(beforeDash) == 0 || strings.TrimSpace(beforeDash[0]) == "" {
		return DispatchPlan{}, fmt.Errorf("missing target finding argument: specify a finding JSON file path or '-' for stdin")
	}

	target := strings.TrimSpace(beforeDash[0])
	var raw []byte

	if target == "-" {
		if stdin == nil {
			return DispatchPlan{}, fmt.Errorf("stdin is nil: cannot read finding payload")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return DispatchPlan{}, fmt.Errorf("failed to read finding from stdin: %w", err)
		}
		if len(bytes.TrimSpace(data)) == 0 {
			return DispatchPlan{}, fmt.Errorf("empty finding payload received from stdin")
		}
		raw = data
	} else {
		targetFile := target
		if !filepath.IsAbs(targetFile) && workspaceRoot != "" {
			if _, err := os.Stat(targetFile); err != nil {
				wsTarget := filepath.Join(workspaceRoot, targetFile)
				if _, wsErr := os.Stat(wsTarget); wsErr == nil {
					targetFile = wsTarget
				}
			}
		}

		if _, err := os.Stat(targetFile); err != nil {
			return DispatchPlan{}, fmt.Errorf("finding file not found: %s (%w)", target, err)
		}

		data, err := os.ReadFile(targetFile)
		if err != nil {
			return DispatchPlan{}, fmt.Errorf("failed to read finding file: %s (%w)", target, err)
		}
		if len(bytes.TrimSpace(data)) == 0 {
			return DispatchPlan{}, fmt.Errorf("empty finding file: %s", target)
		}
		raw = data
	}

	return DispatchPlan{
		Action:           ActionFix,
		RawFinding:       raw,
		PassthroughFlags: afterDash,
	}, nil
}
