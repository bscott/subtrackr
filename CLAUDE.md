# SubTrackr - Claude Code Instructions

## Release Workflow

This project uses versioned branches for releases. Follow this workflow when working on new features or bug fixes.

### 1. Create a Versioned Branch

```bash
# Check current version
gh release list --limit 1

# Create and checkout versioned branch
git checkout -b v0.X.Y
```

### 2. Track Work with Beads

```bash
# Create beads issues for work items
bd create --title="Feature description (#GitHub-issue)" --type=feature --priority=2

# Update status when starting work
bd update <issue-id> --status=in_progress

# Close when complete
bd close <issue-id> --reason="Implemented in vX.Y.Z"
```

### 3. Create Draft Release Before Committing

```bash
# Create draft release with release notes
gh release create vX.Y.Z --draft --title "vX.Y.Z - Release Title" --notes "$(cat <<'EOF'
## What's New

### Feature Name (#issue)
- Description of changes

## Technical Changes
- List of technical changes
EOF
)"
```

### 4. Code Review

Before committing, run the code review agent:
- Check for code quality issues
- Verify security concerns
- Ensure best practices

### 5. Commit and Push

```bash
# Stage changes
git add <files>

# Commit with descriptive message
git commit -m "vX.Y.Z - Release Title

- Change 1
- Change 2"

# Push branch
git push -u origin vX.Y.Z
```

### 6. Create Pull Request

```bash
gh pr create --title "vX.Y.Z - Release Title" --body "$(cat <<'EOF'
## Summary
- Change summary

## Test Plan
- [ ] Test item 1
- [ ] Test item 2

Closes #issue1
Closes #issue2
EOF
)"
```

### 7. Comment on GitHub Issues

```bash
# Notify issue reporters
gh issue comment <issue-number> --body "Fixed in PR #XX. Description of fix."
```

### 8. Monitor CI and Merge

```bash
# Watch GitHub Actions
gh run watch <run-id> --exit-status

# Merge when CI passes
gh pr merge <pr-number> --merge --delete-branch

# Switch to main
git checkout main && git pull
```

### 9. Publish Release

```bash
# Publish the draft release
gh release edit vX.Y.Z --draft=false

# Verify
gh release view vX.Y.Z
```

## Beads Integration

This project uses beads for local issue tracking across sessions.

### Files
- `.beads/issues.jsonl` - Issue data (committed)
- `.beads/interactions.jsonl` - Audit log (committed)
- `.beads/beads.db` - Local cache (gitignored)

### Commands
- `bd ready` - Find available work
- `bd create` - Create new issue
- `bd update` - Update issue status
- `bd close` - Close completed issues
- `bd sync --from-main` - Sync from main branch

## Git Commit Guidelines

- Do not include AI attribution in commit messages
- Use conventional commit format
- Keep messages clear and descriptive
- Reference GitHub issue numbers where applicable
