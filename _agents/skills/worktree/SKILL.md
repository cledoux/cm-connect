---
name: worktree
description: >-
  Manage parallel agent git worktrees for cm-connect. Use before writing any code
  to create, manage, verify, and clean up isolated branch worktrees.
---

# Git Worktree Workflow for Multi-Agent Coordination

This skill provides standard procedures for creating, working within, and
cleaning up Git worktrees in `cm-connect`.

## Core Principle

- Never write or modify code in `./repo/`.
- Always perform work inside an isolated worktree under
  `./worktrees/<branch-slug>`.
- **Flat directory naming**: Worktree directory names MUST be flat and sanitize
  branch names by replacing slashes with dashes (e.g. branch
  `feat/finding-ingestion` -> directory `worktrees/feat-finding-ingestion`).

______________________________________________________________________

## 1. Inspect Existing Worktrees

Check active worktrees before starting a new task:

```bash
git -C /usr/local/google/home/ledoux/workspace/cm-connect/repo worktree list
```

## 2. Fetch Latest Upstream State

Ensure the local golden repo has the latest upstream `main`:

```bash
git -C /usr/local/google/home/ledoux/workspace/cm-connect/repo fetch upstream main
```

## 3. Create a Worktree

1. Choose a descriptive branch name:
   - Features: `feat/<task-description>` (e.g. `feat/finding-ingestion`)
   - Bug fixes: `fix/<issue-description>` (e.g. `fix/sigterm-handling`)
   - Documentation/Refactoring: `docs/<name>`, `refactor/<name>`
1. Derive the flattened directory slug (`<branch-slug>` replacing `/` with `-`):
   - `feat/finding-ingestion` -> `feat-finding-ingestion`
   - `fix/sigterm-handling` -> `fix-sigterm-handling`
1. Create the worktree:
   ```bash
   git -C /usr/local/google/home/ledoux/workspace/cm-connect/repo worktree add \
     /usr/local/google/home/ledoux/workspace/cm-connect/worktrees/<branch-slug> \
     -b <branch-name> upstream/main
   ```

## 4. Work Strictly Inside the Worktree

- Always target file operations and shell commands to
  `/usr/local/google/home/ledoux/workspace/cm-connect/worktrees/<branch-slug>`.
- Run tests and verifications:
  ```bash
  cd /usr/local/google/home/ledoux/workspace/cm-connect/worktrees/<branch-slug>
  make test
  make integration-test
  ```

## 5. Commit & Push

1. Use the `/commit` workflow to build focused, atomic commits.
1. Push the branch to upstream:
   ```bash
   git -C /usr/local/google/home/ledoux/workspace/cm-connect/worktrees/<branch-slug> push -u upstream <branch-name>
   ```

## 6. Cleanup After Merge

Keep the worktree alive during review. Once the pull request is merged, remove
the worktree:

```bash
git -C /usr/local/google/home/ledoux/workspace/cm-connect/repo worktree remove \
  /usr/local/google/home/ledoux/workspace/cm-connect/worktrees/<branch-slug>
```
