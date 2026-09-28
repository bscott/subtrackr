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

# Switch to main, fast-forward to the merge commit, and pin its SHA
git checkout main
git pull --ff-only
RELEASE_SHA=$(git rev-parse HEAD)
```

Drop `--delete-branch` for pull requests from forks. The branch lives in the
contributor's repository, so the delete fails for lack of permission — and when
the PR's head branch is the fork's `main` (common for drive-by contributions),
it is that fork's default branch, which GitHub refuses to delete outright.

Merge **everything** destined for the release before pinning `$RELEASE_SHA`.
`docker-publish.yml` sets `concurrency.cancel-in-progress`, keyed on the ref, so
a later push to `main` cancels the build for an earlier one. Pinning first and
then merging again leaves step 9 watching a cancelled run, which `gh run watch
--exit-status` reports as a failure rather than as the no-op it is.

### 8b. Collecting several contributor PRs into one release

When a release gathers external PRs rather than your own feature branch, merge
their refs into the version branch instead of merging each PR on GitHub. This
keeps one CI run and one Docker build to verify, and preserves the contributors'
commits and authorship:

```bash
for n in <pr numbers>; do git fetch origin "pull/$n/head:pr-$n"; done
git merge --no-ff -m "Merge PR #<n>: <title>" "pr-<n>"   # repeat per PR
```

Test the combined tree (`go build ./... && go vet ./... && go test ./...`)
before opening the release PR. Because the contributor PRs are never merged
through GitHub, they stay open: close each one with a comment pointing at the
release PR, and put its `Closes #<n>` lines in the release PR body.

### 9. Verify Docker Build on Main

Every merge to main triggers the Docker build workflow, which pushes `:main` and `:sha-*` images. Do NOT publish the release until the build for `$RELEASE_SHA` succeeds — it proves the exact commit that will be tagged produces a working image.

```bash
# Find and watch the build for the pinned release commit
RUN_ID=$(gh run list --workflow=docker-publish.yml --commit "$RELEASE_SHA" --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$RUN_ID" --exit-status
```

### 10. Publish Release

Only after the main-branch Docker build for `$RELEASE_SHA` has succeeded:

```bash
# Publish the draft and create its tag at the verified commit, not moving main
gh release edit vX.Y.Z --target "$RELEASE_SHA" --draft=false

# Verify the published tag resolves to the verified commit
git fetch --tags origin
test "$(git rev-parse 'vX.Y.Z^{commit}')" = "$RELEASE_SHA"
gh release view vX.Y.Z
```

Publishing creates the version tag at `$RELEASE_SHA`, which triggers a second Docker build for the same source commit. It publishes the semver image tag without the leading `v` (for example, `:0.6.5`) and `:latest`.

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
