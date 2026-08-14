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
	errMissingSubcommand = errors.New("missing subcommand: specify 'find'")
	errInvalidSubcommand = errors.New("unrecognized subcommand")
	errPathNotFound      = errors.New("scan target path does not exist in workspace")
	errPathTraversal     = errors.New("scan target path escapes workspace boundary")
)

// stripCMPrefix trims whitespace around tokens and drops any leading "cm" token.
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

// normalizePath verifies that targetPath exists within workspaceRoot.
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

// parseArgs parses CLI arguments and constructs a *cmrunner.FindCommand object.
// Usage: cm-runner find [path] [-- [flags...]]
func parseArgs(workspaceRoot string, rawArgs []string) (*cmrunner.FindCommand, error) {
	args := stripCMPrefix(rawArgs)
	if len(args) == 0 {
		return nil, errMissingSubcommand
	}

	subcommand := args[0]
	if subcommand != "find" {
		return nil, fmt.Errorf("%w '%s'", errInvalidSubcommand, subcommand)
	}

	remaining := args[1:]
	target := "."
	var flags []string

	// Look for standard '--' separator
	dashIndex := -1
	for i, arg := range remaining {
		if arg == "--" {
			dashIndex = i
			break
		}
	}

	if dashIndex != -1 {
		// Positional path is before '--', forwarded flags are after '--'
		beforeDash := remaining[:dashIndex]
		afterDash := remaining[dashIndex+1:]

		for _, token := range beforeDash {
			if strings.HasPrefix(token, "-") {
				flags = append(flags, token)
			} else {
				target = token
			}
		}
		flags = append(flags, afterDash...)
	} else {
		// Positional path followed by flags
		for i := 0; i < len(remaining); i++ {
			token := remaining[i]
			if strings.HasPrefix(token, "-") {
				flags = append(flags, token)
			} else if target == "." {
				target = token
			} else {
				flags = append(flags, token)
			}
		}
	}

	normalizedTarget, err := normalizePath(workspaceRoot, target)
	if err != nil {
		return nil, err
	}

	return cmrunner.NewFindCommand(normalizedTarget, flags...), nil
}
