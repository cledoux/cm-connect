package cmpatch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var hunkHeaderRegex = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func normalizeDiffPath(p string) string {
	p = strings.TrimSpace(p)
	if fields := strings.Fields(p); len(fields) > 0 {
		p = fields[0]
	}
	if p == "/dev/null" {
		return ""
	}
	p = strings.TrimPrefix(p, "a/")
	p = strings.TrimPrefix(p, "b/")
	p = strings.TrimPrefix(p, "/workspace-ro/")
	p = strings.TrimPrefix(p, "/workspace/")
	p = strings.TrimPrefix(p, "/")
	return strings.TrimSpace(p)
}

// ParseDiff parses a unified diff string into a list of modified relative file paths
// and structured replacement hunks.
func ParseDiff(diffStr string) ([]string, []Hunk, error) {
	var filesModified []string
	var hunks []Hunk
	seenFiles := make(map[string]bool)

	var currentFile string
	var currentHunk *Hunk
	var origBuilder strings.Builder
	var replBuilder strings.Builder

	flushHunk := func() {
		if currentHunk != nil {
			currentHunk.Original = origBuilder.String()
			currentHunk.Replacement = replBuilder.String()
			hunks = append(hunks, *currentHunk)
			currentHunk = nil
			origBuilder.Reset()
			replBuilder.Reset()
		}
	}

	scanner := bufio.NewScanner(strings.NewReader(diffStr))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "diff --git ") {
			flushHunk()
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				f := normalizeDiffPath(parts[3])
				if f == "" && len(parts) >= 3 {
					f = normalizeDiffPath(parts[2])
				}
				if f != "" {
					currentFile = f
					if !seenFiles[f] {
						seenFiles[f] = true
						filesModified = append(filesModified, f)
					}
				}
			}
			continue
		}

		if strings.HasPrefix(line, "--- ") {
			flushHunk()
			rawPath := strings.TrimPrefix(line, "--- ")
			f := normalizeDiffPath(rawPath)
			if f != "" {
				currentFile = f
			}
			continue
		}

		if strings.HasPrefix(line, "+++ ") {
			flushHunk()
			rawPath := strings.TrimPrefix(line, "+++ ")
			f := normalizeDiffPath(rawPath)
			if f != "" {
				currentFile = f
				if !seenFiles[f] {
					seenFiles[f] = true
					filesModified = append(filesModified, f)
				}
			} else if currentFile != "" && !seenFiles[currentFile] {
				seenFiles[currentFile] = true
				filesModified = append(filesModified, currentFile)
			}
			continue
		}

		if strings.HasPrefix(line, "@@ ") {
			flushHunk()
			matches := hunkHeaderRegex.FindStringSubmatch(line)
			if len(matches) >= 4 {
				startLine, _ := strconv.Atoi(matches[1])
				count := 1
				if matches[2] != "" {
					count, _ = strconv.Atoi(matches[2])
				}
				endLine := startLine + count - 1
				if count == 0 {
					endLine = startLine
				}

				currentHunk = &Hunk{
					FilePath:  currentFile,
					StartLine: startLine,
					EndLine:   endLine,
				}
			}
			continue
		}

		if currentHunk != nil {
			if strings.HasPrefix(line, "+") {
				replBuilder.WriteString(line[1:])
				replBuilder.WriteString("\n")
			} else if strings.HasPrefix(line, "-") {
				origBuilder.WriteString(line[1:])
				origBuilder.WriteString("\n")
			} else if strings.HasPrefix(line, " ") {
				content := line[1:]
				origBuilder.WriteString(content)
				origBuilder.WriteString("\n")
				replBuilder.WriteString(content)
				replBuilder.WriteString("\n")
			} else if strings.HasPrefix(line, "\\") {
				// E.g., \ No newline at end of file
				continue
			}
		}
	}

	flushHunk()

	if filesModified == nil {
		filesModified = []string{}
	}
	if hunks == nil {
		hunks = []Hunk{}
	}

	return filesModified, hunks, nil
}

// SynthesizeEnvelope constructs a ChangeEnvelope from finding details and a raw unified diff.
// If rawDiff is empty, status is set to "UNRESOLVED". If non-empty, status is "FIXED".
func SynthesizeEnvelope(findingID, vulnType, title, summary, rawDiff string) (*ChangeEnvelope, error) {
	if strings.TrimSpace(rawDiff) == "" {
		return &ChangeEnvelope{
			FindingID:     findingID,
			Status:        "UNRESOLVED",
			VulnType:      vulnType,
			Title:         title,
			Summary:       summary,
			FilesModified: []string{},
			Patch:         "",
			Hunks:         []Hunk{},
		}, nil
	}

	files, hunks, err := ParseDiff(rawDiff)
	if err != nil {
		return nil, fmt.Errorf("failed to parse diff: %w", err)
	}

	return &ChangeEnvelope{
		FindingID:     findingID,
		Status:        "FIXED",
		VulnType:      vulnType,
		Title:         title,
		Summary:       summary,
		FilesModified: files,
		Patch:         rawDiff,
		Hunks:         hunks,
	}, nil
}

// ExtractPatch captures the unified diff inside workspaceDir using git diff HEAD
// with a fallback to directory diffing against fallbackDir if needed.
func ExtractPatch(ctx context.Context, workspaceDir, fallbackDir string) (string, error) {
	// Check if git repository is present
	checkGit := exec.CommandContext(ctx, "git", "-c", "safe.directory=*", "rev-parse", "--is-inside-work-tree")
	checkGit.Dir = workspaceDir
	if err := checkGit.Run(); err == nil {
		// Stage untracked files intent so new files appear in git diff
		stageCmd := exec.CommandContext(ctx, "git", "-c", "safe.directory=*", "add", "-N", ".")
		stageCmd.Dir = workspaceDir
		_ = stageCmd.Run()

		diffCmd := exec.CommandContext(ctx, "git", "-c", "safe.directory=*", "diff", "HEAD")
		diffCmd.Dir = workspaceDir
		out, err := diffCmd.Output()
		if err == nil {
			return string(out), nil
		}

		// Fallback to git diff (without HEAD in case no commits exist yet)
		diffNoHead := exec.CommandContext(ctx, "git", "-c", "safe.directory=*", "diff")
		diffNoHead.Dir = workspaceDir
		outNoHead, errNoHead := diffNoHead.Output()
		if errNoHead == nil {
			return string(outNoHead), nil
		}
	}

	// Fallback to directory diff
	if fallbackDir != "" {
		diffCmd := exec.CommandContext(ctx, "diff", "-u", "-r", fallbackDir, workspaceDir)
		out, err := diffCmd.CombinedOutput()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
				// Exit code 1 indicates differences were found
				return string(out), nil
			}
			return "", fmt.Errorf("directory diff failed: %s (%w)", string(out), err)
		}
		// Exit code 0 means identical
		return "", nil
	}

	return "", nil
}
