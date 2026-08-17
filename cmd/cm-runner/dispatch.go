package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cm-connect/pkg/cmrunner"
)

var (
	errMissingSubcommand = errors.New("missing subcommand: specify 'find' or 'shell'")
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

// parseArgs parses raw CLI arguments into executable command sequences or shell parameters.
// Enforces exact os.Args[1] subcommand dispatch and '--' passthrough partitioning.
// Governing: ADR-0001, ADR-0002, SPEC-cm-batch-runner, REQ-0003, REQ-0004, REQ-0005, REQ-0006, REQ-0008, REQ-0009
func parseArgs(workspaceRoot string, rawArgs []string) (cmds []cmrunner.Command, targetDir string, isShell bool, err error) {
	if len(rawArgs) == 0 {
		return nil, "", false, errMissingSubcommand
	}

	subcommand := rawArgs[0]
	if subcommand == "cm" {
		return nil, "", false, fmt.Errorf("%w '%s'", errInvalidSubcommand, subcommand)
	}

	if subcommand == "shell" {
		target := "."
		if len(rawArgs) > 1 {
			target = rawArgs[1]
		}
		relPath, err := normalizePath(workspaceRoot, target)
		if err != nil {
			return nil, "", false, err
		}
		return nil, filepath.Join(workspaceRoot, relPath), true, nil
	}

	if subcommand != "find" {
		return nil, "", false, fmt.Errorf("%w '%s'", errInvalidSubcommand, subcommand)
	}

	remaining := rawArgs[1:]
	beforeDash, afterDash := partitionDash(remaining)

	target := "."
	if len(beforeDash) > 0 {
		target = beforeDash[0]
	}

	normalizedTarget, err := normalizePath(workspaceRoot, target)
	if err != nil {
		return nil, "", false, err
	}

	findCmd := cmrunner.NewFindCommand(normalizedTarget)
	findCmd.Flags = afterDash

	if isHelpRequested(findCmd.Flags) {
		return []cmrunner.Command{findCmd}, workspaceRoot, false, nil
	}

	reportCmd := cmrunner.NewReportCommand("json")
	return []cmrunner.Command{findCmd, reportCmd}, workspaceRoot, false, nil
}
