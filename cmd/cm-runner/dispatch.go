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

// stripCMPrefix trims whitespace around tokens and drops any leading "cm" token.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0008
func stripCMPrefix(rawArgs []string) []string {
	clean := make([]string, 0, len(rawArgs))
	for _, arg := range rawArgs {
		trimmed := strings.TrimSpace(arg)
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}

	if len(clean) > 0 && clean[0] == "cm" {
		return clean[1:]
	}
	return clean
}

// normalizePath verifies that targetPath exists within workspaceRoot and prevents path traversal.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0004
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
// Handles prefix normalization, subcommand detection, flag splitting, and nested command parsing.
// Governing: ADR-0001, SPEC-cm-batch-runner, REQ-0003, REQ-0004, REQ-0006, REQ-0008, REQ-0009
func parseArgs(workspaceRoot string, rawArgs []string) (cmds []cmrunner.Command, targetDir string, isShell bool, err error) {
	cleanArgs := stripCMPrefix(rawArgs)
	if len(cleanArgs) == 0 {
		return nil, "", false, errMissingSubcommand
	}

	subcommand := cleanArgs[0]
	if subcommand == "shell" {
		target := "."
		if len(cleanArgs) > 1 {
			target = cleanArgs[1]
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

	remaining := cleanArgs[1:]
	target := "."
	var cmFlags []string

	// Look for standard '--' separator
	dashIndex := -1
	for i, arg := range remaining {
		if arg == "--" {
			dashIndex = i
			break
		}
	}

	if dashIndex != -1 {
		beforeDash := remaining[:dashIndex]
		afterDash := remaining[dashIndex+1:]

		for _, token := range beforeDash {
			if !strings.HasPrefix(token, "-") && target == "." {
				target = token
			} else {
				cmFlags = append(cmFlags, token)
			}
		}
		cmFlags = append(cmFlags, afterDash...)
	} else {
		for _, token := range remaining {
			if !strings.HasPrefix(token, "-") && target == "." {
				target = token
			} else {
				cmFlags = append(cmFlags, token)
			}
		}
	}

	normalizedTarget, err := normalizePath(workspaceRoot, target)
	if err != nil {
		return nil, "", false, err
	}

	// Nested command parsing: ReportCommand extracts format flags, leftovers go to FindCommand
	reportCmd := cmrunner.NewReportCommand()
	scanFlags, err := reportCmd.SetArgs(cmFlags...)
	if err != nil {
		return nil, "", false, err
	}

	findCmd := cmrunner.NewFindCommand(normalizedTarget)
	if _, err := findCmd.SetArgs(scanFlags...); err != nil {
		return nil, "", false, err
	}

	if isHelpRequested(findCmd.Flags) {
		return []cmrunner.Command{findCmd}, workspaceRoot, false, nil
	}

	return []cmrunner.Command{findCmd, reportCmd}, workspaceRoot, false, nil
}
